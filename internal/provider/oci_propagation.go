// oci_propagation.go handles validation of OCI IAM credential propagation
// across services.
//
// OCI IAM is eventually consistent: after creating a user, group, policy,
// and API key, the credentials may not be immediately recognized by all
// OCI services (Identity, Core/VCN, Container Engine). Each service
// maintains its own IAM cache that refreshes independently.
//
// There is no OCI API to query propagation status directly, so we use
// two standard patterns to handle eventual consistency:
//
//   - Retry with backoff: repeatedly attempt API calls using the new
//     credentials, waiting between failures until the service accepts them.
//
//   - Convergence check: require N consecutive successes before considering
//     a service propagated. A single success may be a fluke if the request
//     was routed to a backend node that has already refreshed its cache
//     while others have not. Multiple consecutive successes provide
//     statistical confidence that propagation is complete.
//
// Services are validated in parallel since they propagate independently.
package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	ocicontainerengine "github.com/oracle/oci-go-sdk/v65/containerengine"
	ocicore "github.com/oracle/oci-go-sdk/v65/core"
	"github.com/oracle/oci-go-sdk/v65/identity"
	"golang.org/x/term"
)

const (
	// requiredConsecutiveSuccesses is the number of consecutive successful
	// API calls required before a service is considered propagated.
	requiredConsecutiveSuccesses = 10

	// maxPropagationAttempts is the maximum number of attempts per service
	// before giving up. At retryDelay intervals this gives a ~20 minute window.
	maxPropagationAttempts = 600

	// propagationRetryDelay is the wait time between failed attempts.
	propagationRetryDelay = 2 * time.Second

	// propagationSuccessDelay is the wait time between consecutive successes
	// during the convergence check.
	propagationSuccessDelay = 2 * time.Second
)

// propagationService defines a single OCI service to validate for credential propagation.
type propagationService struct {
	id   string
	name string
	test func() error
}

// propagationStatusUpdate is a status message sent from a validation goroutine
// to the display loop.
type propagationStatusUpdate struct {
	serviceID string
	status    OCIServiceStatus
}

// OCIServiceStatus represents the current status of a service propagation check.
type OCIServiceStatus struct {
	Name                 string
	ConsecutiveSuccesses int
	Attempts             int
	LastError            error
	Completed            bool
	Failed               bool
}

// propagationDisplay manages the terminal output for propagation status.
type propagationDisplay struct {
	services           []propagationService
	statusMap          map[string]*OCIServiceStatus
	lastStatuses       map[string]OCIServiceStatus
	lastDisplayTime    time.Time
	displayInitialized bool
}

// newPropagationDisplay creates a new propagation display.
func newPropagationDisplay(
	services []propagationService,
	statusMap map[string]*OCIServiceStatus,
) *propagationDisplay {
	return &propagationDisplay{
		services:     services,
		statusMap:    statusMap,
		lastStatuses: make(map[string]OCIServiceStatus),
	}
}

// update renders the current propagation status to stdout.
func (d *propagationDisplay) update() {
	// only display if there's been a meaningful change or enough time has passed
	hasChanged := false
	for id, status := range d.statusMap {
		if lastStatus, exists := d.lastStatuses[id]; !exists ||
			lastStatus.Completed != status.Completed ||
			lastStatus.Failed != status.Failed {
			hasChanged = true
			break
		}
	}

	if !hasChanged && time.Since(d.lastDisplayTime) < 2*time.Second {
		return
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	if !d.displayInitialized {
		// first time - print the initial lines
		for _, service := range d.services {
			fmt.Println(d.formatStatusLine(d.statusMap[service.id]))
			d.lastStatuses[service.id] = *d.statusMap[service.id]
		}
		d.displayInitialized = true
	} else if isTTY {
		// move cursor up to overwrite previous lines
		fmt.Printf("\033[%dA", len(d.services))
		for _, service := range d.services {
			fmt.Printf("\033[K%s\n", d.formatStatusLine(d.statusMap[service.id]))
			d.lastStatuses[service.id] = *d.statusMap[service.id]
		}
	}

	d.lastDisplayTime = time.Now()
}

// formatStatusLine returns a formatted status line for a single service.
func (d *propagationDisplay) formatStatusLine(status *OCIServiceStatus) string {
	if status.Completed {
		return fmt.Sprintf("%-30s synced", status.Name+"...")
	}

	state := "waiting"
	if status.Failed {
		state = "failed"
	}

	errorMsg := ""
	if status.LastError != nil {
		errorMsg = status.LastError.Error()
	}

	return fmt.Sprintf("%-30s %-10s%s", status.Name+"...", state, errorMsg)
}

// validateOCIUserPropagation validates that the service user credentials are propagated across all OCI services.
func (i *KubernetesRuntimeInfraOKE) validateOCIUserPropagation() error {
	// create a raw configuration provider with the service user credentials
	configProvider := common.NewRawConfigurationProvider(
		i.TenancyOCID,
		i.ServiceUserOCID,
		i.Region,
		i.Fingerprint,
		i.PrivateKeyPEM,
		nil,
	)

	// define the OCI services to validate — each service maintains its own
	// IAM cache and propagates independently
	services := []propagationService{
		{
			id:   "identity",
			name: "Identity service",
			test: func() error {
				identityClient, err := identity.NewIdentityClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create identity client: %w", err)
				}
				// set region to ensure we're validating against the target deployment region
				identityClient.SetRegion(i.Region)
				getCompartmentRequest := identity.GetCompartmentRequest{
					CompartmentId: &i.CompartmentOCID,
				}
				_, err = identityClient.GetCompartment(context.Background(), getCompartmentRequest)
				return err
			},
		},
		{
			id:   "core",
			name: "Core service",
			test: func() error {
				coreClient, err := ocicore.NewVirtualNetworkClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create core client: %w", err)
				}
				// set region to ensure we're validating against the target deployment region
				coreClient.SetRegion(i.Region)

				// test VCN access
				vcnRequest := ocicore.ListVcnsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListVcns(context.Background(), vcnRequest)
				if err != nil {
					return fmt.Errorf("VCN access failed: %w", err)
				}

				// test Internet Gateway access
				igwRequest := ocicore.ListInternetGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListInternetGateways(context.Background(), igwRequest)
				if err != nil {
					return fmt.Errorf("Internet Gateway access failed: %w", err)
				}

				// test NAT Gateway access
				natRequest := ocicore.ListNatGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListNatGateways(context.Background(), natRequest)
				if err != nil {
					return fmt.Errorf("NAT Gateway access failed: %w", err)
				}

				// test Service Gateway access
				sgwRequest := ocicore.ListServiceGatewaysRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListServiceGateways(context.Background(), sgwRequest)
				if err != nil {
					return fmt.Errorf("Service Gateway access failed: %w", err)
				}

				// test Security Lists access
				secListRequest := ocicore.ListSecurityListsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListSecurityLists(context.Background(), secListRequest)
				if err != nil {
					return fmt.Errorf("Security Lists access failed: %w", err)
				}

				// test Subnets access
				subnetRequest := ocicore.ListSubnetsRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = coreClient.ListSubnets(context.Background(), subnetRequest)
				if err != nil {
					return fmt.Errorf("Subnets access failed: %w", err)
				}

				return nil
			},
		},
		{
			id:   "container-engine",
			name: "Container Engine service",
			test: func() error {
				ceClient, err := ocicontainerengine.NewContainerEngineClientWithConfigurationProvider(configProvider)
				if err != nil {
					return fmt.Errorf("failed to create container engine client: %w", err)
				}
				// set region to ensure we're validating against the target deployment region
				ceClient.SetRegion(i.Region)
				ceRequest := ocicontainerengine.ListClustersRequest{
					CompartmentId: common.String(i.CompartmentOCID),
					Limit:         common.Int(1),
				}
				_, err = ceClient.ListClusters(context.Background(), ceRequest)
				return err
			},
		},
	}

	return waitForPropagation(services)
}

// waitForPropagation runs convergence checks for all services in parallel and
// displays live status updates. It returns nil when all services have reached
// the required number of consecutive successes, or an error if any service
// fails to propagate within the maximum number of attempts.
func waitForPropagation(services []propagationService) error {
	// initialize status map
	statusMap := make(map[string]*OCIServiceStatus)
	for _, service := range services {
		statusMap[service.id] = &OCIServiceStatus{
			Name: service.name,
		}
	}

	// channel for status updates from validation goroutines
	statusChan := make(chan propagationStatusUpdate, 100)

	// start convergence check for each service in parallel
	var wg sync.WaitGroup
	for _, service := range services {
		wg.Add(1)
		go func(svc propagationService) {
			defer wg.Done()
			runConvergenceCheck(svc, statusChan)
		}(service)
	}

	// close channel when all goroutines are done
	go func() {
		wg.Wait()
		close(statusChan)
	}()

	// display live status updates
	fmt.Printf("Validating service propagation\n")
	display := newPropagationDisplay(services, statusMap)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case update, ok := <-statusChan:
			if !ok {
				// channel closed, all done
				display.update()

				// check for failures
				for _, status := range statusMap {
					if status.Failed {
						return fmt.Errorf("%s failed to propagate", status.Name)
					}
				}

				fmt.Printf("All services propagated successfully\n")
				return nil
			}
			statusMap[update.serviceID] = &update.status
			display.update()

		case <-ticker.C:
			display.update()
		}
	}
}

// runConvergenceCheck runs the retry-with-backoff and convergence check loop
// for a single service. It sends status updates to the provided channel
// until the service is confirmed propagated or the maximum attempts are exhausted.
func runConvergenceCheck(svc propagationService, statusChan chan<- propagationStatusUpdate) {
	consecutiveSuccesses := 0
	attempts := 0

	for consecutiveSuccesses < requiredConsecutiveSuccesses && attempts < maxPropagationAttempts {
		attempts++

		err := svc.test()
		if err != nil {
			consecutiveSuccesses = 0 // reset on failure
			statusChan <- propagationStatusUpdate{
				serviceID: svc.id,
				status: OCIServiceStatus{
					Name:                 svc.name,
					ConsecutiveSuccesses: consecutiveSuccesses,
					Attempts:             attempts,
					LastError:            simplifyOCIError(err),
					Completed:            false,
					Failed:               false,
				},
			}
			time.Sleep(propagationRetryDelay)
		} else {
			consecutiveSuccesses++
			completed := consecutiveSuccesses >= requiredConsecutiveSuccesses
			statusChan <- propagationStatusUpdate{
				serviceID: svc.id,
				status: OCIServiceStatus{
					Name:                 svc.name,
					ConsecutiveSuccesses: consecutiveSuccesses,
					Attempts:             attempts,
					LastError:            nil,
					Completed:            completed,
					Failed:               false,
				},
			}
			if !completed {
				time.Sleep(propagationSuccessDelay)
			}
		}
	}

	// mark as failed if max attempts reached
	if consecutiveSuccesses < requiredConsecutiveSuccesses {
		statusChan <- propagationStatusUpdate{
			serviceID: svc.id,
			status: OCIServiceStatus{
				Name:                 svc.name,
				ConsecutiveSuccesses: consecutiveSuccesses,
				Attempts:             attempts,
				LastError:            fmt.Errorf("max attempts reached"),
				Completed:            false,
				Failed:               true,
			},
		}
	}
}

// simplifyOCIError reduces verbose OCI SDK error messages to a concise
// form suitable for display in the status output.
func simplifyOCIError(err error) error {
	if strings.Contains(err.Error(), "Http Status Code:") {
		// extract just the HTTP status code
		parts := strings.Split(err.Error(), "Http Status Code: ")
		if len(parts) > 1 {
			codePart := strings.Split(parts[1], ".")[0]
			return fmt.Errorf("HTTP %s", codePart)
		}
		return fmt.Errorf("Auth failed")
	}
	return fmt.Errorf("Auth failed: %w", err)
}

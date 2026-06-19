package gcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	machine "github.com/threeport/threeport/internal/provider/machine"
	gcpauth "github.com/threeport/threeport/pkg/auth/v0"
)

// orphanReclaimCloud is the cloud surface the post-destroy reclaim probes and
// deletes through. Production satisfies it with a compute-API client and tests
// with a fake, so the reclaim logic runs without a live cloud.
type orphanReclaimCloud interface {
	instanceExists() (bool, error)
	deleteInstance() error
	firewallExists() (bool, error)
	deleteFirewall() error
}

// reclaimOrphans deletes any instance or firewall the cloud still reports after
// a destroy that claimed success, then re-probes to confirm. It returns nil only
// once both are gone; a survivor, a delete failure, or a probe failure returns
// an error so the caller requeues rather than confirming a deletion that left a
// live resource behind.
func reclaimOrphans(cloud orphanReclaimCloud) error {
	if err := reclaimResource("instance", cloud.instanceExists, cloud.deleteInstance); err != nil {
		return err
	}
	return reclaimResource("firewall", cloud.firewallExists, cloud.deleteFirewall)
}

// reclaimResource deletes one named resource when the cloud still reports it,
// then re-probes; a resource that survives the delete returns an error so the
// teardown retries instead of abandoning it.
func reclaimResource(kind string, exists func() (bool, error), del func() error) error {
	present, err := exists()
	if err != nil {
		return fmt.Errorf("failed to probe %s for orphan reclaim: %w", kind, err)
	}
	if !present {
		return nil
	}
	if err := del(); err != nil {
		return fmt.Errorf("failed to delete orphaned %s: %w", kind, err)
	}
	stillPresent, err := exists()
	if err != nil {
		return fmt.Errorf("failed to re-probe %s after orphan reclaim: %w", kind, err)
	}
	if stillPresent {
		return fmt.Errorf("orphaned %s still present after delete", kind)
	}
	return nil
}

// computeOrphanReclaimCloud probes and deletes the deterministically named GCE
// instance and firewall through the compute API.
type computeOrphanReclaimCloud struct {
	ctx          context.Context
	service      *computev1.Service
	project      string
	zone         string
	instanceName string
	firewallName string
}

// newComputeOrphanReclaimCloud builds a reclaim client for a GCE machine. It
// requires the project, zone, and instance name to address the resources, so it
// accumulates every missing field into one error rather than probing against
// unknown coordinates and reading a false not-found as gone.
func newComputeOrphanReclaimCloud(gceInfra *machine.GceMachineInfra) (*computeOrphanReclaimCloud, error) {
	var missing []string
	if gceInfra.ProjectID == "" {
		missing = append(missing, "ProjectID")
	}
	if gceInfra.Zone == "" {
		missing = append(missing, "Zone")
	}
	if gceInfra.RuntimeInstanceName == "" {
		missing = append(missing, "RuntimeInstanceName")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("orphan reclaim missing required fields: %s", strings.Join(missing, ", "))
	}

	if err := gcpauth.EnsureGCPAuth(gceInfra.ServiceAccountCredentials); err != nil {
		return nil, fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	ctx := context.Background()
	service, err := computev1.NewService(ctx, option.WithScopes(computev1.ComputeScope))
	if err != nil {
		return nil, fmt.Errorf("failed to create compute service: %w", err)
	}

	return &computeOrphanReclaimCloud{
		ctx:          ctx,
		service:      service,
		project:      gceInfra.ProjectID,
		zone:         gceInfra.Zone,
		instanceName: gceInfra.RuntimeInstanceName,
		firewallName: fmt.Sprintf("%s-ssh", gceInfra.RuntimeInstanceName),
	}, nil
}

// instanceExists reports whether the named instance is still present; a 404 from
// the compute API reads as gone.
func (c *computeOrphanReclaimCloud) instanceExists() (bool, error) {
	if _, err := c.service.Instances.Get(c.project, c.zone, c.instanceName).Context(c.ctx).Do(); err != nil {
		if isComputeNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// deleteInstance deletes the named instance and awaits the zone operation,
// tolerating a not-found in case a concurrent pass already removed it.
func (c *computeOrphanReclaimCloud) deleteInstance() error {
	op, err := c.service.Instances.Delete(c.project, c.zone, c.instanceName).Context(c.ctx).Do()
	if err != nil {
		if isComputeNotFound(err) {
			return nil
		}
		return err
	}
	return c.awaitZoneOperation(op.Name)
}

// firewallExists reports whether the named firewall is still present; a 404
// reads as gone.
func (c *computeOrphanReclaimCloud) firewallExists() (bool, error) {
	if _, err := c.service.Firewalls.Get(c.project, c.firewallName).Context(c.ctx).Do(); err != nil {
		if isComputeNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// deleteFirewall deletes the named firewall and awaits the global operation,
// tolerating a not-found in case a concurrent pass already removed it.
func (c *computeOrphanReclaimCloud) deleteFirewall() error {
	op, err := c.service.Firewalls.Delete(c.project, c.firewallName).Context(c.ctx).Do()
	if err != nil {
		if isComputeNotFound(err) {
			return nil
		}
		return err
	}
	return c.awaitGlobalOperation(op.Name)
}

// awaitZoneOperation polls a zone operation until it reports DONE, surfacing an
// operation-level failure as an error.
func (c *computeOrphanReclaimCloud) awaitZoneOperation(opName string) error {
	for {
		op, err := c.service.ZoneOperations.Wait(c.project, c.zone, opName).Context(c.ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to await zone operation %s: %w", opName, err)
		}
		if op.Status == "DONE" {
			if op.Error != nil && len(op.Error.Errors) > 0 {
				return fmt.Errorf("zone operation %s failed: %s", opName, op.Error.Errors[0].Message)
			}
			return nil
		}
	}
}

// awaitGlobalOperation polls a global operation until it reports DONE, surfacing
// an operation-level failure as an error.
func (c *computeOrphanReclaimCloud) awaitGlobalOperation(opName string) error {
	for {
		op, err := c.service.GlobalOperations.Wait(c.project, opName).Context(c.ctx).Do()
		if err != nil {
			return fmt.Errorf("failed to await global operation %s: %w", opName, err)
		}
		if op.Status == "DONE" {
			if op.Error != nil && len(op.Error.Errors) > 0 {
				return fmt.Errorf("global operation %s failed: %s", opName, op.Error.Errors[0].Message)
			}
			return nil
		}
	}
}

// isComputeNotFound reports whether a compute API error is a 404, the signal
// that the addressed resource does not exist.
func isComputeNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}

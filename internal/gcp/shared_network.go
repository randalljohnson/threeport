package gcp

import (
	"errors"
	"fmt"
	"net"

	"github.com/go-logr/logr"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
)

// ensureGcpSharedNetwork resolves the shared VPC network for the (provider,
// region) tuple carried by the GCE machine instance. When no shared network
// exists, one is created from the instance's NetworkCIDR and SubnetCIDR and
// the instance record is updated with the resulting FK so requires-AOR wiring
// holds the shared network in place while the instance exists. Returns the
// resolved shared network or an error when the instance carries no region or
// no CIDRs to seed a fresh shared network.
func ensureGcpSharedNetwork(
	r *controller.Reconciler,
	instance *v0.GcpGceMachineRuntimeInstance,
	log *logr.Logger,
) (*v0.GcpSharedNetwork, error) {
	// re-read the latest instance state so a redelivered create sees the
	// shared network FK it wrote on a prior pass, and so the region check
	// keys off the persisted value rather than the possibly-stale
	// notification payload
	if instance.ID == nil {
		return nil, errors.New("GCE instance missing required field ID")
	}
	latest, err := client.GetGcpGceMachineRuntimeInstanceByID(
		r.APIClient,
		r.APIServer,
		*instance.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest GCE instance for shared network resolution: %w", err)
	}

	// short-circuit when the instance is already wired to a shared network so
	// a redelivered create does not clone the record
	if latest.GcpSharedNetworkID != nil {
		instance.GcpSharedNetworkID = latest.GcpSharedNetworkID
		return client.GetGcpSharedNetworkByID(
			r.APIClient,
			r.APIServer,
			*latest.GcpSharedNetworkID,
		)
	}

	if latest.GcpProviderID == nil {
		return nil, errors.New("GCE instance missing required field GcpProviderID")
	}
	// no region means no shared-network scope to resolve into; log and skip so
	// the caller falls back to the instance-level network fields
	if latest.Region == nil || *latest.Region == "" {
		if log != nil {
			log.Info("GCE instance missing region; skipping shared network resolution")
		}
		return nil, nil
	}
	instance = latest

	query := fmt.Sprintf(
		"gcpproviderid=%d&region=%s",
		*instance.GcpProviderID,
		*instance.Region,
	)
	existing, err := client.GetGcpSharedNetworksByQueryString(
		r.APIClient,
		r.APIServer,
		query,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing GCP shared networks: %w", err)
	}

	var shared *v0.GcpSharedNetwork
	if existing != nil && len(*existing) > 0 {
		shared = &(*existing)[0]
	} else {
		if instance.NetworkCIDR == nil || *instance.NetworkCIDR == "" {
			return nil, errors.New("cannot create shared network: instance NetworkCIDR is empty")
		}
		if instance.SubnetCIDR == nil || *instance.SubnetCIDR == "" {
			return nil, errors.New("cannot create shared network: instance SubnetCIDR is empty")
		}
		contained, err := CidrContainsSubnet(*instance.NetworkCIDR, *instance.SubnetCIDR)
		if err != nil {
			return nil, fmt.Errorf("failed to validate subnet containment: %w", err)
		}
		if !contained {
			return nil, fmt.Errorf(
				"subnet CIDR %s is not contained in network CIDR %s",
				*instance.SubnetCIDR,
				*instance.NetworkCIDR,
			)
		}

		sharedName := fmt.Sprintf(
			"gcp-shared-network-%d-%s",
			*instance.GcpProviderID,
			*instance.Region,
		)
		toCreate := &v0.GcpSharedNetwork{
			Instance:      v0.Instance{Name: &sharedName},
			GcpProviderID: instance.GcpProviderID,
			Region:        instance.Region,
			NetworkCIDR:   instance.NetworkCIDR,
			SubnetCIDR:    instance.SubnetCIDR,
		}
		created, err := client.CreateGcpSharedNetwork(
			r.APIClient,
			r.APIServer,
			toCreate,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP shared network: %w", err)
		}
		if log != nil {
			log.Info(
				"created shared network for GCE instance",
				"gcpSharedNetworkID", *created.ID,
				"gcpProviderID", *instance.GcpProviderID,
				"region", *instance.Region,
			)
		}
		shared = created
	}

	// wire the FK back onto the instance so requires-AOR fires and subsequent
	// reconciles short-circuit through the GcpSharedNetworkID lookup
	fkUpdate := v0.GcpGceMachineRuntimeInstance{
		Common:             v0.Common{ID: instance.ID},
		GcpSharedNetworkID: shared.ID,
	}
	if _, err := client.UpdateGcpGceMachineRuntimeInstance(
		r.APIClient,
		r.APIServer,
		&fkUpdate,
	); err != nil {
		return nil, fmt.Errorf("failed to attach shared network to GCE instance: %w", err)
	}
	instance.GcpSharedNetworkID = shared.ID
	return shared, nil
}

// CidrContainsSubnet reports whether subnetCIDR is entirely contained within
// networkCIDR. Both arguments must parse as valid CIDR blocks.
func CidrContainsSubnet(networkCIDR, subnetCIDR string) (bool, error) {
	_, networkNet, err := net.ParseCIDR(networkCIDR)
	if err != nil {
		return false, fmt.Errorf("failed to parse network CIDR: %w", err)
	}
	subnetIP, subnetNet, err := net.ParseCIDR(subnetCIDR)
	if err != nil {
		return false, fmt.Errorf("failed to parse subnet CIDR: %w", err)
	}
	// containment check: the subnet's network address must sit inside the
	// outer network, and the outer mask must be no wider than the subnet's
	networkOnes, networkBits := networkNet.Mask.Size()
	subnetOnes, subnetBits := subnetNet.Mask.Size()
	if networkBits != subnetBits {
		return false, nil
	}
	if networkOnes > subnetOnes {
		return false, nil
	}
	return networkNet.Contains(subnetIP), nil
}


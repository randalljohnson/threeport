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

// defaultNetworkCIDR is applied to a GcpNetwork when the referencing instance
// leaves NetworkCIDR unset. Matches the sxalable deployer's default VPC CIDR.
const defaultNetworkCIDR = "10.0.0.0/16"

// defaultSubnetCIDR is applied to a GcpNetwork when the referencing instance
// leaves SubnetCIDR unset. Matches the sxalable deployer's default subnet CIDR.
const defaultSubnetCIDR = "10.0.1.0/24"

// ensureGcpNetwork resolves the shared VPC network for the (provider, zone)
// tuple carried by the machine runtime instance. When no network exists, one
// is created from the machine runtime instance's NetworkCIDR and SubnetCIDR
// (defaulted when unset). Returns the resolved network so the caller can seed
// the pulumi program with its provider IDs or CIDRs. Deletion protection is
// enforced by GcpNetwork.beforeDelete, which counts referencing instances
// live, so no FK is stored on the instance.
func ensureGcpNetwork(
	r *controller.Reconciler,
	gceInstance *v0.GcpGceMachineRuntimeInstance,
	mri *v0.MachineRuntimeInstance,
	log *logr.Logger,
) (*v0.GcpNetwork, error) {
	if gceInstance.GcpProviderID == nil {
		return nil, errors.New("GCE instance missing required field GcpProviderID")
	}
	if gceInstance.Zone == nil || *gceInstance.Zone == "" {
		if log != nil {
			log.Info("GCE instance missing zone; skipping shared network resolution")
		}
		return nil, nil
	}

	// look up existing networks for the (provider, zone) tuple; a hit is
	// reused so every instance in the same org+zone shares one VPC
	query := fmt.Sprintf(
		"gcpproviderid=%d&zone=%s",
		*gceInstance.GcpProviderID,
		*gceInstance.Zone,
	)
	existing, err := client.GetGcpNetworksByQueryString(
		r.APIClient,
		r.APIServer,
		query,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing GCP networks: %w", err)
	}
	if existing != nil && len(*existing) > 0 {
		shared := (*existing)[0]
		return &shared, nil
	}

	// no existing network: fetch the provider for orgName and derive CIDRs
	// from the machine runtime instance, defaulting when unset
	gcpProvider, err := client.GetGcpProviderByID(
		r.APIClient,
		r.APIServer,
		*gceInstance.GcpProviderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve GCP provider by ID: %w", err)
	}
	if gcpProvider.Name == nil || *gcpProvider.Name == "" {
		return nil, errors.New("gcp provider missing required field Name")
	}

	networkCIDR := defaultNetworkCIDR
	subnetCIDR := defaultSubnetCIDR
	if mri != nil {
		if mri.NetworkCIDR != nil && *mri.NetworkCIDR != "" {
			networkCIDR = *mri.NetworkCIDR
		}
		if mri.SubnetCIDR != nil && *mri.SubnetCIDR != "" {
			subnetCIDR = *mri.SubnetCIDR
		}
	}

	contained, err := CidrContainsSubnet(networkCIDR, subnetCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to validate subnet containment: %w", err)
	}
	if !contained {
		return nil, fmt.Errorf(
			"subnet CIDR %s is not contained in network CIDR %s",
			subnetCIDR,
			networkCIDR,
		)
	}

	// name the network per the sxalable convention:
	// sxalable-vpc{n}-{orgName}-{zone}, where {n} is a monotonic sequence
	// for the (org, zone) tuple derived from the count of existing networks
	sequence := 1
	if existing != nil {
		sequence = len(*existing) + 1
	}
	networkName := fmt.Sprintf(
		"sxalable-vpc%d-%s-%s",
		sequence,
		*gcpProvider.Name,
		*gceInstance.Zone,
	)

	toCreate := &v0.GcpNetwork{
		Instance:      v0.Instance{Name: &networkName},
		GcpProviderID: gceInstance.GcpProviderID,
		Zone:          gceInstance.Zone,
		NetworkCIDR:   &networkCIDR,
		SubnetCIDR:    &subnetCIDR,
	}
	created, err := client.CreateGcpNetwork(
		r.APIClient,
		r.APIServer,
		toCreate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCP network: %w", err)
	}
	if log != nil {
		log.Info(
			"created network for GCE instance",
			"gcpNetworkID", *created.ID,
			"gcpNetworkName", networkName,
			"gcpProviderID", *gceInstance.GcpProviderID,
			"zone", *gceInstance.Zone,
		)
	}
	return created, nil
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
	// containment: the subnet's network address sits inside the outer network
	// and the outer mask is no wider than the subnet's
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

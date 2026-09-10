// Package machine provisions a Google Compute Engine VM for a Threeport
// machine runtime through Pulumi. The stack creates an SSH firewall rule
// and an instance with an ephemeral public IP, then captures hostname and
// NAT IP as outputs. SSH keys are generated in process. The public key is
// written to instance metadata. The private key is not a stack output.
package machine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/crypto/ssh"
	computev1 "google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	"gorm.io/datatypes"

	"github.com/threeport/threeport/internal/provider"
	gcpauth "github.com/threeport/threeport/pkg/auth/v0"
)

// compile-time guarantees that GceMachineInfra satisfies the infra provider
// lifecycle contract plus the optional streaming, refresh, and adopt seams.
// The three streaming and refresh methods (GetStateFilePath, ReadStateFile,
// RefreshStack) come from the embedded PulumiWorkspace for free; the adopt
// method is implemented on this provider.
var (
	_ provider.InfraProvider       = (*GceMachineInfra)(nil)
	_ provider.StreamableProvider  = (*GceMachineInfra)(nil)
	_ provider.RefreshableProvider = (*GceMachineInfra)(nil)
	_ provider.AdoptableProvider   = (*GceMachineInfra)(nil)
)

// adoptResourceKind identifies which deterministically named GCE resource an
// adopt target refers to, so the discover helper can branch on kind without
// string matching on logical names.
type adoptResourceKind int

const (
	// adoptInstance is the VM instance resource.
	adoptInstance adoptResourceKind = iota
)

// adoptTarget pairs a resource kind with the Pulumi logical name the program
// registers it under, so DiscoverAndAdopt records each constructed import ID
// against the name the program later looks it up by.
type adoptTarget struct {
	kind        adoptResourceKind
	logicalName string
}

// defaultIngressSourceRange is the fallback source-CIDR list used when an
// ingress rule leaves SourceRanges empty. World-open; callers should narrow
// it per rule for production.
const defaultIngressSourceRange = "0.0.0.0/0"

// GceIngressRule describes a single firewall ingress rule in the shape the
// GCE provider consumes. Callers translate a portable rule model into this
// value when configuring the provider.
type GceIngressRule struct {
	// Protocol is the L4 protocol name ("tcp", "udp", "icmp") or bare
	// protocol number ("112" for VRRP).
	Protocol string

	// Ports are the destination ports the rule allows. Empty means all
	// ports for this protocol, which is the correct shape for protocols
	// without ports (icmp, esp, vrrp).
	Ports []string

	// SourceRanges are the source CIDR blocks the rule allows. Empty
	// falls back to the world-open range.
	SourceRanges []string

	// Description is an optional human-readable note attached to the rule.
	Description string
}

// GceMachineInfra is the Google Compute Engine backend for a machine runtime.
// It embeds PulumiWorkspace for stack, state, and automation API helpers.
type GceMachineInfra struct {
	provider.PulumiWorkspace

	// The Google Cloud project ID where the VM is provisioned
	ProjectID string

	// The Google Cloud region written to Pulumi stack config
	Region string

	// The Google Cloud zone where the VM is created
	Zone string

	// The GCE machine type, e.g. e2-medium
	MachineType string

	// The boot disk image, e.g. debian-cloud/debian-12
	ImageID string

	// The VPC network self-link or name the instance attaches to
	NetworkID string

	// SubnetID is the subnetwork selfLink or self-name the VM's primary
	// interface should attach to. When non-empty it takes priority over
	// SubnetCIDR: the program skips subnet creation and attaches to the
	// existing subnetwork verbatim. Required in custom-mode shared VPCs
	// where multiple subnets share a region and no CIDR-driven create is
	// desired.
	SubnetID string

	// ServiceAccountCredentials contains the JSON key for a GCP service
	// account, used when running outside GCP where Workload Identity is
	// not available.
	ServiceAccountCredentials string

	// The Linux user that receives the generated SSH public key
	SSHUser string

	// IngressRules are additional firewall ingress rules to open on the VM's
	// network. Each rule renders to one google_compute_firewall alongside the
	// SSH firewall; the SSH-allow rule stays on its own resource for now.
	IngressRules []GceIngressRule

	// NetworkCIDR, when non-empty, drives creation of a custom-mode VPC
	// network for the VM. Modern GCP VPCs are logical containers with no
	// top-level CIDR; the CIDR intent is applied to the subnet resource.
	// When empty the program attaches to the existing network named by
	// NetworkID.
	NetworkCIDR string

	// SubnetCIDR, when non-empty, drives creation of a subnetwork with that
	// CIDR range in the VM's region. The subnetwork is attached to the
	// custom-mode network created from NetworkCIDR, or to the pre-existing
	// NetworkID network when NetworkCIDR is empty. When both CIDRs are empty
	// the VM uses the default subnet of NetworkID.
	SubnetCIDR string

	// AssignPublicIP controls whether the primary network interface gets an
	// ephemeral external IP via an access_config block. When false the
	// interface has no access_config and the VM has only an internal IP.
	AssignPublicIP bool

	// The generated RSA private key in PEM form
	sshPrivateKeyPEM string

	// The generated SSH public key in authorized_keys form
	sshPublicKeyAuthorized string

	// The instance name exported from the stack
	hostname string

	// The ephemeral public IPv4 exported from the stack
	externalIP string

	// internalIPs are the primary internal IP addresses captured from each
	// network interface after a successful deploy. Single-entry today because
	// the pulumi program attaches only one interface; kept as a slice so a
	// future multi-interface program surfaces every address without an API change.
	internalIPs []string

	// attachedVPCs are the VPC selfLink URLs of every network the VM is
	// attached to, captured from network interface outputs after deploy.
	// Same single-entry shape as internalIPs for the same reason.
	attachedVPCs []string

	// attachedSubnets are the subnet selfLink URLs of every subnetwork the VM
	// is attached to, captured from network interface outputs. Empty on
	// interfaces that fall back to the default subnet of the attached network.
	attachedSubnets []string

	// adoptImportIDs maps a resource's Pulumi logical name to the import ID
	// DiscoverAndAdopt found for it. The program attaches pulumi.Import for
	// any resource present here so the deploy adopts an existing cloud
	// resource instead of creating a duplicate. Empty on a clean create.
	adoptImportIDs map[string]string
}

// NewGceMachineInfra returns a GCE machine provider for the named runtime instance.
func NewGceMachineInfra(name string, opts ...provider.PulumiWorkspaceOption) *GceMachineInfra {
	return &GceMachineInfra{
		PulumiWorkspace: *provider.NewPulumiWorkspace(name, "gce", opts...),
	}
}

// ensurePulumiProjectDefaults sets Pulumi project metadata when not provided by callers.
func (i *GceMachineInfra) ensurePulumiProjectDefaults() {
	if i.ProjectName == "" {
		i.ProjectName = "gce"
	}
	if i.ProjectDescription == "" {
		i.ProjectDescription = "Google Compute Engine VM for Threeport"
	}
}

// syncStackConfigs updates stack config keys from the current ProjectID and Region.
func (i *GceMachineInfra) syncStackConfigs() {
	i.StackConfigs = map[string]string{
		"gcp:project": i.ProjectID,
		"gcp:region":  i.Region,
	}
}

// validateRequiredFields returns a descriptive error naming every required
// field that is empty, so a misconfigured provider fails fast before any auth
// or cloud call rather than mid-deploy inside the Pulumi engine. NetworkID and
// NetworkCIDR are mutually exclusive: exactly one must be set so the program
// either attaches to a pre-existing network or creates a new one, never both.
func (i *GceMachineInfra) validateRequiredFields() error {
	var missing []string
	if i.RuntimeInstanceName == "" {
		missing = append(missing, "RuntimeInstanceName")
	}
	if i.ProjectID == "" {
		missing = append(missing, "ProjectID")
	}
	if i.Zone == "" {
		missing = append(missing, "Zone")
	}
	if i.MachineType == "" {
		missing = append(missing, "MachineType")
	}
	if i.ImageID == "" {
		missing = append(missing, "ImageID")
	}
	if i.SSHUser == "" {
		missing = append(missing, "SSHUser")
	}
	if i.NetworkID == "" && i.NetworkCIDR == "" {
		missing = append(missing, "NetworkID or NetworkCIDR")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	if i.NetworkID != "" && i.NetworkCIDR != "" {
		return fmt.Errorf("NetworkID and NetworkCIDR are mutually exclusive: set one to attach to an existing network or the other to create a new one")
	}
	return nil
}

// DeployInfra creates the GCE VM and SSH firewall. It satisfies InfraProvider.
func (i *GceMachineInfra) DeployInfra() error {
	return i.createInfra()
}

// createInfra validates config, authenticates to GCP, generates SSH keys, and runs the stack.
func (i *GceMachineInfra) createInfra() error {
	// validate required fields
	if err := i.validateRequiredFields(); err != nil {
		return fmt.Errorf("invalid GCE machine configuration: %w", err)
	}

	// ensure GCP authentication is in place
	if err := gcpauth.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	// generate SSH keys outside the Pulumi program so the program is deterministic
	if err := i.ensureSSHKeyPair(); err != nil {
		return fmt.Errorf("failed to ensure SSH key pair: %w", err)
	}

	// set Pulumi project defaults and stack config
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	// set up Pulumi workspace and get stack
	stack, err := i.SetupStack(i.pulumiProgram())
	if err != nil {
		return fmt.Errorf("failed to set up Pulumi workspace: %w", err)
	}

	// deploy the stack
	upResult, err := i.RunUp(context.Background(), stack)
	if err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	// capture hostname and external IP from stack outputs
	i.captureOutputs(upResult.Outputs)

	// assert the VM actually exists in GCP so a pulumi program that skipped
	// instance creation cannot leak an unbacked create-success up to the
	// reconciler.
	if err := i.verifyInstanceExists(context.Background()); err != nil {
		return err
	}

	return nil
}

// verifyInstanceExists confirms the deterministically named VM instance is live
// in GCP after a successful pulumi up. It returns a descriptive error when the
// compute API reports the instance is not found, so a pulumi program that
// silently skipped the instance resource cannot masquerade as create success.
func (i *GceMachineInfra) verifyInstanceExists(ctx context.Context) error {
	service, err := computev1.NewService(ctx, i.GcpClientOptions(option.WithScopes(computev1.ComputeReadonlyScope))...)
	if err != nil {
		return fmt.Errorf("failed to create GCE compute service for existence check: %w", err)
	}
	if _, err := service.Instances.Get(i.ProjectID, i.Zone, i.instanceLogicalName()).Context(ctx).Do(); err != nil {
		if isNotFound(err) {
			return fmt.Errorf(
				"pulumi reported success but instance %s not found in project %s",
				i.instanceLogicalName(), i.ProjectID,
			)
		}
		return fmt.Errorf("failed to verify GCE instance exists: %w", err)
	}
	return nil
}

// DestroyInfra tears down the GCE VM and SSH firewall. It satisfies InfraProvider.
func (i *GceMachineInfra) DestroyInfra() error {
	// ensure GCP authentication is in place
	if err := gcpauth.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	// set Pulumi project defaults and stack config
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	// destroy the Pulumi stack
	if err := i.DestroyStack(); err != nil {
		return fmt.Errorf("failed to destroy Pulumi stack: %w", err)
	}

	return nil
}

// GetStackState returns the current stack state. It fills project defaults first.
func (i *GceMachineInfra) GetStackState() (*datatypes.JSON, error) {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.GetStackState()
}

// SetStackState restores stack state from JSON. It fills project defaults first.
func (i *GceMachineInfra) SetStackState(state *datatypes.JSON) error {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.SetStackState(state)
}

// pulumiProgram defines the Pulumi resources for the GCE VM stack: an
// optional custom-mode network and subnet, the SSH-allow firewall, one
// firewall per configured IngressRule, and the VM instance itself with the
// generated public key injected into ssh-keys metadata.
func (i *GceMachineInfra) pulumiProgram() pulumi.RunFunc {
	return func(pctx *pulumi.Context) error {
		// thread service account credentials directly into the gcp provider
		// for this stack rather than relying on a process-global env var, so
		// two concurrent gce creates for different service accounts each get
		// their own provider credentials
		providerArgs := &gcp.ProviderArgs{
			Project: pulumi.String(i.ProjectID),
			Region:  pulumi.String(i.Region),
		}
		if i.ServiceAccountCredentials != "" {
			providerArgs.Credentials = pulumi.String(i.ServiceAccountCredentials)
		}

		gcpProvider, err := gcp.NewProvider(pctx, "gcp-provider", providerArgs)
		if err != nil {
			return fmt.Errorf("failed to create GCP provider: %w", err)
		}

		// default the network reference to the pre-existing NetworkID string;
		// upgrade to a newly-created custom-mode network when NetworkCIDR is
		// set. GCP VPCs have no top-level CIDR in custom-subnet mode; the
		// CIDR intent is carried by the subnetwork resource below.
		var networkRef pulumi.StringInput = pulumi.String(i.NetworkID)
		var networkResource pulumi.Resource
		if i.NetworkCIDR != "" {
			network, err := compute.NewNetwork(pctx, i.networkLogicalName(), &compute.NetworkArgs{
				Name:                  pulumi.String(i.networkLogicalName()),
				AutoCreateSubnetworks: pulumi.Bool(false),
			}, i.resourceOptions(gcpProvider, i.networkLogicalName())...)
			if err != nil {
				return fmt.Errorf("failed to create network: %w", err)
			}
			networkRef = network.SelfLink
			networkResource = network
		}

		// create a subnetwork with the configured CIDR when set; attach the
		// VM's primary interface to it below. When unset the interface takes
		// the network's default subnet for the region. A non-empty SubnetID
		// takes priority: the program attaches to the existing subnetwork by
		// selfLink or name and skips subnet creation entirely.
		var subnetworkRef pulumi.StringInput
		if i.SubnetID != "" {
			subnetworkRef = pulumi.String(i.SubnetID)
		} else if i.SubnetCIDR != "" {
			subnetOpts := i.resourceOptions(gcpProvider, i.subnetLogicalName())
			if networkResource != nil {
				subnetOpts = append(subnetOpts, pulumi.DependsOn([]pulumi.Resource{networkResource}))
			}
			subnet, err := compute.NewSubnetwork(pctx, i.subnetLogicalName(), &compute.SubnetworkArgs{
				Name:        pulumi.String(i.subnetLogicalName()),
				IpCidrRange: pulumi.String(i.SubnetCIDR),
				Region:      pulumi.String(i.Region),
				Network:     networkRef,
			}, subnetOpts...)
			if err != nil {
				return fmt.Errorf("failed to create subnetwork: %w", err)
			}
			subnetworkRef = subnet.SelfLink
		}

		// render each configured ingress rule to its own google_compute_firewall.
		// Empty ports means all ports for the protocol; empty source ranges
		// falls back to the world-open range. The SSH rule is folded into
		// IngressRules by the adapter, so no standalone SSH firewall is
		// created here.
		for idx, rule := range i.IngressRules {
			name := i.ingressFirewallLogicalName(idx)
			allowArgs := &compute.FirewallAllowArgs{
				Protocol: pulumi.String(rule.Protocol),
			}
			if len(rule.Ports) > 0 {
				allowArgs.Ports = pulumi.ToStringArray(rule.Ports)
			}
			ranges := rule.SourceRanges
			if len(ranges) == 0 {
				ranges = []string{defaultIngressSourceRange}
			}
			firewallArgs := &compute.FirewallArgs{
				Name:         pulumi.String(name),
				Network:      networkRef,
				Allows:       compute.FirewallAllowArray{allowArgs},
				SourceRanges: pulumi.ToStringArray(ranges),
			}
			if rule.Description != "" {
				firewallArgs.Description = pulumi.String(rule.Description)
			}
			if _, err := compute.NewFirewall(
				pctx,
				name,
				firewallArgs,
				i.resourceOptions(gcpProvider, name)...,
			); err != nil {
				return fmt.Errorf("failed to create ingress firewall %s: %w", name, err)
			}
		}

		// build the primary network interface; attach a subnetwork when one
		// was created, and include an access_config only when the caller
		// asked for a public IP so the VM has no external address by default.
		networkInterfaceArgs := &compute.InstanceNetworkInterfaceArgs{
			Network: networkRef,
		}
		if subnetworkRef != nil {
			networkInterfaceArgs.Subnetwork = subnetworkRef
		}
		if i.AssignPublicIP {
			networkInterfaceArgs.AccessConfigs = compute.InstanceNetworkInterfaceAccessConfigArray{
				&compute.InstanceNetworkInterfaceAccessConfigArgs{},
			}
		}

		// VM instance with the generated PUBLIC key injected into ssh-keys
		// metadata. The authorized-key marshal ends with a newline, so trim it.
		instance, err := compute.NewInstance(pctx, i.instanceLogicalName(), &compute.InstanceArgs{
			Name:        pulumi.String(i.RuntimeInstanceName),
			MachineType: pulumi.String(i.MachineType),
			Zone:        pulumi.String(i.Zone),
			BootDisk: &compute.InstanceBootDiskArgs{
				InitializeParams: &compute.InstanceBootDiskInitializeParamsArgs{
					Image: pulumi.String(i.ImageID),
				},
			},
			NetworkInterfaces: compute.InstanceNetworkInterfaceArray{
				networkInterfaceArgs,
			},
			Metadata: pulumi.StringMap{
				"ssh-keys": pulumi.String(fmt.Sprintf(
					"%s:%s",
					i.SSHUser,
					// strip trailing newline from authorized_keys marshal
					strings.TrimSpace(i.sshPublicKeyAuthorized),
				)),
			},
			// mark the instance as managed by threeport.
			Labels: pulumi.StringMap{
				provider.ManagedByLabelKey: pulumi.String(provider.ManagedByLabelValue),
			},
		}, i.resourceOptions(gcpProvider, i.instanceLogicalName())...)
		if err != nil {
			return fmt.Errorf("failed to create GCE instance: %w", err)
		}

		// export only the hostname and, when a public IP was allocated, the
		// external IP. The private key must NEVER be exported: exports land in
		// the streamed and persisted state file.
		pctx.Export("hostname", instance.Name)
		if i.AssignPublicIP {
			pctx.Export("externalIP", instance.NetworkInterfaces.
				Index(pulumi.Int(0)).
				AccessConfigs().
				Index(pulumi.Int(0)).
				NatIp())
		}

		// export the primary interface's internal IP and the VPC and subnet
		// selfLink URLs so the resource inventory captures the network
		// attachments the VM ended up with, not just the inputs the caller
		// supplied. The Google Compute API normalizes network and subnetwork
		// references on read to full selfLink URLs, so these exports always
		// resolve to URLs regardless of whether the caller passed a bare name
		// or a URL as input.
		pctx.Export("internalIP", instance.NetworkInterfaces.
			Index(pulumi.Int(0)).
			NetworkIp())
		pctx.Export("attachedVPC", instance.NetworkInterfaces.
			Index(pulumi.Int(0)).
			Network())
		pctx.Export("attachedSubnet", instance.NetworkInterfaces.
			Index(pulumi.Int(0)).
			Subnetwork())

		return nil
	}
}

// instanceLogicalName returns the Pulumi logical name and GCE resource name of
// the VM instance. The two are identical and deterministic, which is what lets
// an orphaned instance be re-acquired by constructed import ID.
func (i *GceMachineInfra) instanceLogicalName() string {
	return i.RuntimeInstanceName
}

// networkLogicalName returns the Pulumi logical name and GCE resource name of
// the custom-mode network created when NetworkCIDR is set.
func (i *GceMachineInfra) networkLogicalName() string {
	return fmt.Sprintf("%s-network", i.RuntimeInstanceName)
}

// subnetLogicalName returns the Pulumi logical name and GCE resource name of
// the subnetwork created when SubnetCIDR is set.
func (i *GceMachineInfra) subnetLogicalName() string {
	return fmt.Sprintf("%s-subnet", i.RuntimeInstanceName)
}

// ingressFirewallLogicalName returns the Pulumi logical name and GCE resource
// name for the ingress firewall rule at position idx in IngressRules. The
// index keeps names deterministic across reconciles for the same rule list.
func (i *GceMachineInfra) ingressFirewallLogicalName(idx int) string {
	return fmt.Sprintf("%s-ingress-%d", i.RuntimeInstanceName, idx)
}

// resourceOptions returns the Pulumi resource options for the named resource:
// always the GCP provider, plus a pulumi.Import option when DiscoverAndAdopt
// recorded an import ID for that logical name so the deploy adopts the existing
// cloud resource instead of creating a duplicate.
func (i *GceMachineInfra) resourceOptions(gcpProvider pulumi.ProviderResource, logicalName string) []pulumi.ResourceOption {
	opts := []pulumi.ResourceOption{pulumi.Provider(gcpProvider)}
	if importID, ok := i.adoptImportIDs[logicalName]; ok {
		opts = append(opts, pulumi.Import(pulumi.ID(importID)))
	}
	return opts
}

// adoptTargets returns the deterministically named resources DiscoverAndAdopt
// probes, each paired with the Pulumi logical name the program registers it
// under so a found import ID lands against the right resource.
func (i *GceMachineInfra) adoptTargets() []adoptTarget {
	return []adoptTarget{
		{kind: adoptInstance, logicalName: i.instanceLogicalName()},
	}
}

// DiscoverAndAdopt probes the GCE compute API for each deterministically named
// resource and records an import ID for every one that already exists, so the
// next deploy adopts the orphan instead of colliding on its name. It satisfies
// AdoptableProvider and is a no-op for resources the API reports as not found.
func (i *GceMachineInfra) DiscoverAndAdopt() error {
	if err := i.validateRequiredFields(); err != nil {
		return fmt.Errorf("invalid GCE machine configuration: %w", err)
	}

	if err := gcpauth.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	ctx := context.Background()
	service, err := computev1.NewService(ctx, i.GcpClientOptions(option.WithScopes(computev1.ComputeReadonlyScope))...)
	if err != nil {
		return fmt.Errorf("failed to create GCE compute service: %w", err)
	}

	// probe each target and record the import ID of any that already exists,
	// accumulating into the adopt map the program reads when attaching imports
	for _, target := range i.adoptTargets() {
		importID, found, err := i.discoverImportID(ctx, service, target.kind)
		if err != nil {
			return fmt.Errorf("failed to discover %s: %w", target.logicalName, err)
		}
		if !found {
			continue
		}
		if i.adoptImportIDs == nil {
			i.adoptImportIDs = make(map[string]string)
		}
		i.adoptImportIDs[target.logicalName] = importID
	}

	return nil
}

// discoverImportID checks whether the resource of the given kind exists by its
// deterministic name and, if so, returns its Pulumi import ID. A 404 from the
// compute API means not found and yields found=false with no error; any other
// API error is returned to the caller.
func (i *GceMachineInfra) discoverImportID(
	ctx context.Context,
	service *computev1.Service,
	kind adoptResourceKind,
) (importID string, found bool, err error) {
	switch kind {
	case adoptInstance:
		if _, err := service.Instances.Get(i.ProjectID, i.Zone, i.instanceLogicalName()).Context(ctx).Do(); err != nil {
			if isNotFound(err) {
				return "", false, nil
			}
			return "", false, fmt.Errorf("failed to get instance: %w", err)
		}
		return fmt.Sprintf(
			"projects/%s/zones/%s/instances/%s",
			i.ProjectID, i.Zone, i.instanceLogicalName(),
		), true, nil
	default:
		return "", false, fmt.Errorf("unknown adopt resource kind: %d", kind)
	}
}

// isNotFound reports whether a compute API error is a 404, the signal that the
// probed resource does not exist and so cannot be adopted.
func isNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 404
}

// generateSSHKeyPair generates a 2048-bit RSA key pair, returning the private
// key in PKCS1 PEM form and the public key in authorized-keys form.
func generateSSHKeyPair() (privPEM, pubAuthorized string, err error) {
	// generate RSA key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	// encode private key to PEM
	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEMBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	})
	if privPEMBytes == nil {
		return "", "", errors.New("failed to encode private key to PEM")
	}

	// marshal SSH public key
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to build SSH public key: %w", err)
	}
	pubAuthorizedBytes := ssh.MarshalAuthorizedKey(pub)

	return string(privPEMBytes), string(pubAuthorizedBytes), nil
}

// ensureSSHKeyPair generates an SSH key pair when sshPublicKeyAuthorized is empty.
func (i *GceMachineInfra) ensureSSHKeyPair() error {
	if i.sshPublicKeyAuthorized != "" {
		return nil
	}

	priv, pub, err := generateSSHKeyPair()
	if err != nil {
		return err
	}
	i.sshPrivateKeyPEM = priv
	i.sshPublicKeyAuthorized = pub

	return nil
}

// captureOutputs maps the hostname and externalIP entries from the Pulumi up
// outputs onto the receiver, tolerating missing keys rather than panicking.
// The internalIP, attachedVPC, and attachedSubnet outputs feed the resource
// inventory the lifecycle adapter persists onto the abstract machine runtime
// instance; each is wrapped in a single-entry slice so the inventory shape
// generalizes to a future multi-interface program without changing the JSON.
func (i *GceMachineInfra) captureOutputs(outputs auto.OutputMap) {
	if v, ok := outputs["hostname"]; ok {
		if s, ok := v.Value.(string); ok {
			i.hostname = s
		}
	}
	if v, ok := outputs["externalIP"]; ok {
		if s, ok := v.Value.(string); ok {
			i.externalIP = s
		}
	}
	if s := captureStringOutput(outputs, "internalIP"); s != "" {
		i.internalIPs = []string{s}
	}
	if s := captureStringOutput(outputs, "attachedVPC"); s != "" {
		i.attachedVPCs = []string{s}
	}
	if s := captureStringOutput(outputs, "attachedSubnet"); s != "" {
		i.attachedSubnets = []string{s}
	}
}

// captureStringOutput reads a single string value from the Pulumi output map,
// tolerating the two shapes the automation API produces for a string export:
// a bare string, and a pointer to string that came from a StringPtrOutput.
// A missing key or an unexpected value type both return the empty string.
func captureStringOutput(outputs auto.OutputMap, key string) string {
	v, ok := outputs[key]
	if !ok {
		return ""
	}
	switch val := v.Value.(type) {
	case string:
		return val
	case *string:
		if val == nil {
			return ""
		}
		return *val
	default:
		return ""
	}
}

// BuildResourceInventory returns a generic GCP infrastructure inventory for
// the deployed VM: external and internal IPs, the VPC and subnet self-links
// the instance is attached to, the deterministic firewall names, and the
// zone and region it lives in. The lifecycle adapter persists this payload
// onto the abstract machine runtime instance so a downstream consumer can
// act on the deployed resources without provider-specific knowledge. Call
// after createInfra has populated the captured Pulumi outputs.
func (i *GceMachineInfra) BuildResourceInventory() map[string]any {
	// derive firewall names from the ingress rule index; deterministic naming
	// avoids a dedicated Pulumi output for the same information
	firewallNames := make([]string, 0, len(i.IngressRules))
	for idx := range i.IngressRules {
		firewallNames = append(firewallNames, i.ingressFirewallLogicalName(idx))
	}

	return map[string]any{
		"external_ip":       i.externalIP,
		"internal_ips":      i.internalIPs,
		"vpc_self_links":    i.attachedVPCs,
		"subnet_self_links": i.attachedSubnets,
		"firewall_names":    firewallNames,
		"zone":              i.Zone,
		"region":            i.Region,
	}
}

// CreateOutputs returns the instance hostname, public IP, and SSH private key.
// The private key is never written to Pulumi state.
func (i *GceMachineInfra) CreateOutputs() (hostname, externalIP, sshPrivateKey string) {
	return i.hostname, i.externalIP, i.sshPrivateKeyPEM
}

// SetCreateOutputs stores hostname, public IP, and SSH private key on the provider.
func (i *GceMachineInfra) SetCreateOutputs(hostname, externalIP, sshPrivateKey string) {
	i.hostname = hostname
	i.externalIP = externalIP
	i.sshPrivateKeyPEM = sshPrivateKey
}

// SeedSSHKeyPair seeds a previously persisted private key onto the provider and
// derives its authorized-keys public form, so a rebuilt provider reuses the
// stored key on the next deploy instead of minting a fresh pair and rotating
// the instance's authorized key away from it. The public key is derived rather
// than persisted separately so no extra stored field is needed.
func (i *GceMachineInfra) SeedSSHKeyPair(sshPrivateKeyPEM string) error {
	if sshPrivateKeyPEM == "" {
		return errors.New("cannot seed SSH key pair from empty private key")
	}
	signer, err := ssh.ParsePrivateKey([]byte(sshPrivateKeyPEM))
	if err != nil {
		return fmt.Errorf("failed to parse persisted SSH private key: %w", err)
	}
	i.sshPrivateKeyPEM = sshPrivateKeyPEM
	i.sshPublicKeyAuthorized = string(ssh.MarshalAuthorizedKey(signer.PublicKey()))
	return nil
}

// GcpClientOptions returns the client options for GCP SDK clients built on
// behalf of this instance. When ServiceAccountCredentials is set, the JSON key
// is threaded in per call so two concurrent operations for different service
// accounts authenticate independently rather than sharing whatever ambient
// credentials the process happens to hold. Base options such as scopes are
// preserved ahead of the credentials option. It is exported because the orphan
// reclaim client is built outside this package from the same provider.
func (i *GceMachineInfra) GcpClientOptions(base ...option.ClientOption) []option.ClientOption {
	if i.ServiceAccountCredentials == "" {
		return base
	}
	return append(base, option.WithCredentialsJSON([]byte(i.ServiceAccountCredentials)))
}

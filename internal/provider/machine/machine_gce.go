// Package machine provides a generic, reusable Google Compute Engine (GCE) VM
// infrastructure provider. It implements the threeport infra provider lifecycle
// contract by embedding the core Pulumi workspace and provisioning a single VM
// with an SSH-allow firewall rule and an injected SSH public key.
//
// This package only depends on the core provider package; the API type,
// lifecycle adapter, codegen, and controller wiring that consume these outputs
// live elsewhere.
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

// adoptResourceKind identifies which of the two deterministically named GCE
// resources an adopt target refers to, so the discover helper can branch on
// kind without string matching on logical names.
type adoptResourceKind int

const (
	// adoptInstance is the VM instance resource.
	adoptInstance adoptResourceKind = iota

	// adoptFirewall is the SSH-allow firewall rule resource.
	adoptFirewall
)

// adoptTarget pairs a resource kind with the Pulumi logical name the program
// registers it under, so DiscoverAndAdopt records each constructed import ID
// against the name the program later looks it up by.
type adoptTarget struct {
	kind        adoptResourceKind
	logicalName string
}

// defaultSSHSourceRange is the initial default ingress range for the SSH
// firewall rule when SSHSourceRanges is left empty. It is world-open; callers
// should narrow it for production by setting SSHSourceRanges explicitly.
const defaultSSHSourceRange = "0.0.0.0/0"

// GceMachineInfra provisions a single Google Compute Engine VM via Pulumi and
// implements the threeport infra provider lifecycle contract. It embeds the
// core PulumiWorkspace by value, mirroring the bare embed in the GKE provider,
// so the workspace, stack, and state-management helpers are inherited directly.
type GceMachineInfra struct {
	// PulumiWorkspace provides workspace, stack, state, and automation API
	// helpers. Embedded by value so RuntimeInstanceName/ProjectName and the
	// inherited methods are reachable on the receiver.
	provider.PulumiWorkspace

	// ProjectID is the Google Cloud project where the VM is provisioned.
	ProjectID string

	// Region is the Google Cloud region where the VM is provisioned.
	Region string

	// Zone is the Google Cloud zone where the VM is placed.
	Zone string

	// MachineType is the GCE machine type for the VM (e.g. e2-medium).
	MachineType string

	// ImageID is the boot image the VM disk is initialized from.
	ImageID string

	// NetworkID is the network or selfLink the VM and firewall attach to.
	NetworkID string

	// ServiceAccountCredentials contains the JSON key for a GCP service
	// account, used when running outside GCP where Workload Identity is
	// not available.
	ServiceAccountCredentials string

	// SSHUser is the username the generated public key is authorized for in
	// the instance ssh-keys metadata.
	SSHUser string

	// SSHSourceRanges scopes the SSH firewall ingress. When empty it defaults
	// to the world-open range; callers should narrow it for production.
	SSHSourceRanges []string

	// sshPrivateKeyPEM holds the generated RSA private key in PKCS1 PEM form.
	// It is surfaced only through CreateOutputs and is never exported into
	// Pulumi state nor written into instance metadata.
	sshPrivateKeyPEM string

	// sshPublicKeyAuthorized holds the generated public key in authorized-keys
	// form. This is the only key material injected into instance metadata.
	sshPublicKeyAuthorized string

	// hostname is captured from the Pulumi up outputs after a successful deploy.
	hostname string

	// externalIP is captured from the Pulumi up outputs after a successful deploy.
	externalIP string

	// adoptImportIDs maps a resource's Pulumi logical name to the import ID
	// DiscoverAndAdopt found for it. The program attaches pulumi.Import for
	// any resource present here so the deploy adopts an existing cloud
	// resource instead of creating a duplicate. Empty on a clean create.
	adoptImportIDs map[string]string
}

// NewGceMachineInfra builds a GCE machine provider for the named runtime
// instance, constructing the embedded workspace under the fixed "gce" pulumi
// project. Tests pass provider.WithStateDirRoot(t.TempDir()) to isolate state;
// production callers pass no options and get the default runtime state root.
func NewGceMachineInfra(name string, opts ...provider.PulumiWorkspaceOption) *GceMachineInfra {
	return &GceMachineInfra{
		PulumiWorkspace: *provider.NewPulumiWorkspace(name, "gce", opts...),
	}
}

// ensurePulumiProjectDefaults sets Pulumi project metadata when not provided by
// callers, mirroring the GKE provider's defaults shape.
func (i *GceMachineInfra) ensurePulumiProjectDefaults() {
	if i.ProjectName == "" {
		i.ProjectName = "gce"
	}
	if i.ProjectDescription == "" {
		i.ProjectDescription = "Google Compute Engine VM for Threeport"
	}
}

// syncStackConfigs updates the stack config keys from the current ProjectID and Region.
func (i *GceMachineInfra) syncStackConfigs() {
	i.StackConfigs = map[string]string{
		"gcp:project": i.ProjectID,
		"gcp:region":  i.Region,
	}
}

// sshSourceRanges returns the configured SSH ingress ranges, falling back to the
// world-open default when the field is empty.
func (i *GceMachineInfra) sshSourceRanges() []string {
	if len(i.SSHSourceRanges) == 0 {
		return []string{defaultSSHSourceRange}
	}
	return i.SSHSourceRanges
}

// validateRequiredFields returns a descriptive error naming the first required
// field that is empty, so a misconfigured provider fails fast before any auth
// or cloud call rather than mid-deploy inside the Pulumi engine.
func (i *GceMachineInfra) validateRequiredFields() error {
	// collect every missing field so the caller fixes them in one pass rather
	// than rediscovering them one failed deploy at a time
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
	if i.NetworkID == "" {
		missing = append(missing, "NetworkID")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return nil
}

// DeployInfra creates the GCE VM infrastructure. It satisfies InfraProvider.
func (i *GceMachineInfra) DeployInfra() error {
	return i.createInfra()
}

// createInfra validates configuration, ensures auth and an SSH key pair, then
// drives the Pulumi up and captures the resulting outputs onto the receiver.
func (i *GceMachineInfra) createInfra() error {
	// validate required fields first so a misconfigured provider fails before
	// any auth or cloud call, with an error naming the missing field
	if err := i.validateRequiredFields(); err != nil {
		return fmt.Errorf("invalid GCE machine configuration: %w", err)
	}

	if err := provider.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	// generate the SSH key pair outside the Pulumi program so the same key
	// material is used across preview and update, avoiding nondeterministic
	// diffs and metadata churn on every requeue
	if err := i.ensureSSHKeyPair(); err != nil {
		return fmt.Errorf("failed to ensure SSH key pair: %w", err)
	}

	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	stack, err := i.SetupStack(i.pulumiProgram())
	if err != nil {
		return fmt.Errorf("failed to set up Pulumi workspace: %w", err)
	}

	upResult, err := i.RunUp(context.Background(), stack)
	if err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	i.captureOutputs(upResult.Outputs)

	return nil
}

// DestroyInfra tears down the GCE VM infrastructure. It satisfies InfraProvider.
func (i *GceMachineInfra) DestroyInfra() error {
	if err := provider.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	if err := i.DestroyStack(); err != nil {
		return fmt.Errorf("failed to destroy Pulumi stack: %w", err)
	}

	return nil
}

// GetStackState returns the state of the GCE stack as a JSON object, applying
// project defaults and stack configs before delegating to the embedded method.
func (i *GceMachineInfra) GetStackState() (*datatypes.JSON, error) {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.GetStackState()
}

// SetStackState restores Pulumi state from a JSON object, applying project
// defaults and stack configs before delegating to the embedded method.
func (i *GceMachineInfra) SetStackState(state *datatypes.JSON) error {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.SetStackState(state)
}

// pulumiProgram defines the Pulumi resources for the GCE VM stack: a firewall
// rule allowing SSH ingress and the VM instance itself, with the generated
// public key injected into the instance ssh-keys metadata.
func (i *GceMachineInfra) pulumiProgram() pulumi.RunFunc {
	return func(pctx *pulumi.Context) error {
		gcpProvider, err := gcp.NewProvider(pctx, "gcp-provider", &gcp.ProviderArgs{
			Project: pulumi.String(i.ProjectID),
			Region:  pulumi.String(i.Region),
		})
		if err != nil {
			return fmt.Errorf("failed to create GCP provider: %w", err)
		}

		// SSH-allow firewall rule scoped to the configured source ranges.
		// The firewall resource has no labels field, so the managed-by label
		// is applied only to labelable resources such as the instance.
		sourceRanges := pulumi.ToStringArray(i.sshSourceRanges())
		_, err = compute.NewFirewall(pctx, i.firewallLogicalName(), &compute.FirewallArgs{
			Name:    pulumi.String(i.firewallLogicalName()),
			Network: pulumi.String(i.NetworkID),
			Allows: compute.FirewallAllowArray{
				&compute.FirewallAllowArgs{
					Protocol: pulumi.String("tcp"),
					Ports:    pulumi.StringArray{pulumi.String("22")},
				},
			},
			SourceRanges: sourceRanges,
		}, i.resourceOptions(gcpProvider, i.firewallLogicalName())...)
		if err != nil {
			return fmt.Errorf("failed to create SSH firewall rule: %w", err)
		}

		// VM instance with an ephemeral external IP (empty access config) and
		// the generated PUBLIC key injected into ssh-keys metadata. The
		// authorized-key marshal ends with a newline, so trim it.
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
				&compute.InstanceNetworkInterfaceArgs{
					Network: pulumi.String(i.NetworkID),
					AccessConfigs: compute.InstanceNetworkInterfaceAccessConfigArray{
						&compute.InstanceNetworkInterfaceAccessConfigArgs{},
					},
				},
			},
			Metadata: pulumi.StringMap{
				"ssh-keys": pulumi.String(fmt.Sprintf(
					"%s:%s",
					i.SSHUser,
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

		// export only the hostname and external IP. The private key must NEVER
		// be exported: exports land in the streamed and persisted state file.
		pctx.Export("hostname", instance.Name)
		pctx.Export("externalIP", instance.NetworkInterfaces.
			Index(pulumi.Int(0)).
			AccessConfigs().
			Index(pulumi.Int(0)).
			NatIp())

		return nil
	}
}

// instanceLogicalName returns the Pulumi logical name and GCE resource name of
// the VM instance. The two are identical and deterministic, which is what lets
// an orphaned instance be re-acquired by constructed import ID.
func (i *GceMachineInfra) instanceLogicalName() string {
	return i.RuntimeInstanceName
}

// firewallLogicalName returns the Pulumi logical name and GCE resource name of
// the SSH-allow firewall rule. It is deterministic for the same reason the
// instance name is.
func (i *GceMachineInfra) firewallLogicalName() string {
	return fmt.Sprintf("%s-ssh", i.RuntimeInstanceName)
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

// adoptTargets returns the two deterministically named resources DiscoverAndAdopt
// probes, each paired with the Pulumi logical name the program registers it
// under so a found import ID lands against the right resource.
func (i *GceMachineInfra) adoptTargets() []adoptTarget {
	return []adoptTarget{
		{kind: adoptInstance, logicalName: i.instanceLogicalName()},
		{kind: adoptFirewall, logicalName: i.firewallLogicalName()},
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

	if err := provider.EnsureGCPAuth(i.ServiceAccountCredentials); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	ctx := context.Background()
	service, err := computev1.NewService(ctx, option.WithScopes(computev1.ComputeReadonlyScope))
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
	case adoptFirewall:
		if _, err := service.Firewalls.Get(i.ProjectID, i.firewallLogicalName()).Context(ctx).Do(); err != nil {
			if isNotFound(err) {
				return "", false, nil
			}
			return "", false, fmt.Errorf("failed to get firewall: %w", err)
		}
		return fmt.Sprintf(
			"projects/%s/global/firewalls/%s",
			i.ProjectID, i.firewallLogicalName(),
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
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	privDER := x509.MarshalPKCS1PrivateKey(key)
	privPEMBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	})
	if privPEMBytes == nil {
		return "", "", fmt.Errorf("failed to encode private key to PEM")
	}

	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to build SSH public key: %w", err)
	}
	pubAuthorizedBytes := ssh.MarshalAuthorizedKey(pub)

	return string(privPEMBytes), string(pubAuthorizedBytes), nil
}

// ensureSSHKeyPair generates a key pair only when the public key is empty, so
// re-deploys driven by requeue reuse the stored key material idempotently.
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
}

// CreateOutputs returns the captured hostname and external IP together with the
// generated SSH private key. It is the exported accessor a downstream adapter
// uses to persist create outputs, and the sole surface that exposes the private
// key, which is never exported into Pulumi state.
func (i *GceMachineInfra) CreateOutputs() (hostname, externalIP, sshPrivateKey string) {
	return i.hostname, i.externalIP, i.sshPrivateKeyPEM
}

// SetCreateOutputs sets the values that CreateOutputs() returns. Use it to
// populate a provider with previously persisted outputs, or to supply known
// values in tests that exercise callers of CreateOutputs().
func (i *GceMachineInfra) SetCreateOutputs(hostname, externalIP, sshPrivateKey string) {
	i.hostname = hostname
	i.externalIP = externalIP
	i.sshPrivateKeyPEM = sshPrivateKey
}

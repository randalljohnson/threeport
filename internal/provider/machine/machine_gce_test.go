package machine

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/crypto/ssh"
	"google.golang.org/api/googleapi"
	"gorm.io/datatypes"

	"github.com/threeport/threeport/internal/provider"
)

// requirePulumi skips the calling test when the pulumi CLI is not on PATH.
// SetStackState and GetStackState shell out via auto.NewLocalWorkspace and
// auto.UpsertStack, so the real state-write path cannot run without the CLI.
func requirePulumi(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("pulumi"); err != nil {
		t.Skip("pulumi CLI not found on PATH; skipping pulumi-backed test")
	}
}

// recordedResource captures the type token, logical name, inputs, and import
// ID of one resource registered during a mocked Pulumi program run. The import
// ID is the physical identifier Pulumi passes when a resource carries a
// pulumi.Import option, so tests can assert the adopt path attached it.
type recordedResource struct {
	typeToken string
	name      string
	inputs    map[string]any
	importID  string
}

// recordingMocks is a MockResourceMonitor that records every NewResource call
// so tests can assert on the resources a pulumiProgram registers.
type recordingMocks struct {
	mu        sync.Mutex
	resources []recordedResource
}

func (m *recordingMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.resources = append(m.resources, recordedResource{
		typeToken: args.TypeToken,
		name:      args.Name,
		inputs:    args.Inputs.Mappable(),
		importID:  args.ID,
	})
	m.mu.Unlock()
	return args.Name + "-id", args.Inputs, nil
}

func (m *recordingMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return resource.PropertyMap{}, nil
}

// byType returns the recorded resources whose type token equals the argument.
func (m *recordingMocks) byType(typeToken string) []recordedResource {
	var out []recordedResource
	for _, r := range m.resources {
		if r.typeToken == typeToken {
			out = append(out, r)
		}
	}
	return out
}

const (
	instanceTypeToken    = "gcp:compute/instance:Instance"
	firewallTypeToken    = "gcp:compute/firewall:Firewall"
	gcpProviderTypeToken = "pulumi:providers:gcp"
)

// newTestInfra builds a fully-configured provider for program-level tests.
// Seeds a single SSH ingress rule so program-level tests observe the same
// firewall shape the adapter would inject in production, where SSH is one
// entry in IngressRules and the pulumi program renders one firewall per rule.
func newTestInfra(name string) *GceMachineInfra {
	return &GceMachineInfra{
		PulumiWorkspace: provider.PulumiWorkspace{
			RuntimeInstanceName: name,
			ProjectName:         "gce",
		},
		ProjectID:   "test-project",
		Region:      "us-central1",
		Zone:        "us-central1-a",
		MachineType: "e2-medium",
		ImageID:     "debian-cloud/debian-12",
		NetworkID:   "default",
		SSHUser:     "threeport",
		IngressRules: []GceIngressRule{{
			Protocol:     "tcp",
			Ports:        []string{"22"},
			SourceRanges: []string{"0.0.0.0/0"},
			Description:  "ssh",
		}},
	}
}

func TestGenerateSSHKeyPair_ValidFormats(t *testing.T) {
	priv, pub, err := generateSSHKeyPair()
	if err != nil {
		t.Fatalf("generateSSHKeyPair returned error: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatalf("expected non-empty key material, got priv=%q pub=%q", priv, pub)
	}

	// private key parses as PKCS1 PEM
	block, _ := pem.Decode([]byte(priv))
	if block == nil {
		t.Fatal("private key did not decode as PEM")
	}
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("private key did not parse as PKCS1: %v", err)
	}

	// public key parses via authorized-keys
	pubKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pub))
	if err != nil {
		t.Fatalf("public key did not parse as authorized key: %v", err)
	}

	// a signer built from the private key round-trips sign/verify against the
	// parsed public key
	signer, err := ssh.NewSignerFromKey(rsaKey)
	if err != nil {
		t.Fatalf("failed to build signer from private key: %v", err)
	}
	msg := []byte("threeport gce ssh round trip")
	sig, err := signer.Sign(nil, msg)
	if err != nil {
		t.Fatalf("failed to sign message: %v", err)
	}
	if err := pubKey.Verify(msg, sig); err != nil {
		t.Fatalf("public key failed to verify signature from private key: %v", err)
	}
}

func TestEnsureSSHKeyPair_IdempotentAcrossCalls(t *testing.T) {
	i := newTestInfra("idempotent")

	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("first ensureSSHKeyPair: %v", err)
	}
	firstPriv := i.sshPrivateKeyPEM
	firstPub := i.sshPublicKeyAuthorized
	if firstPriv == "" || firstPub == "" {
		t.Fatal("expected key material after first call")
	}

	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("second ensureSSHKeyPair: %v", err)
	}
	if i.sshPrivateKeyPEM != firstPriv || i.sshPublicKeyAuthorized != firstPub {
		t.Fatal("second ensureSSHKeyPair overwrote existing key material")
	}

	// a pre-seeded public key is never overwritten
	seeded := newTestInfra("seeded")
	seeded.sshPublicKeyAuthorized = "ssh-rsa AAAAseeded threeport"
	if err := seeded.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair on seeded: %v", err)
	}
	if seeded.sshPublicKeyAuthorized != "ssh-rsa AAAAseeded threeport" {
		t.Fatal("ensureSSHKeyPair overwrote a pre-seeded public key")
	}
	if seeded.sshPrivateKeyPEM != "" {
		t.Fatal("ensureSSHKeyPair generated a private key when the public key was already set")
	}
}

func TestPulumiProgram_CreatesInstanceAndFirewall(t *testing.T) {
	i := newTestInfra("create-test")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}

	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	instances := mocks.byType(instanceTypeToken)
	if len(instances) != 1 {
		t.Fatalf("expected exactly 1 instance, got %d", len(instances))
	}
	firewalls := mocks.byType(firewallTypeToken)
	if len(firewalls) != 1 {
		t.Fatalf("expected exactly 1 firewall, got %d", len(firewalls))
	}

	inst := instances[0]
	if got := inst.inputs["machineType"]; got != "e2-medium" {
		t.Errorf("instance machineType = %v, want e2-medium", got)
	}
	if got := inst.inputs["zone"]; got != "us-central1-a" {
		t.Errorf("instance zone = %v, want us-central1-a", got)
	}

	// firewall source ranges match the seeded ingress rule
	if got := firewallSourceRanges(t, firewalls[0]); !equalStringSlices(got, i.IngressRules[0].SourceRanges) {
		t.Errorf("firewall sourceRanges = %v, want %v", got, i.IngressRules[0].SourceRanges)
	}
	// firewall allows tcp/22 from the seeded ingress rule
	if !firewallAllowsTCP22(t, firewalls[0]) {
		t.Errorf("firewall does not allow tcp/22: %v", firewalls[0].inputs["allows"])
	}
}

func TestPulumiProgram_InjectsSSHKeyMetadata(t *testing.T) {
	i := newTestInfra("metadata-test")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}

	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	instances := mocks.byType(instanceTypeToken)
	if len(instances) != 1 {
		t.Fatalf("expected exactly 1 instance, got %d", len(instances))
	}

	sshKeys := instanceSSHKeysMetadata(t, instances[0])
	wantPrefix := i.SSHUser + ":"
	if !strings.HasPrefix(sshKeys, wantPrefix) {
		t.Errorf("ssh-keys metadata = %q, want prefix %q", sshKeys, wantPrefix)
	}
	if !strings.Contains(sshKeys, strings.TrimSpace(i.sshPublicKeyAuthorized)) {
		t.Errorf("ssh-keys metadata does not contain the generated public key")
	}
	// metadata must carry the PUBLIC key only, never the private PEM
	if strings.Contains(sshKeys, "PRIVATE KEY") {
		t.Errorf("ssh-keys metadata contains a private key: %q", sshKeys)
	}
}

func TestPulumiProgram_DoesNotExportPrivateKey(t *testing.T) {
	i := newTestInfra("no-export-test")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}
	if i.sshPrivateKeyPEM == "" {
		t.Fatal("expected a generated private key on the receiver")
	}

	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	// the private key must never enter the Pulumi graph: scan every input of
	// every registered resource for the PEM marker. This is strictly stronger
	// than "not exported" because exports are derived from resource outputs.
	for _, r := range mocks.resources {
		if containsPrivateKey(r.inputs, i.sshPrivateKeyPEM) {
			t.Fatalf("resource %s (%s) inputs contain the private key", r.name, r.typeToken)
		}
	}
}

// TestResourceOptions_AttachesImportWhenAdopting asserts the program attaches a
// pulumi.Import option carrying the recorded import ID to a resource whose
// logical name DiscoverAndAdopt found in the cloud, and leaves resources with
// no recorded ID untouched so a partial adopt imports only the orphan.
func TestResourceOptions_AttachesImportWhenAdopting(t *testing.T) {
	// build a configured provider and seed an adopt ID for the instance only,
	// modeling an interrupted create where the VM was made but the firewall was not
	i := newTestInfra("adopt-test")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}
	wantInstanceID := "projects/test-project/zones/us-central1-a/instances/adopt-test"
	i.adoptImportIDs = map[string]string{
		i.instanceLogicalName(): wantInstanceID,
	}

	// run the program under the recording monitor so the import ID Pulumi
	// receives for each resource is observable
	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	// the instance carries the seeded import ID, marking it adopted not created
	instances := mocks.byType(instanceTypeToken)
	if len(instances) != 1 {
		t.Fatalf("expected exactly 1 instance, got %d", len(instances))
	}
	if instances[0].importID != wantInstanceID {
		t.Errorf("instance importID = %q, want %q", instances[0].importID, wantInstanceID)
	}

	// the firewall has no recorded ID, so it is created fresh with no import
	firewalls := mocks.byType(firewallTypeToken)
	if len(firewalls) != 1 {
		t.Fatalf("expected exactly 1 firewall, got %d", len(firewalls))
	}
	if firewalls[0].importID != "" {
		t.Errorf("firewall importID = %q, want empty (no adopt)", firewalls[0].importID)
	}
}

// TestResourceOptions_NoImportOnCleanCreate asserts that with no recorded adopt
// IDs the program imports nothing, so an ordinary create still creates both
// resources rather than attempting to import absent cloud resources.
func TestResourceOptions_NoImportOnCleanCreate(t *testing.T) {
	// a provider with an empty adopt map models a clean first create
	i := newTestInfra("clean-create")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}

	// run the program and confirm no resource received an import ID
	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	for _, r := range mocks.resources {
		if r.importID != "" {
			t.Errorf("resource %q (%s) carried importID %q on a clean create", r.name, r.typeToken, r.importID)
		}
	}
}

// TestAdoptTargets_DeterministicLogicalNames asserts the adopt targets pair each
// resource kind with the same logical name the program registers it under, so a
// found import ID lands on the resource that gets imported.
func TestAdoptTargets_DeterministicLogicalNames(t *testing.T) {
	// the instance name derives deterministically from the runtime instance
	// name, which is what makes the constructed import ID valid
	i := newTestInfra("targets")
	targets := i.adoptTargets()
	if len(targets) != 1 {
		t.Fatalf("expected 1 adopt target, got %d", len(targets))
	}

	// the instance kind maps to its program logical name
	byKind := map[adoptResourceKind]string{}
	for _, target := range targets {
		byKind[target.kind] = target.logicalName
	}
	if got := byKind[adoptInstance]; got != "targets" {
		t.Errorf("instance target logical name = %q, want %q", got, "targets")
	}
}

// TestIsNotFound_DistinguishesFourOhFour asserts only a 404 from the compute API
// reads as not found, so DiscoverAndAdopt skips an absent resource yet surfaces
// other API errors instead of silently treating them as not found.
func TestIsNotFound_DistinguishesFourOhFour(t *testing.T) {
	// a 404 is the not-found signal that lets adopt skip a missing resource
	if !isNotFound(&googleapi.Error{Code: 404}) {
		t.Error("expected a 404 googleapi error to read as not found")
	}
	// a 403 is a real failure, not a not-found, and must not be swallowed
	if isNotFound(&googleapi.Error{Code: 403}) {
		t.Error("a 403 googleapi error must not read as not found")
	}
	// a wrapped 404 still reads as not found via errors.As unwrapping
	wrapped := fmt.Errorf("get instance: %w", &googleapi.Error{Code: 404})
	if !isNotFound(wrapped) {
		t.Error("expected a wrapped 404 to read as not found")
	}
	// a non-api error is not a not-found
	if isNotFound(errors.New("connection reset")) {
		t.Error("a plain error must not read as not found")
	}
}

func TestCaptureOutputs_MapsHostnameAndIP(t *testing.T) {
	i := newTestInfra("capture-test")

	outputs := auto.OutputMap{
		"hostname":   auto.OutputValue{Value: "vm-host"},
		"externalIP": auto.OutputValue{Value: "203.0.113.7"},
	}
	i.captureOutputs(outputs)
	if i.hostname != "vm-host" {
		t.Errorf("hostname = %q, want vm-host", i.hostname)
	}
	if i.externalIP != "203.0.113.7" {
		t.Errorf("externalIP = %q, want 203.0.113.7", i.externalIP)
	}

	// missing keys leave fields empty without panic
	empty := newTestInfra("capture-empty")
	empty.captureOutputs(auto.OutputMap{})
	if empty.hostname != "" || empty.externalIP != "" {
		t.Errorf("expected empty fields, got hostname=%q externalIP=%q", empty.hostname, empty.externalIP)
	}
}

func TestCreateOutputs_SurfacesHostnameIPKey(t *testing.T) {
	i := newTestInfra("outputs-test")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}
	i.captureOutputs(auto.OutputMap{
		"hostname":   auto.OutputValue{Value: "vm-host"},
		"externalIP": auto.OutputValue{Value: "203.0.113.7"},
	})

	hostname, externalIP, sshPrivateKey := i.CreateOutputs()
	if hostname != "vm-host" {
		t.Errorf("CreateOutputs hostname = %q, want vm-host", hostname)
	}
	if externalIP != "203.0.113.7" {
		t.Errorf("CreateOutputs externalIP = %q, want 203.0.113.7", externalIP)
	}
	if sshPrivateKey != i.sshPrivateKeyPEM {
		t.Error("CreateOutputs did not return the generated private key")
	}
	if !strings.Contains(sshPrivateKey, "PRIVATE KEY") {
		t.Errorf("CreateOutputs private key is not a PEM: %q", sshPrivateKey)
	}
}

func TestGceInfra_SatisfiesStreamableRefreshable(t *testing.T) {
	root := t.TempDir()
	i := NewGceMachineInfra("x", provider.WithStateDirRoot(root))

	path, err := i.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath: %v", err)
	}
	wantSuffix := filepath.Join(".pulumi", "stacks", "gce", "x.json")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Errorf("state file path = %q, want suffix %q", path, wantSuffix)
	}
	if !strings.HasPrefix(path, root) {
		t.Errorf("state file path = %q, want it under temp root %q", path, root)
	}
}

func TestNewGceMachineInfra_BuildsWorkspaceWithStateDirRoot(t *testing.T) {
	root := t.TempDir()
	a := NewGceMachineInfra("alpha", provider.WithStateDirRoot(root))
	b := NewGceMachineInfra("beta", provider.WithStateDirRoot(root))

	pathA, err := a.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath alpha: %v", err)
	}
	pathB, err := b.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath beta: %v", err)
	}
	if pathA == pathB {
		t.Errorf("expected distinct state paths, both resolved to %q", pathA)
	}
	if !strings.HasPrefix(pathA, root) || !strings.HasPrefix(pathB, root) {
		t.Errorf("expected both paths under root %q, got %q and %q", root, pathA, pathB)
	}
}

func TestSetStackState_AppliesProjectDefaults(t *testing.T) {
	requirePulumi(t)
	root := t.TempDir()
	// construct with an EMPTY ProjectName to prove the wrapper applies defaults
	i := &GceMachineInfra{
		PulumiWorkspace: provider.PulumiWorkspace{
			RuntimeInstanceName: "defaults",
		},
	}
	// inject the state-dir root onto the embedded workspace
	provider.WithStateDirRoot(root)(&i.PulumiWorkspace)

	blob := datatypes.JSON([]byte(`{"version":3,"checkpoint":{"stack":"gce/defaults"}}`))
	if err := i.SetStackState(&blob); err != nil {
		t.Fatalf("SetStackState: %v", err)
	}

	// after defaults ran the path resolves under stacks/gce, not a malformed stacks//
	path, err := i.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath: %v", err)
	}
	if !strings.Contains(path, filepath.Join("stacks", "gce", "defaults.json")) {
		t.Errorf("state path = %q, want it under stacks/gce", path)
	}
}

func TestStackStateRoundTrip_CheckpointFormat(t *testing.T) {
	requirePulumi(t)
	root := t.TempDir()
	i := NewGceMachineInfra("roundtrip", provider.WithStateDirRoot(root))

	// checkpoint-format blob: no top-level "deployment" key, so SetStackState
	// takes the direct atomic-write branch rather than stack.Import
	blob := datatypes.JSON([]byte(`{"version":3,"checkpoint":{"stack":"gce/roundtrip","latest":{}}}`))
	if err := i.SetStackState(&blob); err != nil {
		t.Fatalf("SetStackState: %v", err)
	}

	readBack, err := i.ReadStateFile()
	if err != nil {
		t.Fatalf("ReadStateFile: %v", err)
	}
	if readBack == nil {
		t.Fatal("ReadStateFile returned nil after SetStackState")
	}
	if string(*readBack) != string(blob) {
		t.Errorf("read-back state != written state\n got: %s\nwant: %s", *readBack, blob)
	}

	// the .tmp file from the atomic write must be gone
	path, err := i.GetStateFilePath()
	if err != nil {
		t.Fatalf("GetStateFilePath: %v", err)
	}
	if exists(path + ".tmp") {
		t.Errorf("temp state file %q was not cleaned up", path+".tmp")
	}
}

// TestIngressRule_SourceRangesOverrideReachesFirewall drives the pulumi program
// with an explicit ingress rule and asserts the configured source ranges reach
// the firewall args under mocks. Replaces the earlier SSH-firewall-focused test:
// the SSH firewall is no longer a standalone resource, so this exercises the
// generic ingress-rule pipeline the SSH rule now folds into.
func TestIngressRule_SourceRangesOverrideReachesFirewall(t *testing.T) {
	override := newTestInfra("override-ranges")
	override.IngressRules = []GceIngressRule{{
		Protocol:     "tcp",
		Ports:        []string{"22"},
		SourceRanges: []string{"10.0.0.0/8"},
		Description:  "ssh",
	}}
	if err := override.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}
	mocks := &recordingMocks{}
	if err := pulumi.RunErr(override.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}
	firewalls := mocks.byType(firewallTypeToken)
	if len(firewalls) != 1 {
		t.Fatalf("expected exactly 1 firewall, got %d", len(firewalls))
	}
	if got := firewallSourceRanges(t, firewalls[0]); !equalStringSlices(got, []string{"10.0.0.0/8"}) {
		t.Errorf("firewall sourceRanges = %v, want [10.0.0.0/8]", got)
	}
}

func TestDeployInfra_MissingRequiredFields(t *testing.T) {
	// base provides a configuration valid enough that each case's single
	// mutation isolates the field the case is exercising; NetworkID is
	// seeded so the network-resolution rule is satisfied by default
	base := func() *GceMachineInfra {
		return &GceMachineInfra{
			PulumiWorkspace: provider.PulumiWorkspace{RuntimeInstanceName: "validate"},
			ProjectID:       "p",
			Zone:            "z",
			MachineType:     "m",
			ImageID:         "img",
			SSHUser:         "u",
			NetworkID:       "default",
		}
	}
	cases := []struct {
		name  string
		mut   func(*GceMachineInfra)
		field string
	}{
		{"missing runtime instance name", func(i *GceMachineInfra) { i.RuntimeInstanceName = "" }, "RuntimeInstanceName"},
		{"missing project id", func(i *GceMachineInfra) { i.ProjectID = "" }, "ProjectID"},
		{"missing zone", func(i *GceMachineInfra) { i.Zone = "" }, "Zone"},
		{"missing machine type", func(i *GceMachineInfra) { i.MachineType = "" }, "MachineType"},
		{"missing image id", func(i *GceMachineInfra) { i.ImageID = "" }, "ImageID"},
		{"missing ssh user", func(i *GceMachineInfra) { i.SSHUser = "" }, "SSHUser"},
		{"missing network id", func(i *GceMachineInfra) { i.NetworkID = "" }, "NetworkID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// mutate a single field to empty and confirm the validator names
			// that specific field in the accumulated error string
			i := base()
			tc.mut(i)
			err := i.DeployInfra()
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.field)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name the missing field %q", err.Error(), tc.field)
			}
		})
	}
}

// TestDeployInfra_NetworkFieldsExclusive covers the mutually-exclusive rule
// between NetworkID and NetworkCIDR: neither set is a missing-field error, both
// set is an exclusivity error, and either one set alone passes validation
// through to the next stage.
func TestDeployInfra_NetworkFieldsExclusive(t *testing.T) {
	// base leaves both network fields unset so each case controls the pair
	base := func() *GceMachineInfra {
		return &GceMachineInfra{
			PulumiWorkspace: provider.PulumiWorkspace{RuntimeInstanceName: "validate"},
			ProjectID:       "p",
			Zone:            "z",
			MachineType:     "m",
			ImageID:         "img",
			SSHUser:         "u",
		}
	}

	// neither field set: reject with the required-field accumulated message
	// naming the network resolution requirement
	t.Run("neither network id nor cidr", func(t *testing.T) {
		i := base()
		err := i.DeployInfra()
		if err == nil {
			t.Fatal("expected error when both NetworkID and NetworkCIDR are unset")
		}
		if !strings.Contains(err.Error(), "NetworkID or NetworkCIDR") {
			t.Errorf("error %q does not name the network resolution requirement", err.Error())
		}
	})

	// both fields set: reject with an exclusivity error naming both fields so
	// the operator can see which pair conflicts
	t.Run("both network id and cidr", func(t *testing.T) {
		i := base()
		i.NetworkID = "default"
		i.NetworkCIDR = "10.0.0.0/16"
		err := i.DeployInfra()
		if err == nil {
			t.Fatal("expected error when both NetworkID and NetworkCIDR are set")
		}
		if !strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("error %q does not name the exclusivity rule", err.Error())
		}
		if !strings.Contains(err.Error(), "NetworkID") || !strings.Contains(err.Error(), "NetworkCIDR") {
			t.Errorf("error %q does not name both conflicting fields", err.Error())
		}
	})

	// only NetworkID set: passes validation, so any subsequent error is not the
	// validator complaining about the network fields
	t.Run("only network id passes validation", func(t *testing.T) {
		i := base()
		i.NetworkID = "default"
		err := i.DeployInfra()
		if err != nil && strings.Contains(err.Error(), "missing required fields") {
			t.Errorf("validator rejected NetworkID-only config: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("validator rejected NetworkID-only config as exclusive: %v", err)
		}
	})

	// only NetworkCIDR set: same shape as above; NetworkID may legitimately be
	// empty when the program is meant to create a new custom-mode network
	t.Run("only network cidr passes validation", func(t *testing.T) {
		i := base()
		i.NetworkCIDR = "10.0.0.0/16"
		err := i.DeployInfra()
		if err != nil && strings.Contains(err.Error(), "missing required fields") {
			t.Errorf("validator rejected NetworkCIDR-only config: %v", err)
		}
		if err != nil && strings.Contains(err.Error(), "mutually exclusive") {
			t.Errorf("validator rejected NetworkCIDR-only config as exclusive: %v", err)
		}
	})
}

// ---- test helpers ----

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// firewallSourceRanges extracts the sourceRanges input as a []string.
func firewallSourceRanges(t *testing.T, r recordedResource) []string {
	t.Helper()
	raw, ok := r.inputs["sourceRanges"]
	if !ok {
		t.Fatal("firewall has no sourceRanges input")
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("sourceRanges is not a slice: %T", raw)
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			t.Fatalf("sourceRanges entry is not a string: %T", it)
		}
		out = append(out, s)
	}
	return out
}

// firewallAllowsTCP22 reports whether the firewall allows tcp on port 22.
func firewallAllowsTCP22(t *testing.T, r recordedResource) bool {
	t.Helper()
	raw, ok := r.inputs["allows"]
	if !ok {
		return false
	}
	items, ok := raw.([]any)
	if !ok {
		t.Fatalf("allows is not a slice: %T", raw)
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if m["protocol"] != "tcp" {
			continue
		}
		ports, ok := m["ports"].([]any)
		if !ok {
			continue
		}
		for _, p := range ports {
			if p == "22" {
				return true
			}
		}
	}
	return false
}

// instanceSSHKeysMetadata extracts metadata["ssh-keys"] from an instance input.
func instanceSSHKeysMetadata(t *testing.T, r recordedResource) string {
	t.Helper()
	metaRaw, ok := r.inputs["metadata"]
	if !ok {
		t.Fatal("instance has no metadata input")
	}
	meta, ok := metaRaw.(map[string]any)
	if !ok {
		t.Fatalf("metadata is not a map: %T", metaRaw)
	}
	val, ok := meta["ssh-keys"]
	if !ok {
		t.Fatal("metadata has no ssh-keys entry")
	}
	s, ok := val.(string)
	if !ok {
		t.Fatalf("ssh-keys is not a string: %T", val)
	}
	return s
}

// containsPrivateKey reports whether any value in the nested input map contains
// the private key PEM or the PEM marker.
func containsPrivateKey(inputs map[string]any, privPEM string) bool {
	return valueContains(inputs, privPEM) || valueContains(inputs, "PRIVATE KEY")
}

// valueContains recursively searches a JSON-like value for a substring.
func valueContains(v any, needle string) bool {
	switch t := v.(type) {
	case string:
		return strings.Contains(t, needle)
	case map[string]any:
		for _, e := range t {
			if valueContains(e, needle) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if valueContains(e, needle) {
				return true
			}
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for idx := range a {
		if a[idx] != b[idx] {
			return false
		}
	}
	return true
}

// TestPulumiProgram_AssertsLabelableResourcesCarryManagedByLabel asserts every
// labelable resource the GCE program registers carries the managed-by label set
// to the threeport value, while resources that cannot carry labels are the
// documented exemptions: the firewall, whose type has no labels field, and the
// cloud-provider meta-resource, which carries no user labels. It rejects any
// resource type the allowlist does not account for, so a future resource cannot
// ship unlabeled without a conscious decision.
func TestPulumiProgram_AssertsLabelableResourcesCarryManagedByLabel(t *testing.T) {
	// build a fully-configured provider and a deterministic key pair so the
	// program runs without touching the network
	i := newTestInfra("label-audit")
	if err := i.ensureSSHKeyPair(); err != nil {
		t.Fatalf("ensureSSHKeyPair: %v", err)
	}

	// run the real program under the recording monitor so the audit sees the
	// exact inputs every resource is registered with
	mocks := &recordingMocks{}
	if err := pulumi.RunErr(i.pulumiProgram(), pulumi.WithMocks("gce", "test-stack", mocks)); err != nil {
		t.Fatalf("RunErr: %v", err)
	}

	// the program must register at least one resource; an empty graph would let
	// every per-resource assertion below pass vacuously
	if len(mocks.resources) == 0 {
		t.Fatal("program registered no resources; nothing to audit")
	}

	// classify each registered type as labelable or exempt; a type missing from
	// this map fails the audit so a new resource forces a labelable-or-exempt
	// decision rather than slipping through unlabeled
	type labelExpectation struct {
		labelable    bool
		exemptReason string
	}
	allowlist := map[string]labelExpectation{
		instanceTypeToken: {labelable: true},
		firewallTypeToken: {
			labelable:    false,
			exemptReason: "firewall rules have no labels field, so the managed-by label cannot be applied",
		},
		gcpProviderTypeToken: {
			labelable:    false,
			exemptReason: "the cloud-provider meta-resource carries no user labels",
		},
	}

	// track that the labelable instance and the exempt firewall both appeared,
	// so a program that silently stops creating either still fails the audit
	sawLabelable := false
	sawFirewall := false

	for _, r := range mocks.resources {
		// reject any resource type the allowlist does not account for
		exp, ok := allowlist[r.typeToken]
		if !ok {
			t.Errorf("resource %q (%s) is not in the label allowlist; classify it as labelable or exempt", r.name, r.typeToken)
			continue
		}

		// exempt types must declare a reason and are skipped without a label check
		if !exp.labelable {
			if r.typeToken == firewallTypeToken {
				sawFirewall = true
			}
			if exp.exemptReason == "" {
				t.Errorf("resource type %s is exempt but carries no documented reason", r.typeToken)
			}
			continue
		}

		// labelable types must carry the managed-by label set to the threeport value
		sawLabelable = true
		labels := instanceLabels(t, r)
		if got := labels[provider.ManagedByLabelKey]; got != provider.ManagedByLabelValue {
			t.Errorf("resource %q (%s) labels[%q] = %q, want %q", r.name, r.typeToken, provider.ManagedByLabelKey, got, provider.ManagedByLabelValue)
		}
	}

	// confirm both the labelable instance and the exempt firewall were observed
	if !sawLabelable {
		t.Error("no labelable resource was registered; expected at least the VM instance")
	}
	if !sawFirewall {
		t.Error("the exempt firewall resource was not registered")
	}
}

// instanceLabels extracts the labels input of a recorded resource as a
// map[string]string, failing the test when the input is absent or the wrong
// shape so a missing labels map surfaces as a clear assertion, not a nil panic.
func instanceLabels(t *testing.T, r recordedResource) map[string]string {
	t.Helper()
	raw, ok := r.inputs["labels"]
	if !ok {
		t.Fatalf("resource %q (%s) has no labels input", r.name, r.typeToken)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("resource %q labels is not a map: %T", r.name, raw)
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("resource %q label %q is not a string: %T", r.name, k, v)
		}
		out[k] = s
	}
	return out
}

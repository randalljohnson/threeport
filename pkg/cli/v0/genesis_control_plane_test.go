package v0

import (
	"errors"
	"os"
	"strings"
	"testing"

	v0 "github.com/threeport/threeport/pkg/api/v0"
	threeport "github.com/threeport/threeport/pkg/threeport-installer/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestValidateCreateGenesisControlPlaneFlagsAcceptsSupportedInputs covers the
// happy paths and every error branch of the flag validator.
func TestValidateCreateGenesisControlPlaneFlagsAcceptsSupportedInputs(t *testing.T) {
	longName := strings.Repeat("a", threeport.InstanceNameMaxLength+1)
	exactName := strings.Repeat("a", threeport.InstanceNameMaxLength)

	tests := []struct {
		name             string
		instanceName     string
		infraProvider    string
		createRootDomain string
		authEnabled      bool
		kindPortMappings []string
		controlPlaneOnly bool
		clusterName      string
		wantErrContains  string
	}{
		{
			name:          "kind accepts minimal args",
			instanceName:  "tp",
			infraProvider: v0.KubernetesRuntimeInfraProviderKind,
		},
		{
			name:          "eks accepts minimal args",
			instanceName:  "tp",
			infraProvider: v0.KubernetesRuntimeInfraProviderEKS,
		},
		{
			name:          "name at boundary length accepts",
			instanceName:  exactName,
			infraProvider: v0.KubernetesRuntimeInfraProviderKind,
		},
		{
			name:            "name over boundary length rejects",
			instanceName:    longName,
			infraProvider:   v0.KubernetesRuntimeInfraProviderKind,
			wantErrContains: "instance name is too long",
		},
		{
			name:            "unknown provider rejects",
			instanceName:    "tp",
			infraProvider:   "unknown",
			wantErrContains: "invalid provider value",
		},
		{
			name:             "kind port mappings on eks rejects",
			instanceName:     "tp",
			infraProvider:    v0.KubernetesRuntimeInfraProviderEKS,
			kindPortMappings: []string{"8080:80"},
			wantErrContains:  "kind port mappings are only supported",
		},
		{
			name:             "kind port mappings on kind accepts",
			instanceName:     "tp",
			infraProvider:    v0.KubernetesRuntimeInfraProviderKind,
			kindPortMappings: []string{"8080:80"},
		},
		{
			name:             "cluster name without control-plane-only rejects",
			instanceName:     "tp",
			infraProvider:    v0.KubernetesRuntimeInfraProviderKind,
			controlPlaneOnly: false,
			clusterName:      "some-cluster",
			wantErrContains:  "--cluster-name is only valid with --control-plane-only",
		},
		{
			name:             "cluster name with control-plane-only accepts",
			instanceName:     "tp",
			infraProvider:    v0.KubernetesRuntimeInfraProviderKind,
			controlPlaneOnly: true,
			clusterName:      "some-cluster",
		},
		{
			name:             "empty cluster name is fine without control-plane-only",
			instanceName:     "tp",
			infraProvider:    v0.KubernetesRuntimeInfraProviderKind,
			controlPlaneOnly: false,
			clusterName:      "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the validator with the case's flag set
			err := ValidateCreateGenesisControlPlaneFlags(
				tc.instanceName,
				tc.infraProvider,
				tc.createRootDomain,
				tc.authEnabled,
				tc.kindPortMappings,
				tc.controlPlaneOnly,
				tc.clusterName,
			)

			// assert the error surface matches expectation
			if tc.wantErrContains == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErrContains)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErrContains, err.Error())
			}
		})
	}
}

// TestGetControlPlaneEnvVarsFallsBackToEnvOnlyWhenCliUnset asserts env vars
// only fill in image repo and tag fields when the CLI didn't already set them.
func TestGetControlPlaneEnvVarsFallsBackToEnvOnlyWhenCliUnset(t *testing.T) {
	// track original env so the test doesn't leak state between cases
	origRepo := os.Getenv("CONTROL_PLANE_IMAGE_REPO")
	origTag := os.Getenv("CONTROL_PLANE_IMAGE_TAG")
	t.Cleanup(func() {
		os.Setenv("CONTROL_PLANE_IMAGE_REPO", origRepo)
		os.Setenv("CONTROL_PLANE_IMAGE_TAG", origTag)
	})

	tests := []struct {
		name       string
		envRepo    string
		envTag     string
		cliRepo    string
		cliTag     string
		wantRepo   string
		wantTag    string
	}{
		{
			name:     "env fills both when cli empty",
			envRepo:  "docker.io/example",
			envTag:   "v1.2.3",
			wantRepo: "docker.io/example",
			wantTag:  "v1.2.3",
		},
		{
			name:     "cli values preserved over env",
			envRepo:  "docker.io/example",
			envTag:   "v1.2.3",
			cliRepo:  "cli/repo",
			cliTag:   "cli-tag",
			wantRepo: "cli/repo",
			wantTag:  "cli-tag",
		},
		{
			name:     "empty env leaves cli untouched",
			cliRepo:  "cli/repo",
			cliTag:   "cli-tag",
			wantRepo: "cli/repo",
			wantTag:  "cli-tag",
		},
		{
			name: "empty env and empty cli both remain empty",
		},
		{
			name:     "only tag env fills only tag",
			envTag:   "v1.2.3",
			wantRepo: "",
			wantTag:  "v1.2.3",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// stage env for this case
			os.Setenv("CONTROL_PLANE_IMAGE_REPO", tc.envRepo)
			os.Setenv("CONTROL_PLANE_IMAGE_TAG", tc.envTag)

			args := &GenesisControlPlaneCLIArgs{
				ControlPlaneImageRepo: tc.cliRepo,
				ControlPlaneImageTag:  tc.cliTag,
			}
			// invoke the env-lookup side of arg population
			args.GetControlPlaneEnvVars()

			// assert cli fields hold the resolved values
			if args.ControlPlaneImageRepo != tc.wantRepo {
				t.Errorf("ControlPlaneImageRepo: want %q, got %q", tc.wantRepo, args.ControlPlaneImageRepo)
			}
			if args.ControlPlaneImageTag != tc.wantTag {
				t.Errorf("ControlPlaneImageTag: want %q, got %q", tc.wantTag, args.ControlPlaneImageTag)
			}
		})
	}
}

// TestInitArgsPreservesExplicitFieldsWhenAllProvided asserts the init sets no
// defaults when the caller populated every field the initializer touches.
func TestInitArgsPreservesExplicitFieldsWhenAllProvided(t *testing.T) {
	// stage every field the initializer would otherwise fill in
	args := &GenesisControlPlaneCLIArgs{
		ProviderConfigDir: "/explicit/provider/config",
		KubeconfigPath:    "/explicit/kube/config",
		ThreeportPath:     "/explicit/threeport/path",
	}

	// invoke default-filling
	InitArgs(args)

	// assert the caller-provided values are untouched
	if args.ProviderConfigDir != "/explicit/provider/config" {
		t.Errorf("ProviderConfigDir mutated: got %q", args.ProviderConfigDir)
	}
	if args.KubeconfigPath != "/explicit/kube/config" {
		t.Errorf("KubeconfigPath mutated: got %q", args.KubeconfigPath)
	}
	if args.ThreeportPath != "/explicit/threeport/path" {
		t.Errorf("ThreeportPath mutated: got %q", args.ThreeportPath)
	}
}

// TestInitArgsFillsKubeconfigAndThreeportPathDefaults asserts the initializer
// picks up client-go's default kubeconfig path and the process working
// directory when those fields are empty.
func TestInitArgsFillsKubeconfigAndThreeportPathDefaults(t *testing.T) {
	// pre-set ProviderConfigDir so InitArgs skips the ~/.threeport mkdir
	args := &GenesisControlPlaneCLIArgs{
		ProviderConfigDir: "/explicit/provider/config",
	}

	// invoke default-filling
	InitArgs(args)

	// assert the kubeconfig fallback ran and picked up any non-empty path
	if args.KubeconfigPath == "" {
		t.Errorf("expected KubeconfigPath to be filled with a default, got empty")
	}
	// assert threeport path fallback matches the process working directory
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if args.ThreeportPath != cwd {
		t.Errorf("ThreeportPath: want %q, got %q", cwd, args.ThreeportPath)
	}
}

// TestCreateInstallerCopiesArgsIntoOpts asserts every relevant CLI field lands
// on the installer's Opts. Also confirms the initializer sets the constant
// fields the caller shouldn't override (CreateOrUpdateKubeResources=false,
// RestApiLoadBalancer=true).
func TestCreateInstallerCopiesArgsIntoOpts(t *testing.T) {
	// build args with a distinctive value on every field CreateInstaller reads
	args := &GenesisControlPlaneCLIArgs{
		AuthEnabled:       true,
		AwsConfigProfile:  "aws-profile",
		AwsConfigEnv:      true,
		AwsRegion:         "us-east-1",
		OciRegion:         "us-phoenix-1",
		OciConfigProfile:  "oci-profile",
		GcpProjectId:      "gcp-proj",
		GcpRegion:         "us-central1",
		CfgFile:           "/tmp/cfg",
		CreateRootDomain:  "example.com",
		CreateAdminEmail:  "admin@example.com",
		DevEnvironment:    true,
		ForceOverwriteConfig: true,
		ControlPlaneName:  "cp1",
		InfraProvider:     v0.KubernetesRuntimeInfraProviderKind,
		KubeconfigPath:    "/tmp/kubeconfig",
		NumWorkerNodes:    2,
		ProviderConfigDir: "/tmp/provider",
		ThreeportPath:     "/tmp/threeport",
		Debug:             true,
		Verbose:           true,
		TeardownOnFailure: true,
		ControlPlaneOnly:  true,
		ClusterName:       "my-cluster",
		InfraOnly:         true,
		KindPortMappings:  []string{"8080:80"},
		LocalRegistry:     true,
	}

	// invoke installer build
	cpi, err := args.CreateInstaller()
	if err != nil {
		t.Fatalf("CreateInstaller returned unexpected error: %v", err)
	}
	if cpi == nil {
		t.Fatal("CreateInstaller returned nil installer")
	}

	// assert every copied field lands on Opts
	checks := map[string]struct {
		got  interface{}
		want interface{}
	}{
		"AuthEnabled":          {cpi.Opts.AuthEnabled, true},
		"AwsConfigProfile":     {cpi.Opts.AwsConfigProfile, "aws-profile"},
		"AwsConfigEnv":         {cpi.Opts.AwsConfigEnv, true},
		"AwsRegion":            {cpi.Opts.AwsRegion, "us-east-1"},
		"OciRegion":            {cpi.Opts.OciRegion, "us-phoenix-1"},
		"OciConfigProfile":     {cpi.Opts.OciConfigProfile, "oci-profile"},
		"GcpProjectId":         {cpi.Opts.GcpProjectId, "gcp-proj"},
		"GcpRegion":            {cpi.Opts.GcpRegion, "us-central1"},
		"CfgFile":              {cpi.Opts.CfgFile, "/tmp/cfg"},
		"CreateRootDomain":     {cpi.Opts.CreateRootDomain, "example.com"},
		"CreateAdminEmail":     {cpi.Opts.CreateAdminEmail, "admin@example.com"},
		"DevEnvironment":       {cpi.Opts.DevEnvironment, true},
		"ForceOverwriteConfig": {cpi.Opts.ForceOverwriteConfig, true},
		"ControlPlaneName":     {cpi.Opts.ControlPlaneName, "cp1"},
		"InfraProvider":        {cpi.Opts.InfraProvider, v0.KubernetesRuntimeInfraProviderKind},
		"KubeconfigPath":       {cpi.Opts.KubeconfigPath, "/tmp/kubeconfig"},
		"NumWorkerNodes":       {cpi.Opts.NumWorkerNodes, 2},
		"ProviderConfigDir":    {cpi.Opts.ProviderConfigDir, "/tmp/provider"},
		"ThreeportPath":        {cpi.Opts.ThreeportPath, "/tmp/threeport"},
		"Debug":                {cpi.Opts.Debug, true},
		"Verbose":              {cpi.Opts.Verbose, true},
		"TeardownOnFailure":    {cpi.Opts.TeardownOnFailure, true},
		"ControlPlaneOnly":     {cpi.Opts.ControlPlaneOnly, true},
		"ClusterName":          {cpi.Opts.ClusterName, "my-cluster"},
		"InfraOnly":            {cpi.Opts.InfraOnly, true},
		"LocalRegistry":        {cpi.Opts.LocalRegistry, true},
	}
	for name, c := range checks {
		if c.got != c.want {
			t.Errorf("%s: want %v, got %v", name, c.want, c.got)
		}
	}
	// assert the KindPortMappings slice was copied through
	if len(cpi.Opts.KindPortMappings) != 1 || cpi.Opts.KindPortMappings[0] != "8080:80" {
		t.Errorf("KindPortMappings: want [8080:80], got %v", cpi.Opts.KindPortMappings)
	}

	// assert the constants the initializer forces on every install
	if cpi.Opts.CreateOrUpdateKubeResources != false {
		t.Errorf("CreateOrUpdateKubeResources: want false, got %v", cpi.Opts.CreateOrUpdateKubeResources)
	}
	if cpi.Opts.RestApiLoadBalancer != true {
		t.Errorf("RestApiLoadBalancer: want true, got %v", cpi.Opts.RestApiLoadBalancer)
	}
}

// TestCreateInstallerEmptyImageRepoAndTagSkipsMutators asserts the installer
// build does not propagate blank image repo or tag onto shared component
// defaults when the caller didn't supply values.
func TestCreateInstallerEmptyImageRepoAndTagSkipsMutators(t *testing.T) {
	// capture the current values on the shared component defaults so we can
	// verify they weren't blanked by CreateInstaller
	priorRepo := threeport.ThreeportRestApi.ImageNamespace
	priorTag := threeport.ThreeportRestApi.ImageTag

	args := &GenesisControlPlaneCLIArgs{}
	// invoke installer build without image repo/tag
	if _, err := args.CreateInstaller(); err != nil {
		t.Fatalf("CreateInstaller returned unexpected error: %v", err)
	}

	// assert shared defaults preserved
	if threeport.ThreeportRestApi.ImageNamespace != priorRepo {
		t.Errorf("ThreeportRestApi.ImageNamespace mutated: want %q, got %q", priorRepo, threeport.ThreeportRestApi.ImageNamespace)
	}
	if threeport.ThreeportRestApi.ImageTag != priorTag {
		t.Errorf("ThreeportRestApi.ImageTag mutated: want %q, got %q", priorTag, threeport.ThreeportRestApi.ImageTag)
	}
}

// TestUninstallerCleanOnCreateErrorSkipsTeardownWhenDisabled covers the early
// return path: when teardownOnFailure is false, the function must return the
// wrapped createErr without invoking any teardown or config cleanup.
func TestUninstallerCleanOnCreateErrorSkipsTeardownWhenDisabled(t *testing.T) {
	tests := []struct {
		name         string
		createErrMsg string
		createErr    error
		wantContains []string
	}{
		{
			name:         "wraps message when message provided",
			createErrMsg: "failed to do the thing",
			createErr:    errors.New("root cause"),
			wantContains: []string{"failed to do the thing", "root cause"},
		},
		{
			name:         "returns raw error when message empty",
			createErrMsg: "",
			createErr:    errors.New("root cause"),
			wantContains: []string{"root cause"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// stage an uninstaller with teardown-on-failure disabled
			teardown := false
			cleanCfg := false
			u := &Uninstaller{
				teardownOnFailure: &teardown,
				cleanConfig:       &cleanCfg,
			}

			// invoke the cleanup routine
			err := u.cleanOnCreateError(tc.createErrMsg, tc.createErr)

			// assert the returned error contains every expected fragment
			if err == nil {
				t.Fatalf("expected an error, got nil")
			}
			for _, frag := range tc.wantContains {
				if !strings.Contains(err.Error(), frag) {
					t.Errorf("error %q missing fragment %q", err.Error(), frag)
				}
			}
			// assert the wrapped error still resolves the original via errors.Is
			if !errors.Is(err, tc.createErr) {
				t.Errorf("errors.Is: expected returned error to wrap the create error")
			}
		})
	}
}

// TestRuntimeInstanceNameHonorsControlPlaneOnly asserts the helper returns the
// caller-supplied cluster name when ControlPlaneOnly is set and derives the
// threeport-prefixed name otherwise.
func TestRuntimeInstanceNameHonorsControlPlaneOnly(t *testing.T) {
	tests := []struct {
		name string
		opts threeport.Options
		want string
	}{
		{
			name: "control-plane-only returns cluster name verbatim",
			opts: threeport.Options{ControlPlaneOnly: true, ClusterName: "existing-cluster"},
			want: "existing-cluster",
		},
		{
			name: "control-plane-only with empty cluster name returns empty",
			opts: threeport.Options{ControlPlaneOnly: true, ClusterName: ""},
			want: "",
		},
		{
			name: "fresh install prefixes threeport- onto control plane name",
			opts: threeport.Options{ControlPlaneName: "test"},
			want: "threeport-test",
		},
		{
			name: "fresh install with empty control plane name still prefixes",
			opts: threeport.Options{},
			want: "threeport-",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// invoke the helper
			got := runtimeInstanceName(tc.opts)
			// assert the effective name matches
			if got != tc.want {
				t.Errorf("runtimeInstanceName: want %q, got %q", tc.want, got)
			}
		})
	}
}

// ensure util.Ptr stays referenced so imports don't drift silently.
var _ = util.Ptr[int]

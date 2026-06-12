package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	container "cloud.google.com/go/container/apiv1"
	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp"
	"github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/compute"
	gkecontainer "github.com/pulumi/pulumi-gcp/sdk/v8/go/gcp/container"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/iam/v1"
	gcpiam "google.golang.org/api/iam/v1"
	"google.golang.org/api/option"
	"gorm.io/datatypes"

	kube "github.com/threeport/threeport/pkg/kube/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// GCP OAuth2 configuration for Application Default Credentials
// These are the public client credentials used by gcloud CLI for user authentication
// See: https://cloud.google.com/sdk/docs/authorizing
const (
	gcpOAuthClientID     = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com"
	gcpOAuthClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty"
)

// gcpOAuthScopes defines the scopes needed for GKE operations
var gcpOAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
}

// adcCredentials represents the structure of the Application Default Credentials file
type adcCredentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	Type         string `json:"type"`
}

// KubernetesRuntimeInfraGKE represents the infrastructure for a threeport-managed
// GKE (Google Kubernetes Engine) cluster.
type KubernetesRuntimeInfraGKE struct {
	// PulumiWorkspace provides workspace, stack, state, and automation API helpers
	// (aligned with KubernetesRuntimeInfraOKE).
	PulumiWorkspace

	// Kubernetes version of the GKE cluster.
	Version string

	// The Google Cloud project ID where the cluster infra is provisioned.
	ProjectID string

	// The Google Cloud region where the cluster infra is provisioned.
	Region string

	// The number of nodes initially created for the worker node pool.
	WorkerNodeInitialCount int32

	// The email address of the GCP service account created for Threeport.
	// This service account is used with Workload Identity to allow
	// Threeport controllers to manage GCP resources.
	ServiceAccountEmail string

	// ServiceAccountCredentials contains the JSON key for a GCP service account.
	// Used when running outside GCP (e.g., in AWS or on-prem) where Workload
	// Identity is not available. This is the contents of a service account key
	// file exported from GCP Console.
	ServiceAccountCredentials string

	// credentialsFilePath stores the path to a temporary file containing
	// the service account credentials. This is set internally when
	// ServiceAccountCredentials is used.
	credentialsFilePath string

	// gcpProjectResolvedFromGcloudINI is true when loadGCPConfig set ProjectID from the active gcloud INI.
	gcpProjectResolvedFromGcloudINI bool
	// gcpRegionResolvedFromGcloudINI is true when loadGCPConfig set Region from the active gcloud INI.
	gcpRegionResolvedFromGcloudINI bool
}

// ensurePulumiProjectDefaults sets Pulumi project metadata when not provided by callers.
func (i *KubernetesRuntimeInfraGKE) ensurePulumiProjectDefaults() {
	if i.ProjectName == "" {
		i.ProjectName = "gke"
	}
	if i.ProjectDescription == "" {
		i.ProjectDescription = "Google Kubernetes Engine (GKE) cluster for Threeport"
	}
}

// syncStackConfigs updates stack config keys from the current ProjectID and Region.
func (i *KubernetesRuntimeInfraGKE) syncStackConfigs() {
	i.StackConfigs = map[string]string{
		"gcp:project": i.ProjectID,
		"gcp:region":  i.Region,
	}
}

// Create installs a Kubernetes cluster using Google Cloud GKE for threeport workloads.
func (i *KubernetesRuntimeInfraGKE) Create() (*kube.KubeConnectionInfo, error) {
	// ensure GCP authentication is in place
	if err := i.ensureGCPAuth(); err != nil {
		return nil, fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	// load GCP configuration to ensure ProjectID is set
	if err := i.loadGCPConfig(); err != nil {
		return nil, fmt.Errorf("failed to load GCP configuration: %w", err)
	}

	// Only compare ADC identity to gcloud core.account when project or region came from the
	// gcloud profile; explicit flags/env avoid implying that profile's user context.
	if i.gcpProjectResolvedFromGcloudINI || i.gcpRegionResolvedFromGcloudINI {
		if err := validateADCUserMatchesGcloudAccount(context.Background()); err != nil {
			return nil, err
		}
	}

	// create GCP service account for Threeport to manage GCP resources
	// This service account will be used with Workload Identity
	if err := i.createGCPServiceAccountAndCredentials(); err != nil {
		return nil, fmt.Errorf("failed to create GCP service account: %w", err)
	}

	if err := i.CreateInfra(context.Background()); err != nil {
		return nil, err
	}

	return i.GetConnection()
}

// ensure KubernetesRuntimeInfraGKE implements the observe-and-requeue
// infrastructure interface.
var _ InfraProvider = (*KubernetesRuntimeInfraGKE)(nil)

// CreateInfra provisions GKE cluster infrastructure via Pulumi (network, cluster, node pool)
// and configures Workload Identity. It does not create the Threeport GCP service account:
// call createGCPServiceAccountAndCredentials first for the bootstrap path, or ensure
// credentials and Workload Identity are already configured for the controller path. It runs
// a single Pulumi up pass under the supplied context deadline and returns when that pass
// returns; it does not poll for cluster readiness.
func (i *KubernetesRuntimeInfraGKE) CreateInfra(ctx context.Context) error {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	stack, err := i.SetupStack(i.pulumiProgram())
	if err != nil {
		return fmt.Errorf("failed to set up Pulumi workspace: %w", err)
	}

	if _, err = i.RunUp(ctx, stack); err != nil {
		return fmt.Errorf("failed to deploy stack: %w", err)
	}

	// configure Workload Identity binding after cluster resources exist so
	// Kubernetes service accounts can impersonate the GCP service account
	if err := i.configureWorkloadIdentityBindingPostCreate(ctx); err != nil {
		return fmt.Errorf("failed to configure Workload Identity binding: %w", err)
	}

	return nil
}

// Observe refreshes the persisted Pulumi state against GCP and reports the current
// lifecycle phase. A refresh failure surfaces as an error so the caller requeues
// without kicking a create or destroy; it is never reported as PhaseAbsent, which
// would let Apply provision a duplicate cluster. Completeness is derived from the
// refreshed resource inventory plus a successful connection probe: an empty
// inventory means absent, a non-empty inventory with an unreachable cluster means
// provisioning, and a non-empty inventory with a reachable cluster means ready.
func (i *KubernetesRuntimeInfraGKE) Observe(ctx context.Context) (Observation, error) {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	// set up the workspace and refresh state against the cloud. A refresh error
	// must not be swallowed into an empty state, so surface it to the caller.
	stack, err := i.SetupStack(func(*pulumi.Context) error { return nil })
	if err != nil {
		return Observation{}, fmt.Errorf("failed to set up Pulumi workspace for observe: %w", err)
	}
	if _, err := i.runRefresh(ctx, stack); err != nil {
		return Observation{}, fmt.Errorf("failed to refresh stack state: %w", err)
	}

	// export the refreshed state so the caller can persist it and so phase is
	// derived from cloud reality rather than a stale persisted snapshot
	state, err := i.GetStackState()
	if err != nil {
		return Observation{}, fmt.Errorf("failed to export refreshed stack state: %w", err)
	}

	// no managed resources remain in the refreshed state: genuinely absent
	count, err := countManagedResources(state)
	if err != nil {
		return Observation{}, fmt.Errorf("failed to count resources in refreshed state: %w", err)
	}
	if count == 0 {
		return Observation{Phase: PhaseAbsent}, nil
	}

	// resources exist; probe the cluster connection to tell provisioning from
	// ready. A probe failure is expected while the cluster is still coming up or
	// being torn down, so it maps to provisioning rather than an error.
	if _, err := i.GetConnection(); err != nil {
		return Observation{
			Phase:   PhaseProvisioning,
			State:   state,
			Message: fmt.Sprintf("cluster not yet reachable: %v", err),
		}, nil
	}

	return Observation{Phase: PhaseReady, State: state}, nil
}

// Apply advances the GKE create by running a single bounded Pulumi up pass under
// the context deadline and configuring the Workload Identity binding. It returns
// without polling for readiness; the next Observe reports progress. Apply is
// idempotent: run against partially-created infrastructure, the underlying up
// diffs and converges rather than provisioning a second cluster.
func (i *KubernetesRuntimeInfraGKE) Apply(ctx context.Context) error {
	return i.CreateInfra(ctx)
}

// Destroy advances the GKE teardown by running a single bounded Pulumi destroy
// pass under the context deadline. It does not delete the non-Pulumi GCP service
// account and IAM bindings; that cleanup runs once after the inventory is empty,
// in the lifecycle handler's post-deletion hook, so it is not repeated on every
// destroy kick.
func (i *KubernetesRuntimeInfraGKE) Destroy(ctx context.Context) error {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	return i.destroyStack(ctx)
}

// pulumiProgram defines the Pulumi resources for the GKE stack.
func (i *KubernetesRuntimeInfraGKE) pulumiProgram() pulumi.RunFunc {
	return func(ctx *pulumi.Context) error {
		providerArgs := &gcp.ProviderArgs{
			Project: pulumi.String(i.ProjectID),
			Region:  pulumi.String(i.Region),
		}
		// when explicit service account credentials are configured, pass them to
		// the provider directly so the credential is scoped to this stack rather
		// than discovered from a process-global environment variable shared by
		// every concurrent stack in the controller.
		if i.ServiceAccountCredentials != "" {
			providerArgs.Credentials = pulumi.StringPtr(i.ServiceAccountCredentials)
		}
		gcpProvider, err := gcp.NewProvider(ctx, "gcp-provider", providerArgs)
		if err != nil {
			return fmt.Errorf("failed to create GCP provider: %w", err)
		}

		network, err := compute.NewNetwork(ctx, fmt.Sprintf("%s-vpc", i.RuntimeInstanceName), &compute.NetworkArgs{
			Name:                  pulumi.String(fmt.Sprintf("%s-vpc", i.RuntimeInstanceName)),
			AutoCreateSubnetworks: pulumi.Bool(false),
			Description:           pulumi.String(fmt.Sprintf("VPC network for Threeport GKE cluster %s", i.RuntimeInstanceName)),
		}, pulumi.Provider(gcpProvider))
		if err != nil {
			return fmt.Errorf("failed to create VPC network: %w", err)
		}

		subnet, err := compute.NewSubnetwork(ctx, fmt.Sprintf("%s-subnet", i.RuntimeInstanceName), &compute.SubnetworkArgs{
			Name:        pulumi.String(fmt.Sprintf("%s-subnet", i.RuntimeInstanceName)),
			IpCidrRange: pulumi.String("10.0.0.0/16"),
			Region:      pulumi.String(i.Region),
			Network:     network.ID(),
			SecondaryIpRanges: compute.SubnetworkSecondaryIpRangeArray{
				&compute.SubnetworkSecondaryIpRangeArgs{
					RangeName:   pulumi.String("pods"),
					IpCidrRange: pulumi.String("10.1.0.0/16"),
				},
				&compute.SubnetworkSecondaryIpRangeArgs{
					RangeName:   pulumi.String("services"),
					IpCidrRange: pulumi.String("10.2.0.0/20"),
				},
			},
		}, pulumi.Provider(gcpProvider),
			pulumi.DependsOn([]pulumi.Resource{network}))
		if err != nil {
			return fmt.Errorf("failed to create subnet: %w", err)
		}

		router, err := compute.NewRouter(ctx, fmt.Sprintf("%s-router", i.RuntimeInstanceName), &compute.RouterArgs{
			Name:    pulumi.String(fmt.Sprintf("%s-router", i.RuntimeInstanceName)),
			Network: network.ID(),
			Region:  pulumi.String(i.Region),
		}, pulumi.Provider(gcpProvider),
			pulumi.DependsOn([]pulumi.Resource{network}))
		if err != nil {
			return fmt.Errorf("failed to create Cloud Router: %w", err)
		}

		_, err = compute.NewRouterNat(ctx, fmt.Sprintf("%s-nat", i.RuntimeInstanceName), &compute.RouterNatArgs{
			Name:                          pulumi.String(fmt.Sprintf("%s-nat", i.RuntimeInstanceName)),
			Router:                        router.Name,
			Region:                        pulumi.String(i.Region),
			NatIpAllocateOption:           pulumi.String("AUTO_ONLY"),
			SourceSubnetworkIpRangesToNat: pulumi.String("ALL_SUBNETWORKS_ALL_IP_RANGES"),
			LogConfig: &compute.RouterNatLogConfigArgs{
				Enable: pulumi.Bool(true),
				Filter: pulumi.String("ERRORS_ONLY"),
			},
		}, pulumi.Provider(gcpProvider),
			pulumi.DependsOn([]pulumi.Resource{router}))
		if err != nil {
			return fmt.Errorf("failed to create Cloud NAT: %w", err)
		}

		cluster, err := gkecontainer.NewCluster(ctx, i.RuntimeInstanceName, &gkecontainer.ClusterArgs{
			Name:       pulumi.String(i.RuntimeInstanceName),
			Location:   pulumi.String(i.Region),
			Network:    network.Name,
			Subnetwork: subnet.Name,

			IpAllocationPolicy: &gkecontainer.ClusterIpAllocationPolicyArgs{
				ClusterSecondaryRangeName:  pulumi.String("pods"),
				ServicesSecondaryRangeName: pulumi.String("services"),
			},

			InitialNodeCount:      pulumi.Int(1),
			RemoveDefaultNodePool: pulumi.Bool(true),

			PrivateClusterConfig: &gkecontainer.ClusterPrivateClusterConfigArgs{
				EnablePrivateNodes:    pulumi.Bool(true),
				EnablePrivateEndpoint: pulumi.Bool(false),
				MasterIpv4CidrBlock:   pulumi.String("172.16.0.0/28"),
			},

			NetworkPolicy: &gkecontainer.ClusterNetworkPolicyArgs{
				Enabled:  pulumi.Bool(true),
				Provider: pulumi.String("CALICO"),
			},

			WorkloadIdentityConfig: &gkecontainer.ClusterWorkloadIdentityConfigArgs{
				WorkloadPool: pulumi.String(fmt.Sprintf("%s.svc.id.goog", i.ProjectID)),
			},

			DeletionProtection: pulumi.Bool(false),
		}, pulumi.Provider(gcpProvider),
			pulumi.DependsOn([]pulumi.Resource{subnet}))
		if err != nil {
			return fmt.Errorf("failed to create GKE cluster: %w", err)
		}

		_, err = gkecontainer.NewNodePool(ctx, fmt.Sprintf("%s-nodepool", i.RuntimeInstanceName), &gkecontainer.NodePoolArgs{
			Name:     pulumi.String(fmt.Sprintf("%s-nodepool", i.RuntimeInstanceName)),
			Cluster:  cluster.Name,
			Location: pulumi.String(i.Region),

			NodeCount: pulumi.Int(int(i.WorkerNodeInitialCount)),

			NodeConfig: &gkecontainer.NodePoolNodeConfigArgs{
				MachineType: pulumi.String("e2-medium"),
				DiskSizeGb:  pulumi.Int(50),
				DiskType:    pulumi.String("pd-standard"),
				OauthScopes: pulumi.StringArray{
					pulumi.String("https://www.googleapis.com/auth/cloud-platform"),
				},
				Labels: pulumi.StringMap{
					kube.ThreeportManagedByLabelKey: pulumi.String(kube.ThreeportManagedByLabelValue),
				},
				WorkloadMetadataConfig: &gkecontainer.NodePoolNodeConfigWorkloadMetadataConfigArgs{
					Mode: pulumi.String("GKE_METADATA"),
				},
			},

			Autoscaling: &gkecontainer.NodePoolAutoscalingArgs{
				MinNodeCount:   pulumi.Int(1),
				MaxNodeCount:   pulumi.Int(10),
				LocationPolicy: pulumi.String("ANY"),
			},

			Management: &gkecontainer.NodePoolManagementArgs{
				AutoRepair:  pulumi.Bool(true),
				AutoUpgrade: pulumi.Bool(true),
			},
		}, pulumi.Provider(gcpProvider),
			pulumi.DependsOn([]pulumi.Resource{cluster}))
		if err != nil {
			return fmt.Errorf("failed to create node pool: %w", err)
		}

		return nil
	}
}

// Delete deletes a GKE cluster and the threeport control plane with it. This is
// the bootstrap teardown path used by the CLI: it both destroys the Pulumi stack
// and removes the non-Pulumi GCP service account and IAM bindings in one call. The
// controller lifecycle splits these so the service-account cleanup runs once after
// the stack is empty, not on every destroy step.
func (i *KubernetesRuntimeInfraGKE) Delete() error {
	if err := i.ensureGCPAuth(); err != nil {
		return fmt.Errorf("failed to ensure GCP authentication: %w", err)
	}

	if err := i.loadGCPConfig(); err != nil {
		return fmt.Errorf("failed to load GCP configuration: %w", err)
	}

	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()

	ctx := context.Background()
	if err := i.destroyStack(ctx); err != nil {
		return fmt.Errorf("failed to destroy Pulumi stack: %w", err)
	}

	if err := i.DeleteGCPResources(ctx); err != nil {
		if i.Logger != nil {
			i.Logger.Info("warning: failed to clean up GCP resources", "error", err.Error())
		} else {
			util.CliOutputWarning(fmt.Sprintf("failed to clean up GCP resources: %v", err))
		}
	}

	return nil
}

// destroyStack sets up the workspace, refreshes against the cloud under the
// supplied context, destroys the Pulumi-managed resources, and removes the local
// state directory. It honors the context deadline so a single destroy kick stays
// bounded; the shared workspace helper hardcodes a background context, so the
// teardown is composed here instead.
func (i *KubernetesRuntimeInfraGKE) destroyStack(ctx context.Context) error {
	stack, err := i.SetupStack(func(*pulumi.Context) error { return nil })
	if err != nil {
		return fmt.Errorf("failed to set up Pulumi workspace for destroy: %w", err)
	}

	// refresh before destroy to clear stale pending operations and reconcile the
	// recorded state with what actually exists; a refresh failure is logged but
	// does not abort, since the destroy may still succeed.
	if _, err := i.runRefresh(ctx, stack); err != nil {
		i.logError(err, "failed to refresh stack before destroy, proceeding with destroy")
	}

	if _, err := i.RunDestroy(ctx, stack); err != nil {
		return fmt.Errorf("failed to destroy stack: %w", err)
	}

	if err := os.RemoveAll(i.stateDir); err != nil {
		return fmt.Errorf("failed to remove state directory: %w", err)
	}

	return nil
}

// DeleteGCPResources deletes all GCP resources created for this Threeport instance
// that are not managed by Pulumi. The lifecycle handler calls it once after the
// stack inventory is empty, via the post-deletion hook.
func (i *KubernetesRuntimeInfraGKE) DeleteGCPResources(ctx context.Context) error {
	// Create IAM service client scoped to the configured credentials so a
	// concurrent instance with different credentials cannot bleed in.
	iamService, err := iam.NewService(ctx, i.gcpClientOptions(iam.CloudPlatformScope)...)
	if err != nil {
		return fmt.Errorf("failed to create IAM service client: %w", err)
	}

	// Create Cloud Resource Manager service client for IAM bindings
	crmService, err := cloudresourcemanager.NewService(ctx, i.gcpClientOptions(cloudresourcemanager.CloudPlatformScope)...)
	if err != nil {
		return fmt.Errorf("failed to create Cloud Resource Manager service client: %w", err)
	}

	// Remove IAM role bindings
	if err := i.removeServiceAccountRoles(crmService); err != nil {
		return fmt.Errorf("failed to remove IAM roles: %w", err)
	}

	// Delete the service account
	if err := i.deleteGCPServiceAccount(iamService); err != nil {
		return fmt.Errorf("failed to delete service account: %w", err)
	}

	return nil
}

// GetConnection returns the connection information for the GKE cluster. It scopes
// every GCP client and the access-token source to the configured service account
// credentials when present, so a probe from one instance never authenticates with
// another instance's credentials.
func (i *KubernetesRuntimeInfraGKE) GetConnection() (*kube.KubeConnectionInfo, error) {
	// load GCP configuration from gcloud CLI config or environment variables
	if err := i.loadGCPConfig(); err != nil {
		return nil, fmt.Errorf("failed to load GCP configuration: %w", err)
	}

	ctx := context.Background()

	// create GKE cluster manager client scoped to the configured credentials,
	// falling back to Application Default Credentials when none are set
	clusterManagerClient, err := container.NewClusterManagerClient(ctx, i.gcpClientOptions()...)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster manager client: %w", err)
	}
	defer clusterManagerClient.Close()

	// construct the cluster name in the format required by GKE API
	// Format: projects/{project}/locations/{location}/clusters/{cluster}
	clusterName := fmt.Sprintf(
		"projects/%s/locations/%s/clusters/%s",
		i.ProjectID, i.Region, i.RuntimeInstanceName,
	)

	// get cluster details
	cluster, err := clusterManagerClient.GetCluster(ctx, &containerpb.GetClusterRequest{
		Name: clusterName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster details: %w", err)
	}

	// decode the CA certificate
	caCert, err := base64.StdEncoding.DecodeString(cluster.MasterAuth.ClusterCaCertificate)
	if err != nil {
		return nil, fmt.Errorf("failed to decode CA certificate: %w", err)
	}

	// get an access token for authentication
	// TODO: Implement token refresh mechanism for long-running operations
	// The token has a limited lifetime (typically 1 hour)
	tokenSource, err := i.tokenSource(ctx, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to get token source: %w", err)
	}

	token, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	// construct the API endpoint
	// GKE provides the endpoint without the https:// prefix
	apiEndpoint := fmt.Sprintf("https://%s", cluster.Endpoint)

	// create connection info
	kubeConnInfo := &kube.KubeConnectionInfo{
		APIEndpoint:     apiEndpoint,
		CACertificate:   string(caCert),
		Token:           token.AccessToken,
		TokenExpiration: token.Expiry,
	}

	return kubeConnInfo, nil
}

// LoadConfigFromStack loads the GCP project ID and region from the existing
// Pulumi stack configuration. This is useful when deleting a cluster where
// we only have the runtime instance name but need the project and region
// to connect to GCP.
func (i *KubernetesRuntimeInfraGKE) LoadConfigFromStack() error {
	i.ensurePulumiProjectDefaults()

	workspace, err := i.initWorkspace(auto.Program(func(ctx *pulumi.Context) error { return nil }))
	if err != nil {
		return fmt.Errorf("failed to initialize Pulumi workspace: %w", err)
	}

	ctx := context.Background()
	stack, err := auto.SelectStack(ctx, i.getStackName(), workspace)
	if err != nil {
		return fmt.Errorf("failed to select stack: %w", err)
	}

	projectConfig, err := stack.GetConfig(ctx, "gcp:project")
	if err != nil {
		return fmt.Errorf("failed to get gcp:project config: %w", err)
	}
	i.ProjectID = projectConfig.Value

	regionConfig, err := stack.GetConfig(ctx, "gcp:region")
	if err != nil {
		return fmt.Errorf("failed to get gcp:region config: %w", err)
	}
	i.Region = regionConfig.Value

	return nil
}

// GetStackState returns the state of the GKE stack as a JSON object.
func (i *KubernetesRuntimeInfraGKE) GetStackState() (*datatypes.JSON, error) {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.GetStackState()
}

// SetStackState restores Pulumi state from a JSON object (export or checkpoint format).
func (i *KubernetesRuntimeInfraGKE) SetStackState(state *datatypes.JSON) error {
	i.ensurePulumiProjectDefaults()
	i.syncStackConfigs()
	return i.PulumiWorkspace.SetStackState(state)
}

// countManagedResources reports how many provider-managed cloud resources the
// exported stack state holds, ignoring the synthetic root stack resource and any
// resource already marked for deletion. A count of zero means the refreshed state
// is genuinely empty, which the caller treats as absent infrastructure. A nil,
// empty, or placeholder state ("{}", "null") trivially has no resources.
func countManagedResources(state *datatypes.JSON) (int, error) {
	if state == nil {
		return 0, nil
	}
	raw := string(*state)
	if len(raw) == 0 || raw == "{}" || raw == "null" {
		return 0, nil
	}

	var deployment apitype.UntypedDeployment
	if err := json.Unmarshal(*state, &deployment); err != nil {
		return 0, fmt.Errorf("failed to unmarshal stack state: %w", err)
	}
	if deployment.Deployment == nil {
		return 0, nil
	}

	var snapshot apitype.DeploymentV3
	if err := json.Unmarshal(deployment.Deployment, &snapshot); err != nil {
		return 0, fmt.Errorf("failed to unmarshal deployment snapshot: %w", err)
	}

	count := 0
	for _, res := range snapshot.Resources {
		// the root stack pseudo-resource is always present even for an empty
		// stack; it is not a provider-managed cloud resource
		if res.Type == "pulumi:pulumi:Stack" {
			continue
		}
		// a resource pending deletion is on its way out, not present
		if res.Delete {
			continue
		}
		count++
	}
	return count, nil
}

// configureWorkloadIdentityBindingPostCreate sets up the Workload Identity binding
// after the GKE cluster has been created. The IAM client is scoped to the
// configured service account credentials when present.
func (i *KubernetesRuntimeInfraGKE) configureWorkloadIdentityBindingPostCreate(ctx context.Context) error {
	// Create IAM service client
	iamService, err := gcpiam.NewService(ctx, i.gcpClientOptions(gcpiam.CloudPlatformScope)...)
	if err != nil {
		return fmt.Errorf("failed to create IAM service client: %w", err)
	}

	return i.configureWorkloadIdentityBinding(iamService)
}

// loadGCPConfig populates ProjectID and Region when they are not already set.
// Priority for each field independently:
//  1. Caller-provided values (e.g. CLI flags) already on the receiver — never overwritten
//  2. Environment variables (only if that field is still empty)
//  3. Active gcloud configuration INI ([core] project, [compute] region)
func (i *KubernetesRuntimeInfraGKE) loadGCPConfig() error {
	i.gcpProjectResolvedFromGcloudINI = false
	i.gcpRegionResolvedFromGcloudINI = false

	if i.ProjectID == "" {
		projectID := os.Getenv("GOOGLE_CLOUD_PROJECT")
		if projectID == "" {
			projectID = os.Getenv("CLOUDSDK_CORE_PROJECT")
		}
		if projectID == "" {
			projectID = os.Getenv("GCLOUD_PROJECT")
		}
		if projectID != "" {
			i.ProjectID = projectID
		}
	}

	if i.Region == "" {
		region := os.Getenv("CLOUDSDK_COMPUTE_REGION")
		if region == "" {
			region = os.Getenv("GOOGLE_REGION")
		}
		if region != "" {
			i.Region = region
		}
	}

	if i.ProjectID != "" && i.Region != "" {
		return nil
	}

	if err := i.loadGCPConfigFromFile(); err != nil {
		if i.ProjectID == "" {
			return fmt.Errorf("GCP project ID not found in environment variables or gcloud config: %w", err)
		}
		if i.Region == "" {
			return fmt.Errorf("GCP region not found in environment variables or gcloud config: %w", err)
		}
	}

	return nil
}

// loadGCPConfigFromFile reads the gcloud CLI configuration files
// to get project ID and region settings.
func (i *KubernetesRuntimeInfraGKE) loadGCPConfigFromFile() error {
	cfg, err := loadActiveGcloudConfigINI()
	if err != nil {
		return err
	}

	// get project ID from [core] section
	if i.ProjectID == "" {
		projectID := cfg.Section("core").Key("project").String()
		if projectID != "" {
			i.ProjectID = projectID
			i.gcpProjectResolvedFromGcloudINI = true
		}
	}

	// get region from [compute] section
	if i.Region == "" {
		region := cfg.Section("compute").Key("region").String()
		if region != "" {
			i.Region = region
			i.gcpRegionResolvedFromGcloudINI = true
		}
	}

	return nil
}

// EnsureGCPAuth checks for valid GCP Application Default Credentials and initiates
// the browser OAuth flow if none exist. It is the credentials-free entry point used
// by standalone bootstrap helpers that authenticate as the calling user; the
// instance lifecycle uses the receiver method to also handle a stored service
// account credential.
func EnsureGCPAuth(serviceAccountCredentials string) error {
	infra := &KubernetesRuntimeInfraGKE{ServiceAccountCredentials: serviceAccountCredentials}
	return infra.ensureGCPAuth()
}

// ensureGCPAuth checks for valid GCP Application Default Credentials and initiates
// the OAuth flow if credentials are missing or invalid. This allows users to
// authenticate without manually running `gcloud auth application-default login`.
//
// This function handles three authentication scenarios:
// 1. CLI usage (tptctl): Uses browser-based OAuth flow for user authentication
// 2. Controller in GKE: Uses Workload Identity (automatic via metadata server)
// 3. Controller outside GCP: Uses service account credentials JSON
//
// When ServiceAccountCredentials is set it is written to a temporary file whose
// path is recorded on the receiver; no process environment variable is mutated, so
// concurrent instances stay isolated. When empty, the receiver checks for existing
// credentials (scenarios 1 and 2) and falls back to browser-based auth if needed.
func (i *KubernetesRuntimeInfraGKE) ensureGCPAuth() error {
	ctx := context.Background()

	// FIRST: Check if valid credentials already exist
	// This covers (in order of preference):
	// - Workload Identity in GKE (scenario 2) - most secure, uses short-lived tokens
	// - User credentials from gcloud auth (scenario 1)
	if i.ServiceAccountCredentials == "" && hasValidGCPCredentials(ctx) {
		return nil
	}

	// SECOND: If service account credentials are provided, use them. This is the
	// fallback for controllers running outside GCP.
	if i.ServiceAccountCredentials != "" {
		if err := i.configureServiceAccountCredentials(i.ServiceAccountCredentials); err != nil {
			return fmt.Errorf("failed to configure service account credentials: %w", err)
		}
		return nil
	}

	// THIRD: Fall back to browser-based OAuth flow (scenario 1 - CLI only)
	// This only works for CLI usage (tptctl), not for controllers
	util.CliOutputInfo("GCP credentials not found or expired. Initiating authentication...")

	// perform the OAuth flow
	if err := performGCPOAuthFlow(ctx); err != nil {
		return fmt.Errorf("failed to authenticate with GCP: %w", err)
	}

	util.CliOutputInfo("GCP authentication successful!")
	return nil
}

// configureServiceAccountCredentials writes the service account JSON to a
// temporary file and records its path on the receiver. It does not mutate the
// process environment: every GCP client and the Pulumi provider receive the
// credentials per call, so concurrent instances with different credentials stay
// isolated rather than sharing one process-global environment variable.
func (i *KubernetesRuntimeInfraGKE) configureServiceAccountCredentials(credentialsJSON string) error {
	// create a temporary file for the credentials
	tmpFile, err := os.CreateTemp("", "gcp-sa-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp credentials file: %w", err)
	}

	// write the credentials JSON to the file
	if _, err := tmpFile.WriteString(credentialsJSON); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return fmt.Errorf("failed to write credentials to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFile.Name())
		return fmt.Errorf("failed to close temp credentials file: %w", err)
	}

	// record the path on the receiver for callers that need a credentials file
	// rather than mutating a process-global environment variable
	i.credentialsFilePath = tmpFile.Name()

	return nil
}

// gcpClientOptions returns the client options used to construct GCP SDK clients
// for this instance. When explicit service account credentials are configured it
// returns them as a per-call option so the client never depends on a process-wide
// environment variable; otherwise it returns only the requested scopes and the
// client falls back to Application Default Credentials. Passing scopes is
// optional: omit them to inherit the client's default scope.
func (i *KubernetesRuntimeInfraGKE) gcpClientOptions(scopes ...string) []option.ClientOption {
	var opts []option.ClientOption
	if len(scopes) > 0 {
		opts = append(opts, option.WithScopes(scopes...))
	}
	if i.ServiceAccountCredentials != "" {
		opts = append(opts, option.WithCredentialsJSON([]byte(i.ServiceAccountCredentials)))
	}
	return opts
}

// tokenSource returns an OAuth2 token source for the given scopes, derived from
// the configured service account credentials when present and from Application
// Default Credentials otherwise. Deriving from the in-memory credentials keeps
// the token scoped to this instance instead of whatever the process default
// resolves to.
func (i *KubernetesRuntimeInfraGKE) tokenSource(ctx context.Context, scopes ...string) (oauth2.TokenSource, error) {
	if i.ServiceAccountCredentials != "" {
		creds, err := google.CredentialsFromJSON(ctx, []byte(i.ServiceAccountCredentials), scopes...)
		if err != nil {
			return nil, fmt.Errorf("failed to derive credentials from service account JSON: %w", err)
		}
		return creds.TokenSource, nil
	}
	return google.DefaultTokenSource(ctx, scopes...)
}

// hasValidGCPCredentials checks if valid Application Default Credentials exist
func hasValidGCPCredentials(ctx context.Context) bool {
	// try to get a token using the default credentials
	tokenSource, err := google.DefaultTokenSource(ctx, gcpOAuthScopes...)
	if err != nil {
		return false
	}

	// try to get a token to verify the credentials are valid
	token, err := tokenSource.Token()
	if err != nil {
		return false
	}

	// check if the token is valid and not expired
	return token.Valid()
}

// performGCPOAuthFlow performs the browser-based OAuth flow for GCP authentication
func performGCPOAuthFlow(ctx context.Context) error {
	// create a random state for CSRF protection
	state, err := generateRandomState()
	if err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}

	// find an available port for the callback server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURL := fmt.Sprintf("http://localhost:%d/callback", port)

	// create OAuth2 config
	oauth2Config := &oauth2.Config{
		ClientID:     gcpOAuthClientID,
		ClientSecret: gcpOAuthClientSecret,
		Endpoint:     google.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       gcpOAuthScopes,
	}

	// channel to receive the authorization code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// create HTTP server to handle the callback
	server := &http.Server{}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		// verify state parameter
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("invalid state parameter")
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			return
		}

		// check for errors
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			errChan <- fmt.Errorf("OAuth error: %s - %s", errMsg, r.URL.Query().Get("error_description"))
			http.Error(w, "Authentication failed", http.StatusBadRequest)
			return
		}

		// get the authorization code
		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no authorization code received")
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		// send success response to browser
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h1>Authentication Successful!</h1><p>You can close this window and return to the terminal.</p></body></html>`)

		codeChan <- code
	})

	// start the server
	go func() {
		if err := server.Serve(listener); err != http.ErrServerClosed {
			errChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	// generate the authorization URL
	authURL := oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	util.CliOutputNotice("Opening browser for GCP authentication...")
	util.CliOutputInfo("If the browser doesn't open automatically, please visit:")
	util.CliOutputInfo(authURL)
	fmt.Println()

	// try to open the browser
	if err := openBrowser(authURL); err != nil {
		util.CliOutputWarning("Failed to open browser automatically. Please open the URL above manually.")
	}

	// wait for the authorization code or error
	var code string
	select {
	case code = <-codeChan:
		// success
	case err := <-errChan:
		server.Shutdown(ctx)
		return err
	case <-time.After(5 * time.Minute):
		server.Shutdown(ctx)
		return fmt.Errorf("authentication timed out after 5 minutes")
	}

	// shutdown the callback server
	server.Shutdown(ctx)

	// exchange the authorization code for tokens
	token, err := oauth2Config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("failed to exchange authorization code: %w", err)
	}

	// save the credentials to the ADC file
	if err := saveADCCredentials(token); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	return nil
}

// saveADCCredentials saves the OAuth2 token as Application Default Credentials
func saveADCCredentials(token *oauth2.Token) error {
	// get the ADC file path
	adcPath, err := getADCPath()
	if err != nil {
		return err
	}

	// ensure the directory exists
	adcDir := filepath.Dir(adcPath)
	if err := os.MkdirAll(adcDir, 0700); err != nil {
		return fmt.Errorf("failed to create ADC directory: %w", err)
	}

	// create the credentials structure
	creds := adcCredentials{
		ClientID:     gcpOAuthClientID,
		ClientSecret: gcpOAuthClientSecret,
		RefreshToken: token.RefreshToken,
		Type:         "authorized_user",
	}

	// marshal to JSON
	credsJSON, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// write to file
	if err := os.WriteFile(adcPath, credsJSON, 0600); err != nil {
		return fmt.Errorf("failed to write ADC file: %w", err)
	}

	return nil
}

// getADCPath returns the path to the Application Default Credentials file
func getADCPath() (string, error) {
	// check for GOOGLE_APPLICATION_CREDENTIALS environment variable
	if path := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); path != "" {
		return path, nil
	}

	// use the default location
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	// on Windows, the path is different
	if runtime.GOOS == "windows" {
		return filepath.Join(homeDir, "AppData", "Roaming", "gcloud", "application_default_credentials.json"), nil
	}

	return filepath.Join(homeDir, ".config", "gcloud", "application_default_credentials.json"), nil
}

// generateRandomState generates a random state string for CSRF protection
func generateRandomState() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// openBrowser opens the specified URL in the default browser
func openBrowser(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		// try xdg-open first, then fall back to other options
		if _, err := exec.LookPath("xdg-open"); err == nil {
			cmd = exec.Command("xdg-open", url)
		} else if _, err := exec.LookPath("gnome-open"); err == nil {
			cmd = exec.Command("gnome-open", url)
		} else if _, err := exec.LookPath("kde-open"); err == nil {
			cmd = exec.Command("kde-open", url)
		} else {
			return fmt.Errorf("no browser opener found")
		}
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", strings.ReplaceAll(url, "&", "^&"))
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	return cmd.Start()
}

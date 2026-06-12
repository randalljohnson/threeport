package gcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/gcp/notif"
	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	kube "github.com/threeport/threeport/pkg/kube/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
)

// gkeLifecycle implements provider.InfraLifecycleProvider for GCP GKE
// runtime instances.
type gkeLifecycle struct {
	r          *controller.Reconciler
	instanceID uint
	instance   *v0.GcpGkeKubernetesRuntimeInstance
	log        *logr.Logger
}

// newGkeLifecycleProvider constructs an InfraLifecycleProvider for GKE.
func newGkeLifecycleProvider(
	r *controller.Reconciler,
	instance *v0.GcpGkeKubernetesRuntimeInstance,
	log *logr.Logger,
) *gkeLifecycle {
	return &gkeLifecycle{
		r:          r,
		instanceID: *instance.ID,
		instance:   instance,
		log:        log,
	}
}

// GetReconciliation fetches the latest reconciliation state from the API.
func (g *gkeLifecycle) GetReconciliation() (*provider.ReconciliationSnapshot, error) {
	latest, err := client.GetGcpGkeKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest GKE instance: %w", err)
	}
	creationFailed := false
	if latest.CreationFailed != nil {
		creationFailed = *latest.CreationFailed
	}
	return &provider.ReconciliationSnapshot{
		CreationAcknowledged: latest.CreationAcknowledged,
		CreationConfirmed:    latest.CreationConfirmed,
		CreationFailed:       creationFailed,
		DeletionScheduled:    latest.DeletionScheduled,
		DeletionAcknowledged: latest.DeletionAcknowledged,
		DeletionConfirmed:    latest.DeletionConfirmed,
		ResourceInventory:    latest.ResourceInventory,
	}, nil
}

// BuildInfra constructs the GKE infrastructure provider from API objects.
func (g *gkeLifecycle) BuildInfra() (provider.InfraProvider, error) {
	latest, err := client.GetGcpGkeKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get GKE instance for infra build: %w", err)
	}
	if latest.GcpGkeKubernetesRuntimeDefinitionID == nil {
		return nil, fmt.Errorf("GKE instance missing required field GcpGkeKubernetesRuntimeDefinitionID")
	}
	def, err := client.GetGcpGkeKubernetesRuntimeDefinitionByID(
		g.r.APIClient,
		g.r.APIServer,
		*latest.GcpGkeKubernetesRuntimeDefinitionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get GKE definition: %w", err)
	}
	return buildGkeInfra(g.r, latest, def, g.log)
}

// IsCreateComplete checks whether resource inventory has been persisted.
func (g *gkeLifecycle) IsCreateComplete() (bool, error) {
	latest, err := client.GetGcpGkeKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to check GKE creation status: %w", err)
	}
	if latest.ResourceInventory == nil {
		return false, nil
	}
	// trim whitespace and reject the placeholder forms that signal an empty or
	// cleared inventory. this is intentionally stricter than the provider
	// lifecycle's cleared-inventory check, which compares raw bytes and handles
	// neither padding, arrays, nor a quoted null; the two deliberately diverge
	inventory := strings.TrimSpace(string(*latest.ResourceInventory))
	switch inventory {
	case "", "{}", "null", `"null"`, "[]":
		return false, nil
	}
	return true, nil
}

// OnCreateConfirmed gets connection info and updates the kubernetes runtime instance.
func (g *gkeLifecycle) OnCreateConfirmed(infra provider.InfraProvider) error {
	infraGKE, ok := infra.(*provider.KubernetesRuntimeInfraGKE)
	if !ok {
		return fmt.Errorf("expected a GKE infra provider but got %T", infra)
	}
	kubeConnectionInfo, err := infraGKE.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes API connection info: %w", err)
	}
	return g.updateKubeRuntimeConnection(kubeConnectionInfo)
}

// updateKubeRuntimeConnection writes the GKE cluster connection info onto the
// linked kubernetes runtime instance. It is split out from OnCreateConfirmed so
// it can be unit tested without the environment-dependent GetConnection call.
func (g *gkeLifecycle) updateKubeRuntimeConnection(kubeConnectionInfo *kube.KubeConnectionInfo) error {
	// reject partial connection info so a transient cloud read cannot write
	// empty connection fields onto the kubernetes runtime instance; the
	// resulting error requeues the confirmation for a later retry
	if kubeConnectionInfo.APIEndpoint == "" ||
		kubeConnectionInfo.CACertificate == "" ||
		kubeConnectionInfo.Token == "" {
		return fmt.Errorf(
			"incomplete kube connection info for GKE cluster: API endpoint, CA certificate, and token are all required",
		)
	}

	latest, err := client.GetGcpGkeKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get GKE instance for connection update: %w", err)
	}
	if latest.KubernetesRuntimeInstanceID == nil {
		return fmt.Errorf("GKE instance missing required field KubernetesRuntimeInstanceID")
	}
	kubernetesRuntimeInstance, err := client.GetKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		*latest.KubernetesRuntimeInstanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get kubernetes runtime instance: %w", err)
	}

	kubeRuntimeReconciled := false
	kubernetesRuntimeInstance.APIEndpoint = &kubeConnectionInfo.APIEndpoint
	kubernetesRuntimeInstance.CACertificate = &kubeConnectionInfo.CACertificate
	kubernetesRuntimeInstance.ConnectionToken = &kubeConnectionInfo.Token
	kubernetesRuntimeInstance.ConnectionTokenExpiration = &kubeConnectionInfo.TokenExpiration
	kubernetesRuntimeInstance.Reconciled = &kubeRuntimeReconciled
	if _, err = client.UpdateKubernetesRuntimeInstance(
		g.r.APIClient,
		g.r.APIServer,
		kubernetesRuntimeInstance,
	); err != nil {
		return fmt.Errorf("failed to update kubernetes runtime instance with kube connection info: %w", err)
	}
	return nil
}

// SaveCreateOutputs saves the final Pulumi state.
func (g *gkeLifecycle) SaveCreateOutputs(_ provider.InfraProvider, state *datatypes.JSON) error {
	updatedInstance := v0.GcpGkeKubernetesRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: state,
	}
	if _, err := client.UpdateGcpGkeKubernetesRuntimeInstance(
		g.r.APIClient,
		g.r.APIServer,
		&updatedInstance,
	); err != nil {
		return fmt.Errorf("failed to update GKE instance with resource inventory: %w", err)
	}
	return nil
}

// OnDeleteConfirmed performs provider-specific post-deletion cleanup.
func (g *gkeLifecycle) OnDeleteConfirmed(_ provider.InfraProvider) error {
	return nil
}

// AckCreation sets CreationAcknowledged and clears CreationFailed.
func (g *gkeLifecycle) AckCreation() error {
	ackTimestamp := time.Now().UTC()
	creationFailed := false
	ackUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationAcknowledged: &ackTimestamp,
			CreationFailed:       &creationFailed,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// RefreshCreationAck updates CreationAcknowledged to prevent stale detection.
func (g *gkeLifecycle) RefreshCreationAck() error {
	refreshTimestamp := time.Now().UTC()
	ackUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationAcknowledged: &refreshTimestamp,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// SetCreationFailed marks CreationFailed=true in the API.
func (g *gkeLifecycle) SetCreationFailed() error {
	creationFailed := true
	failedUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationFailed: &creationFailed,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &failedUpdate)
	return err
}

// ConfirmCreation sets CreationConfirmed and Reconciled=true.
func (g *gkeLifecycle) ConfirmCreation() error {
	reconciled := true
	timestamp := time.Now().UTC()
	confirmedUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			Reconciled:        &reconciled,
			CreationConfirmed: &timestamp,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &confirmedUpdate)
	return err
}

// AckDeletion sets DeletionAcknowledged in the API.
func (g *gkeLifecycle) AckDeletion() error {
	timestamp := time.Now().UTC()
	ackUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionAcknowledged: &timestamp,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// RefreshDeletionAck updates DeletionAcknowledged to prevent stale detection.
func (g *gkeLifecycle) RefreshDeletionAck() error {
	refreshTimestamp := time.Now().UTC()
	ackUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionAcknowledged: &refreshTimestamp,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// ConfirmDeletion sets DeletionConfirmed in the API.
func (g *gkeLifecycle) ConfirmDeletion() error {
	timestamp := time.Now().UTC()
	confirmedUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionConfirmed: &timestamp,
		},
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &confirmedUpdate)
	return err
}

// SaveState persists intermediate Pulumi state to the API.
func (g *gkeLifecycle) SaveState(state *datatypes.JSON) error {
	stateUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: state,
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &stateUpdate)
	return err
}

// ClearInventory sets ResourceInventory to "{}" to signal destroy complete.
func (g *gkeLifecycle) ClearInventory() error {
	emptyInventory := datatypes.JSON([]byte("{}"))
	clearedUpdate := v0.GcpGkeKubernetesRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: &emptyInventory,
	}
	_, err := client.UpdateGcpGkeKubernetesRuntimeInstance(g.r.APIClient, g.r.APIServer, &clearedUpdate)
	return err
}

// PublishCreateNotification publishes a NATS notification for creation.
func (g *gkeLifecycle) PublishCreateNotification() error {
	notifPayload, err := g.instance.NotificationPayload(
		notifications.NotificationOperationCreated,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = g.r.JetStreamContext.Publish(
		notif.GcpGkeKubernetesRuntimeInstanceCreateSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish create notification: %w", err)
	}
	return nil
}

// PublishDeleteNotification publishes a NATS notification for deletion.
func (g *gkeLifecycle) PublishDeleteNotification() error {
	notifPayload, err := g.instance.NotificationPayload(
		notifications.NotificationOperationDeleted,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = g.r.JetStreamContext.Publish(
		notif.GcpGkeKubernetesRuntimeInstanceDeleteSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish delete notification: %w", err)
	}
	return nil
}

// buildGkeInfra constructs a KubernetesRuntimeInfraGKE from API objects.
func buildGkeInfra(
	r *controller.Reconciler,
	instance *v0.GcpGkeKubernetesRuntimeInstance,
	definition *v0.GcpGkeKubernetesRuntimeDefinition,
	log *logr.Logger,
) (*provider.KubernetesRuntimeInfraGKE, error) {
	// validate required pointers before dereference so a malformed API
	// object yields an error instead of panicking the create goroutine
	switch {
	case instance.GcpProviderID == nil:
		return nil, fmt.Errorf("GKE instance missing required field GcpProviderID")
	case instance.Name == nil:
		return nil, fmt.Errorf("GKE instance missing required field Name")
	case instance.Region == nil:
		return nil, fmt.Errorf("GKE instance missing required field Region")
	case definition.DefaultNodeGroupInitialSize == nil:
		return nil, fmt.Errorf("GKE definition missing required field DefaultNodeGroupInitialSize")
	}

	gcpProvider, err := client.GetGcpProviderByID(
		r.APIClient,
		r.APIServer,
		*instance.GcpProviderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve GCP provider by ID: %w", err)
	}
	if gcpProvider.ProjectID == nil {
		return nil, fmt.Errorf("GCP provider missing required field ProjectID")
	}

	infraGKE := &provider.KubernetesRuntimeInfraGKE{
		PulumiWorkspace: provider.PulumiWorkspace{
			RuntimeInstanceName: *instance.Name,
			Logger:              log,
		},
		ProjectID:              *gcpProvider.ProjectID,
		Region:                 *instance.Region,
		WorkerNodeInitialCount: int32(*definition.DefaultNodeGroupInitialSize),
	}

	if gcpProvider.ServiceAccountCredentials != nil && *gcpProvider.ServiceAccountCredentials != "" {
		infraGKE.ServiceAccountCredentials = *gcpProvider.ServiceAccountCredentials
	}

	return infraGKE, nil
}

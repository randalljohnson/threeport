package gcp

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"

	notif "github.com/threeport/threeport/internal/gcp/notif"
	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
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

// ensure gkeLifecycle implements the lifecycle interface the infrastructure
// handlers call.
var _ provider.InfraLifecycleProvider = (*gkeLifecycle)(nil)

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
	reconciled := false
	if latest.Reconciled != nil {
		reconciled = *latest.Reconciled
	}
	return &provider.ReconciliationSnapshot{
		Reconciled:        reconciled,
		CreationConfirmed: latest.CreationConfirmed,
		CreationFailed:    creationFailed,
		DeletionScheduled: latest.DeletionScheduled,
		DeletionConfirmed: latest.DeletionConfirmed,
		ResourceInventory: latest.ResourceInventory,
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

// UpdateReconciliation persists the next reconciliation snapshot in a single
// PATCH. Pointer fields are written only when the handler set them; the boolean
// reconciliation flags are always written from the snapshot, since their zero
// value (not reconciled, not failed) is the correct in-progress state.
func (g *gkeLifecycle) UpdateReconciliation(snapshot provider.ReconciliationSnapshot) error {
	reconciled := snapshot.Reconciled
	creationFailed := snapshot.CreationFailed
	update := v0.GcpGkeKubernetesRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: snapshot.ResourceInventory,
		Reconciliation: v0.Reconciliation{
			Reconciled:        &reconciled,
			CreationFailed:    &creationFailed,
			CreationConfirmed: snapshot.CreationConfirmed,
			DeletionConfirmed: snapshot.DeletionConfirmed,
		},
	}
	if _, err := client.UpdateGcpGkeKubernetesRuntimeInstance(
		g.r.APIClient,
		g.r.APIServer,
		&update,
	); err != nil {
		return fmt.Errorf("failed to update GKE reconciliation state: %w", err)
	}
	return nil
}

// OnCreateConfirmed gets connection info and updates the kubernetes runtime instance.
func (g *gkeLifecycle) OnCreateConfirmed(infra provider.InfraProvider) error {
	infraGKE := infra.(*provider.KubernetesRuntimeInfraGKE)
	kubeConnectionInfo, err := infraGKE.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes API connection info: %w", err)
	}

	latest, err := client.GetGcpGkeKubernetesRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get GKE instance for connection update: %w", err)
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

// OnDeleteConfirmed performs provider-specific post-deletion cleanup once the
// stack is empty: it removes the non-Pulumi GCP service account and IAM bindings.
// Running it here, rather than on every destroy step, keeps the cleanup to a
// single pass after teardown is confirmed.
func (g *gkeLifecycle) OnDeleteConfirmed(infra provider.InfraProvider) error {
	infraGKE, ok := infra.(*provider.KubernetesRuntimeInfraGKE)
	if !ok {
		return fmt.Errorf("expected *provider.KubernetesRuntimeInfraGKE, got %T", infra)
	}
	if err := infraGKE.DeleteGCPResources(context.Background()); err != nil {
		return fmt.Errorf("failed to clean up GCP resources: %w", err)
	}
	return nil
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
	gcpProvider, err := client.GetGcpProviderByID(
		r.APIClient,
		r.APIServer,
		*instance.GcpProviderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve GCP provider by ID: %w", err)
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

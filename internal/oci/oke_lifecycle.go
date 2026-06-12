package oci

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/oracle/oci-go-sdk/v65/common"
	ociidentity "github.com/oracle/oci-go-sdk/v65/identity"

	notif "github.com/threeport/threeport/internal/oci/notif"
	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// okeLifecycle implements provider.InfraLifecycleProvider for OCI OKE
// runtime instances.
type okeLifecycle struct {
	r          *controller.Reconciler
	instanceID uint
	instance   *v0.OciOkeKubernetesRuntimeInstance
	log        *logr.Logger
}

// ensure okeLifecycle implements the lifecycle interface the infrastructure
// handlers call.
var _ provider.InfraLifecycleProvider = (*okeLifecycle)(nil)

// newOkeLifecycleProvider constructs an InfraLifecycleProvider for OKE.
func newOkeLifecycleProvider(
	r *controller.Reconciler,
	instance *v0.OciOkeKubernetesRuntimeInstance,
	log *logr.Logger,
) *okeLifecycle {
	return &okeLifecycle{
		r:          r,
		instanceID: *instance.ID,
		instance:   instance,
		log:        log,
	}
}

// GetReconciliation fetches the latest reconciliation state from the API.
func (o *okeLifecycle) GetReconciliation() (*provider.ReconciliationSnapshot, error) {
	latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
		o.r.APIClient,
		o.r.APIServer,
		o.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest OKE instance: %w", err)
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

// UpdateReconciliation persists the next reconciliation snapshot in a single
// PATCH. Pointer fields are written only when the handler set them; the boolean
// reconciliation flags are always written from the snapshot, since their zero
// value (not reconciled, not failed) is the correct in-progress state.
func (o *okeLifecycle) UpdateReconciliation(snapshot provider.ReconciliationSnapshot) error {
	reconciled := snapshot.Reconciled
	creationFailed := snapshot.CreationFailed
	update := v0.OciOkeKubernetesRuntimeInstance{
		Common:            v0.Common{ID: &o.instanceID},
		ResourceInventory: snapshot.ResourceInventory,
		Reconciliation: v0.Reconciliation{
			Reconciled:        &reconciled,
			CreationFailed:    &creationFailed,
			CreationConfirmed: snapshot.CreationConfirmed,
			DeletionConfirmed: snapshot.DeletionConfirmed,
		},
	}
	if _, err := client.UpdateOciOkeKubernetesRuntimeInstance(
		o.r.APIClient,
		o.r.APIServer,
		&update,
	); err != nil {
		return fmt.Errorf("failed to update OKE reconciliation state: %w", err)
	}
	return nil
}

// BuildInfra constructs the OKE infrastructure provider from API objects.
func (o *okeLifecycle) BuildInfra() (provider.InfraProvider, error) {
	latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
		o.r.APIClient,
		o.r.APIServer,
		o.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get OKE instance for infra build: %w", err)
	}
	def, err := client.GetOciOkeKubernetesRuntimeDefinitionByID(
		o.r.APIClient,
		o.r.APIServer,
		*latest.OciOkeKubernetesRuntimeDefinitionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get OKE definition: %w", err)
	}
	return buildOkeInfra(o.r, latest, def, o.log)
}

// OnCreateConfirmed records the cluster OCID, gets connection info, and updates the
// kubernetes runtime instance. The cluster OCID is persisted here because the
// handler's single reconciliation write carries only generic lifecycle fields; the
// provider-specific OCID would otherwise be lost when the create is confirmed.
func (o *okeLifecycle) OnCreateConfirmed(infra provider.InfraProvider) error {
	infraOKE := infra.(*provider.KubernetesRuntimeInfraOKE)

	// get the cluster OCID so it can be persisted on the instance record
	clusterOCID, err := infraOKE.GetClusterOCID(infraOKE.RuntimeInstanceName)
	if err != nil {
		return fmt.Errorf("failed to get OKE cluster OCID: %w", err)
	}
	ocidUpdate := v0.OciOkeKubernetesRuntimeInstance{
		Common:      v0.Common{ID: &o.instanceID},
		ClusterOCID: &clusterOCID,
	}
	if _, err := client.UpdateOciOkeKubernetesRuntimeInstance(
		o.r.APIClient,
		o.r.APIServer,
		&ocidUpdate,
	); err != nil {
		return fmt.Errorf("failed to update OKE instance with cluster OCID: %w", err)
	}

	// get kubernetes cluster connection info
	kubeConnectionInfo, err := infraOKE.GetConnection()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes API connection info: %w", err)
	}

	// get latest instance to find KubernetesRuntimeInstanceID
	latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
		o.r.APIClient,
		o.r.APIServer,
		o.instanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get OKE instance for connection update: %w", err)
	}

	// get kubernetes runtime instance to update kube connection info
	kubernetesRuntimeInstance, err := client.GetKubernetesRuntimeInstanceByID(
		o.r.APIClient,
		o.r.APIServer,
		*latest.KubernetesRuntimeInstanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get kubernetes runtime instance: %w", err)
	}

	// update kube connection info
	kubeRuntimeReconciled := false
	kubernetesRuntimeInstance.APIEndpoint = &kubeConnectionInfo.APIEndpoint
	kubernetesRuntimeInstance.CACertificate = &kubeConnectionInfo.CACertificate
	kubernetesRuntimeInstance.ConnectionToken = &kubeConnectionInfo.Token
	kubernetesRuntimeInstance.ConnectionTokenExpiration = &kubeConnectionInfo.TokenExpiration
	kubernetesRuntimeInstance.Reconciled = &kubeRuntimeReconciled
	if _, err = client.UpdateKubernetesRuntimeInstance(
		o.r.APIClient,
		o.r.APIServer,
		kubernetesRuntimeInstance,
	); err != nil {
		return fmt.Errorf("failed to update kubernetes runtime instance with kube connection info: %w", err)
	}

	return nil
}

// OnDeleteConfirmed deletes the compartment and cleans up associated resources.
func (o *okeLifecycle) OnDeleteConfirmed(infra provider.InfraProvider) error {
	infraOKE := infra.(*provider.KubernetesRuntimeInfraOKE)

	// delete compartment — this is critical, failing here means
	// cloud resources are orphaned so we must return the error
	if err := infraOKE.DeleteCompartment(); err != nil {
		return fmt.Errorf("failed to delete OCI compartment: %w", err)
	}

	// delete Pulumi stack state (best-effort, just metadata)
	if err := infraOKE.DeleteStackState(); err != nil {
		o.log.Error(err, "failed to delete Pulumi stack state")
	}

	// clean up the associated KubernetesRuntimeInstance so it doesn't
	// get orphaned if the CLI is interrupted during deletion
	latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
		o.r.APIClient,
		o.r.APIServer,
		o.instanceID,
	)
	if err != nil {
		o.log.Error(err, "failed to get OKE instance for KubernetesRuntimeInstance cleanup")
		return nil
	}

	if latest.KubernetesRuntimeInstanceID != nil {
		// mark as deletion-confirmed so the API handler allows hard-delete
		deletionTimestamp := util.Ptr(time.Now().UTC())
		deletedKRI := v0.KubernetesRuntimeInstance{
			Common: v0.Common{ID: latest.KubernetesRuntimeInstanceID},
			Reconciliation: v0.Reconciliation{
				DeletionAcknowledged: deletionTimestamp,
				DeletionConfirmed:    deletionTimestamp,
				Reconciled:           util.Ptr(true),
			},
		}
		if _, err := client.UpdateKubernetesRuntimeInstance(
			o.r.APIClient,
			o.r.APIServer,
			&deletedKRI,
		); err != nil {
			o.log.Error(err, "failed to update KubernetesRuntimeInstance for deletion")
			return nil
		}

		if _, err := client.DeleteKubernetesRuntimeInstance(
			o.r.APIClient,
			o.r.APIServer,
			*latest.KubernetesRuntimeInstanceID,
		); err != nil {
			o.log.Error(err, "failed to delete KubernetesRuntimeInstance")
		}
	}

	return nil
}

// PublishCreateNotification publishes a NATS notification for creation.
func (o *okeLifecycle) PublishCreateNotification() error {
	notifPayload, err := o.instance.NotificationPayload(
		notifications.NotificationOperationCreated,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = o.r.JetStreamContext.Publish(
		notif.OciOkeKubernetesRuntimeInstanceCreateSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish create notification: %w", err)
	}
	return nil
}

// PublishDeleteNotification publishes a NATS notification for deletion.
func (o *okeLifecycle) PublishDeleteNotification() error {
	notifPayload, err := o.instance.NotificationPayload(
		notifications.NotificationOperationDeleted,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = o.r.JetStreamContext.Publish(
		notif.OciOkeKubernetesRuntimeInstanceDeleteSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish delete notification: %w", err)
	}
	return nil
}

// buildOkeInfra constructs a KubernetesRuntimeInfraOKE from API objects.
func buildOkeInfra(
	r *controller.Reconciler,
	instance *v0.OciOkeKubernetesRuntimeInstance,
	definition *v0.OciOkeKubernetesRuntimeDefinition,
	log *logr.Logger,
) (*provider.KubernetesRuntimeInfraOKE, error) {
	// get OCI provider
	ociProvider, err := client.GetOciProviderByID(
		r.APIClient,
		r.APIServer,
		*instance.OciProviderID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve OCI provider by ID: %w", err)
	}

	// decrypt private key
	decryptedPrivateKey, err := encryption.Decrypt(r.EncryptionKey, *ociProvider.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt OCI provider private key: %w", err)
	}

	// construct OCI config provider from provider credentials
	configProvider := common.NewRawConfigurationProvider(
		*ociProvider.TenancyOCID,
		*ociProvider.UserOCID,
		*ociProvider.DefaultRegion,
		*ociProvider.KeyFingerprint,
		decryptedPrivateKey,
		nil,
	)

	// use instance region if set, otherwise fall back to provider default region
	region := *ociProvider.DefaultRegion
	if instance.Region != nil && *instance.Region != "" {
		region = *instance.Region
	}

	infraOKE := &provider.KubernetesRuntimeInfraOKE{
		PulumiWorkspace: provider.PulumiWorkspace{
			RuntimeInstanceName: *instance.Name,
			ProjectName:         "oke",
			ProjectDescription:  "Oracle Kubernetes Engine (OKE) cluster for Threeport",
			StackConfigs:        map[string]string{"oci:region": region},
			Logger:              log,
		},
		Region:                 region,
		ConfigProvider:         configProvider,
		WorkerNodeShape:        *definition.WorkerNodeShape,
		WorkerNodeInitialCount: *definition.WorkerNodeInitialCount,
		Version:                provider.DefaultOKEKubernetesVersion,
		ServiceUserOCID:        *ociProvider.UserOCID,
		Fingerprint:            *ociProvider.KeyFingerprint,
		PrivateKeyPEM:          decryptedPrivateKey,
	}

	// resolve the compartment OCID by looking up the workload compartment
	// under the parent (genesis compartment from OCI provider record)
	identityClient, err := ociidentity.NewIdentityClientWithConfigurationProvider(configProvider)
	if err != nil {
		return nil, fmt.Errorf("failed to create identity client: %w", err)
	}
	identityClient.SetRegion(region)

	listRequest := ociidentity.ListCompartmentsRequest{
		CompartmentId: ociProvider.CompartmentOCID,
		Name:          instance.Name,
	}
	listResponse, err := identityClient.ListCompartments(context.Background(), listRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to list compartments to resolve compartment OCID: %w", err)
	}
	if len(listResponse.Items) > 0 {
		infraOKE.CompartmentOCID = *listResponse.Items[0].Id
	} else {
		// compartment doesn't exist yet — set parent as CompartmentOCID
		// so createOCICompartment will create the child under it
		infraOKE.CompartmentOCID = *ociProvider.CompartmentOCID
	}

	return infraOKE, nil
}

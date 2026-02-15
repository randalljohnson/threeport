package oci

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/oracle/oci-go-sdk/v65/common"
	ociidentity "github.com/oracle/oci-go-sdk/v65/identity"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/oci/notif"
	"github.com/threeport/threeport/internal/provider"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// newOkeLifecycleConfig constructs an InfraLifecycleConfig with all
// OKE-specific closures for a given runtime instance.
func newOkeLifecycleConfig(
	r *controller.Reconciler,
	instance *v0.OciOkeKubernetesRuntimeInstance,
	log *logr.Logger,
) provider.InfraLifecycleConfig {
	instanceID := *instance.ID

	return provider.InfraLifecycleConfig{
		GetReconciliation: func() (*provider.ReconciliationSnapshot, error) {
			latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
				instanceID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get latest OKE instance: %w", err)
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
		},

		BuildInfra: func() (provider.InfraProvider, error) {
			// re-fetch instance for latest state
			latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
				instanceID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get OKE instance for infra build: %w", err)
			}
			def, err := client.GetOciOkeKubernetesRuntimeDefinitionByID(
				r.APIClient,
				r.APIServer,
				*latest.OciOkeKubernetesRuntimeDefinitionID,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get OKE definition: %w", err)
			}
			return buildOkeInfra(r, latest, def, log)
		},

		IsCreateComplete: func() (bool, error) {
			latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
				instanceID,
			)
			if err != nil {
				return false, fmt.Errorf("failed to check OKE cluster creation status: %w", err)
			}
			return latest.ClusterOCID != nil && *latest.ClusterOCID != "", nil
		},

		OnCreateConfirmed: func(infra provider.InfraProvider) error {
			infraOKE := infra.(*provider.KubernetesRuntimeInfraOKE)

			// get kubernetes cluster connection info
			kubeConnectionInfo, err := infraOKE.GetConnection()
			if err != nil {
				return fmt.Errorf("failed to get Kubernetes API connection info: %w", err)
			}

			// get latest instance to find KubernetesRuntimeInstanceID
			latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
				instanceID,
			)
			if err != nil {
				return fmt.Errorf("failed to get OKE instance for connection update: %w", err)
			}

			// get kubernetes runtime instance to update kube connection info
			kubernetesRuntimeInstance, err := client.GetKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
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
				r.APIClient,
				r.APIServer,
				kubernetesRuntimeInstance,
			); err != nil {
				return fmt.Errorf("failed to update kubernetes runtime instance with kube connection info: %w", err)
			}

			return nil
		},

		SaveCreateOutputs: func(infra provider.InfraProvider, state *datatypes.JSON) error {
			infraOKE := infra.(*provider.KubernetesRuntimeInfraOKE)

			// get cluster OCID
			clusterOCID, err := infraOKE.GetClusterOCID(infraOKE.RuntimeInstanceName)
			if err != nil {
				return fmt.Errorf("failed to get OKE cluster OCID: %w", err)
			}

			// update instance with final state and cluster OCID
			updatedInstance := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				ResourceInventory: state,
				ClusterOCID:       &clusterOCID,
			}
			if _, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&updatedInstance,
			); err != nil {
				log.Error(err, "failed to update OKE instance with resource inventory and cluster OCID")
			}

			return nil
		},

		OnDeleteConfirmed: func(infra provider.InfraProvider) error {
			infraOKE := infra.(*provider.KubernetesRuntimeInfraOKE)

			// delete compartment
			if err := infraOKE.DeleteCompartment(); err != nil {
				log.Error(err, "failed to delete OCI compartment")
			}

			// delete Pulumi stack state
			if err := infraOKE.DeleteStackState(); err != nil {
				log.Error(err, "failed to delete Pulumi stack state")
			}

			// clean up the associated KubernetesRuntimeInstance so it doesn't
			// get orphaned if the CLI is interrupted during deletion
			latest, err := client.GetOciOkeKubernetesRuntimeInstanceByID(
				r.APIClient,
				r.APIServer,
				instanceID,
			)
			if err != nil {
				log.Error(err, "failed to get OKE instance for KubernetesRuntimeInstance cleanup")
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
					r.APIClient,
					r.APIServer,
					&deletedKRI,
				); err != nil {
					log.Error(err, "failed to update KubernetesRuntimeInstance for deletion")
					return nil
				}

				if _, err := client.DeleteKubernetesRuntimeInstance(
					r.APIClient,
					r.APIServer,
					*latest.KubernetesRuntimeInstanceID,
				); err != nil {
					log.Error(err, "failed to delete KubernetesRuntimeInstance")
				}
			}

			return nil
		},

		AckCreation: func() error {
			ackTimestamp := time.Now().UTC()
			creationFailed := false
			ackUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					CreationAcknowledged: &ackTimestamp,
					CreationFailed:       &creationFailed,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&ackUpdate,
			)
			return err
		},

		RefreshCreationAck: func() error {
			refreshTimestamp := time.Now().UTC()
			ackUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					CreationAcknowledged: &refreshTimestamp,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&ackUpdate,
			)
			return err
		},

		SetCreationFailed: func() error {
			creationFailed := true
			failedUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					CreationFailed: &creationFailed,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&failedUpdate,
			)
			return err
		},

		ConfirmCreation: func() error {
			reconciled := true
			timestamp := time.Now().UTC()
			confirmedUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					Reconciled:        &reconciled,
					CreationConfirmed: &timestamp,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&confirmedUpdate,
			)
			return err
		},

		AckDeletion: func() error {
			timestamp := time.Now().UTC()
			ackUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					DeletionAcknowledged: &timestamp,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&ackUpdate,
			)
			return err
		},

		RefreshDeletionAck: func() error {
			refreshTimestamp := time.Now().UTC()
			ackUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					DeletionAcknowledged: &refreshTimestamp,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&ackUpdate,
			)
			return err
		},

		ConfirmDeletion: func() error {
			timestamp := time.Now().UTC()
			confirmedUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				Reconciliation: v0.Reconciliation{
					DeletionConfirmed: &timestamp,
				},
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&confirmedUpdate,
			)
			return err
		},

		SaveState: func(state *datatypes.JSON) error {
			stateUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				ResourceInventory: state,
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&stateUpdate,
			)
			return err
		},

		ClearInventory: func() error {
			emptyInventory := datatypes.JSON([]byte("{}"))
			clearedUpdate := v0.OciOkeKubernetesRuntimeInstance{
				Common: v0.Common{
					ID: &instanceID,
				},
				ResourceInventory: &emptyInventory,
			}
			_, err := client.UpdateOciOkeKubernetesRuntimeInstance(
				r.APIClient,
				r.APIServer,
				&clearedUpdate,
			)
			return err
		},

		PublishCreateNotification: func() error {
			notifPayload, err := instance.NotificationPayload(
				notifications.NotificationOperationCreated,
				false,
				time.Now().Unix(),
			)
			if err != nil {
				return fmt.Errorf("failed to create notification payload: %w", err)
			}
			if _, err = r.JetStreamContext.Publish(
				notif.OciOkeKubernetesRuntimeInstanceCreateSubject,
				*notifPayload,
			); err != nil {
				return fmt.Errorf("failed to publish create notification: %w", err)
			}
			return nil
		},

		PublishDeleteNotification: func() error {
			notifPayload, err := instance.NotificationPayload(
				notifications.NotificationOperationDeleted,
				false,
				time.Now().Unix(),
			)
			if err != nil {
				return fmt.Errorf("failed to create notification payload: %w", err)
			}
			if _, err = r.JetStreamContext.Publish(
				notif.OciOkeKubernetesRuntimeInstanceDeleteSubject,
				*notifPayload,
			); err != nil {
				return fmt.Errorf("failed to publish delete notification: %w", err)
			}
			return nil
		},

		Log: log,
	}
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
		*ociProvider.CompartmentOCID,
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

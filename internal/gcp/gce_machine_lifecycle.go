package gcp

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"gorm.io/datatypes"

	notif "github.com/threeport/threeport/internal/gcp/notif"
	"github.com/threeport/threeport/internal/provider"
	machine "github.com/threeport/threeport/internal/provider/machine"
	v0 "github.com/threeport/threeport/pkg/api/v0"
	client "github.com/threeport/threeport/pkg/client/v0"
	controller "github.com/threeport/threeport/pkg/controller/v0"
	encryption "github.com/threeport/threeport/pkg/encryption/v0"
	notifications "github.com/threeport/threeport/pkg/notifications/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// gceMachineLifecycle implements provider.InfraLifecycleProvider for GCP GCE
// machine runtime instances.
type gceMachineLifecycle struct {
	r          *controller.Reconciler
	instanceID uint
	instance   *v0.GcpGceMachineRuntimeInstance
	log        *logr.Logger
}

var _ provider.InfraLifecycleProvider = (*gceMachineLifecycle)(nil)

// StackKey returns the GCE instance name.
func (g *gceMachineLifecycle) StackKey() string {
	return *g.instance.Name
}

// newGceMachineLifecycleProvider constructs an InfraLifecycleProvider for a GCE
// machine runtime instance.
func newGceMachineLifecycleProvider(
	r *controller.Reconciler,
	instance *v0.GcpGceMachineRuntimeInstance,
	log *logr.Logger,
) *gceMachineLifecycle {
	return &gceMachineLifecycle{
		r:          r,
		instanceID: *instance.ID,
		instance:   instance,
		log:        log,
	}
}

// GetReconciliation fetches the latest reconciliation state from the API.
func (g *gceMachineLifecycle) GetReconciliation() (*provider.ReconciliationSnapshot, error) {
	latest, err := client.GetGcpGceMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest GCE instance: %w", err)
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

// BuildInfra constructs the GCE infrastructure provider from API objects.
func (g *gceMachineLifecycle) BuildInfra() (provider.InfraProvider, error) {
	latest, err := client.GetGcpGceMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get GCE instance for infra build: %w", err)
	}
	return buildGceMachineInfra(g.r, latest, g.log)
}

// IsCreateComplete reports whether persisted resource inventory is non-empty
// after trimming whitespace and rejecting placeholder values.
func (g *gceMachineLifecycle) IsCreateComplete() (bool, error) {
	latest, err := client.GetGcpGceMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return false, fmt.Errorf("failed to check GCE creation status: %w", err)
	}
	if latest.ResourceInventory == nil {
		return false, nil
	}
	inventory := strings.TrimSpace(string(*latest.ResourceInventory))
	switch inventory {
	case "", "{}", "[]", "null", `"null"`:
		return false, nil
	}
	return true, nil
}

// OnCreateConfirmed copies connection info onto the related machine runtime
// instance and clears its Reconciled flag.
func (g *gceMachineLifecycle) OnCreateConfirmed(_ provider.InfraProvider) error {
	latest, err := client.GetGcpGceMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		g.instanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get GCE instance for machine runtime update: %w", err)
	}
	if latest.MachineRuntimeInstanceID == nil {
		return fmt.Errorf("GCE instance missing required field MachineRuntimeInstanceID")
	}

	// get related machine runtime instance
	machineRuntimeInstance, err := client.GetMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		*latest.MachineRuntimeInstanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get machine runtime instance: %w", err)
	}

	// copy SSH connection fields; MRI hostname is ExternalIP
	machineRuntimeInstance.Hostname = latest.ExternalIP
	machineRuntimeInstance.SSHUser = latest.SSHUser
	machineRuntimeInstance.SSHKey = latest.SSHKey
	machineRuntimeInstance.Reconciled = util.Ptr(false)
	if _, err = client.UpdateMachineRuntimeInstance(
		g.r.APIClient,
		g.r.APIServer,
		machineRuntimeInstance,
	); err != nil {
		return fmt.Errorf("failed to update machine runtime instance with connection info: %w", err)
	}
	return nil
}

// SaveCreateOutputs writes hostname, external IP, SSH key, and final state
// onto the GCE machine runtime instance.
func (g *gceMachineLifecycle) SaveCreateOutputs(infra provider.InfraProvider, state *datatypes.JSON) error {
	gceInfra, ok := infra.(*machine.GceMachineInfra)
	if !ok {
		return fmt.Errorf(
			"failed to save GCE create outputs: expected *machine.GceMachineInfra, got %T",
			infra,
		)
	}

	hostname, externalIP, sshKey := gceInfra.CreateOutputs()
	updatedInstance := v0.GcpGceMachineRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		Hostname:          &hostname,
		ExternalIP:        &externalIP,
		SSHKey:            &sshKey,
		ResourceInventory: state,
	}
	if _, err := client.UpdateGcpGceMachineRuntimeInstance(
		g.r.APIClient,
		g.r.APIServer,
		&updatedInstance,
	); err != nil {
		return fmt.Errorf("failed to update GCE instance with create outputs: %w", err)
	}
	return nil
}

// OnDeleteConfirmed performs provider-specific post-deletion cleanup.
func (g *gceMachineLifecycle) OnDeleteConfirmed(_ provider.InfraProvider) error {
	return nil
}

// AckCreation sets CreationAcknowledged and clears CreationFailed.
func (g *gceMachineLifecycle) AckCreation() error {
	ackTimestamp := time.Now().UTC()
	creationFailed := false
	ackUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationAcknowledged: &ackTimestamp,
			CreationFailed:       &creationFailed,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// RefreshCreationAck updates CreationAcknowledged to prevent stale detection.
func (g *gceMachineLifecycle) RefreshCreationAck() error {
	refreshTimestamp := time.Now().UTC()
	ackUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationAcknowledged: &refreshTimestamp,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// SetCreationFailed marks CreationFailed=true in the API.
func (g *gceMachineLifecycle) SetCreationFailed() error {
	creationFailed := true
	failedUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			CreationFailed: &creationFailed,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &failedUpdate)
	return err
}

// ConfirmCreation sets CreationConfirmed and Reconciled=true.
func (g *gceMachineLifecycle) ConfirmCreation() error {
	reconciled := true
	timestamp := time.Now().UTC()
	confirmedUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			Reconciled:        &reconciled,
			CreationConfirmed: &timestamp,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &confirmedUpdate)
	return err
}

// AckDeletion sets DeletionAcknowledged in the API.
func (g *gceMachineLifecycle) AckDeletion() error {
	timestamp := time.Now().UTC()
	ackUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionAcknowledged: &timestamp,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// RefreshDeletionAck updates DeletionAcknowledged to prevent stale detection.
func (g *gceMachineLifecycle) RefreshDeletionAck() error {
	refreshTimestamp := time.Now().UTC()
	ackUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionAcknowledged: &refreshTimestamp,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &ackUpdate)
	return err
}

// ConfirmDeletion sets DeletionConfirmed in the API.
func (g *gceMachineLifecycle) ConfirmDeletion() error {
	timestamp := time.Now().UTC()
	confirmedUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionConfirmed: &timestamp,
		},
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &confirmedUpdate)
	return err
}

// SaveState persists intermediate Pulumi state to the API.
func (g *gceMachineLifecycle) SaveState(state *datatypes.JSON) error {
	stateUpdate := v0.GcpGceMachineRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: state,
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &stateUpdate)
	return err
}

// ClearInventory sets ResourceInventory to "{}" to signal destroy complete.
func (g *gceMachineLifecycle) ClearInventory() error {
	emptyInventory := datatypes.JSON([]byte("{}"))
	clearedUpdate := v0.GcpGceMachineRuntimeInstance{
		Common:            v0.Common{ID: &g.instanceID},
		ResourceInventory: &emptyInventory,
	}
	_, err := client.UpdateGcpGceMachineRuntimeInstance(g.r.APIClient, g.r.APIServer, &clearedUpdate)
	return err
}

// PublishCreateNotification publishes a NATS notification for creation.
func (g *gceMachineLifecycle) PublishCreateNotification() error {
	notifPayload, err := g.instance.NotificationPayload(
		notifications.NotificationOperationCreated,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = g.r.JetStreamContext.Publish(
		notif.GcpGceMachineRuntimeInstanceCreateSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish create notification: %w", err)
	}
	return nil
}

// PublishDeleteNotification publishes a NATS notification for deletion.
func (g *gceMachineLifecycle) PublishDeleteNotification() error {
	notifPayload, err := g.instance.NotificationPayload(
		notifications.NotificationOperationDeleted,
		false,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("failed to create notification payload: %w", err)
	}
	if _, err = g.r.JetStreamContext.Publish(
		notif.GcpGceMachineRuntimeInstanceDeleteSubject,
		*notifPayload,
	); err != nil {
		return fmt.Errorf("failed to publish delete notification: %w", err)
	}
	return nil
}

// buildGceMachineInfra constructs a GceMachineInfra from API objects.
func buildGceMachineInfra(
	r *controller.Reconciler,
	instance *v0.GcpGceMachineRuntimeInstance,
	log *logr.Logger,
) (*machine.GceMachineInfra, error) {
	if instance.Name == nil {
		return nil, fmt.Errorf("GCE instance missing required field Name")
	}
	if instance.GcpProviderID == nil {
		return nil, fmt.Errorf("GCE instance missing required field GcpProviderID")
	}

	// get GCP provider
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

	infraGce := machine.NewGceMachineInfra(*instance.Name)
	infraGce.Logger = log
	infraGce.ProjectID = *gcpProvider.ProjectID

	if instance.GcpGceMachineRuntimeDefinitionID == nil {
		return nil, fmt.Errorf("GCE instance missing required field GcpGceMachineRuntimeDefinitionID")
	}
	// get GCE machine runtime definition
	definition, err := client.GetGcpGceMachineRuntimeDefinitionByID(
		r.APIClient,
		r.APIServer,
		*instance.GcpGceMachineRuntimeDefinitionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve GCE machine runtime definition by ID: %w", err)
	}
	if definition.MachineType != nil {
		infraGce.MachineType = *definition.MachineType
	}
	if definition.ImageID != nil {
		infraGce.ImageID = *definition.ImageID
	}

	if instance.Region != nil {
		infraGce.Region = *instance.Region
	}
	if instance.Zone != nil {
		infraGce.Zone = *instance.Zone
	}
	if instance.NetworkID != nil {
		infraGce.NetworkID = *instance.NetworkID
	}
	if instance.SSHUser != nil {
		infraGce.SSHUser = *instance.SSHUser
	}
	if instance.SSHSourceRanges != nil {
		infraGce.SSHSourceRanges = append([]string(nil), *instance.SSHSourceRanges...)
	}

	if gcpProvider.ServiceAccountCredentials != nil && *gcpProvider.ServiceAccountCredentials != "" {
		// decrypt service account credentials
		decryptedCredentials, err := encryption.Decrypt(r.EncryptionKey, *gcpProvider.ServiceAccountCredentials)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt gcp provider service account credentials: %w", err)
		}
		infraGce.ServiceAccountCredentials = decryptedCredentials
	}

	if instance.SSHKey != nil && *instance.SSHKey != "" {
		// decrypt and seed persisted SSH key
		decryptedKey, err := encryption.Decrypt(r.EncryptionKey, *instance.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt persisted GCE SSH key: %w", err)
		}
		if err := infraGce.SeedSSHKeyPair(decryptedKey); err != nil {
			return nil, fmt.Errorf("failed to seed persisted GCE SSH key: %w", err)
		}
	}

	return infraGce, nil
}

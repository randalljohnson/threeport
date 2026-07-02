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

// defaultGceImageID is the boot image used when a GCE machine runtime
// definition leaves the image identifier unset.
const defaultGceImageID = "debian-cloud/debian-12"

// gceMachineLifecycle implements provider.InfraLifecycleProvider for GCP GCE
// machine runtime instances. It wires the reusable GCE VM provider into the
// shared create and delete state machines.
type gceMachineLifecycle struct {
	r          *controller.Reconciler
	instanceID uint
	instance   *v0.GcpGceMachineRuntimeInstance
	log        *logr.Logger
}

// compile-time assertion that the adapter implements all interface methods.
var _ provider.InfraLifecycleProvider = (*gceMachineLifecycle)(nil)

// StackKey returns the runtime-instance name so the shared state machine
// can serialize infra operations per stack. Two reconciles for the same
// instance name resolve to the same key and cannot spawn racing pulumi
// subprocesses against the same local state directory. A malformed instance
// with a nil Name returns an empty key and logs a warning so the caller
// requeues rather than panicking the reconciler worker.
func (g *gceMachineLifecycle) StackKey() string {
	if g.instance == nil || g.instance.Name == nil {
		if g.log != nil {
			g.log.Info("GCE machine runtime instance missing name; returning empty stack key")
		}
		return ""
	}
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

// IsCreateComplete checks whether resource inventory has been persisted.
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
	// trim whitespace before the literal compares so a padded or array-shaped
	// inventory does not read as complete, matching how the shared handler
	// treats a cleared inventory.
	inventory := strings.TrimSpace(string(*latest.ResourceInventory))
	switch inventory {
	case "", "{}", "[]", "null", `"null"`:
		return false, nil
	}
	return true, nil
}

// OnCreateConfirmed populates the married machine runtime instance once the VM
// is provisioned. By this point SaveCreateOutputs has persisted the external IP,
// SSH user, and SSH key onto the GCE instance, so this fetches that record,
// copies the connection fields onto the related machine runtime instance, and
// clears its Reconciled flag so the machine workload SSH install can proceed.
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

	machineRuntimeInstance, err := client.GetMachineRuntimeInstanceByID(
		g.r.APIClient,
		g.r.APIServer,
		*latest.MachineRuntimeInstanceID,
	)
	if err != nil {
		return fmt.Errorf("failed to get machine runtime instance: %w", err)
	}

	// the machine is reached at its external IP; the machine runtime instance
	// hostname carries that address so the workload SSH install can connect.
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

// SaveCreateOutputs persists the surfaced VM outputs and the final state. It
// diverges from the GKE adapter: it type-asserts the concrete GCE provider and
// writes its hostname, external IP, and generated SSH key onto the API object
// alongside the resource inventory.
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

// OnDeleteConfirmed validates against the compute API that the machine's VM and
// firewall are actually gone after destroy, and reclaims any the destroy left
// behind, so a checkpoint that drifted out of sync with the cloud cannot confirm
// a deletion that abandoned a live resource.
func (g *gceMachineLifecycle) OnDeleteConfirmed(infra provider.InfraProvider) error {
	gceInfra, ok := infra.(*machine.GceMachineInfra)
	if !ok {
		return fmt.Errorf(
			"failed to reclaim GCE orphans: expected *machine.GceMachineInfra, got %T",
			infra,
		)
	}
	cloud, err := newComputeOrphanReclaimCloud(gceInfra)
	if err != nil {
		return err
	}
	return reclaimOrphans(cloud)
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

// SetDeletionFailed marks DeletionFailed=true in the API.
func (g *gceMachineLifecycle) SetDeletionFailed() error {
	deletionFailed := true
	failedUpdate := v0.GcpGceMachineRuntimeInstance{
		Common: v0.Common{ID: &g.instanceID},
		Reconciliation: v0.Reconciliation{
			DeletionFailed: &deletionFailed,
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

// SaveState persists intermediate state to the API.
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

// buildGceMachineInfra constructs a *machine.GceMachineInfra from API objects.
// Every required pointer is nil-guarded before dereference so a malformed API
// object returns a descriptive error rather than panicking the create
// goroutine.
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

	// construct through the provider constructor so the embedded workspace and
	// runtime instance name are set; the provider validates the name first and
	// fails before any cloud call when it is empty.
	infraGce := machine.NewGceMachineInfra(*instance.Name)
	// route pulumi up, refresh, and destroy output through the structured
	// logger so the engine streams events instead of raw stdout text.
	infraGce.Logger = log
	infraGce.ProjectID = *gcpProvider.ProjectID

	// the machine type and image identifier live on the definition that
	// configures this instance; fetch it and copy the provisioning template
	// fields, nil-guarding both the foreign key and the fetched fields.
	if instance.GcpGceMachineRuntimeDefinitionID == nil {
		return nil, fmt.Errorf("GCE instance missing required field GcpGceMachineRuntimeDefinitionID")
	}
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
	// default the boot image when the definition leaves it unset so the
	// provider never fails on a missing image identifier
	if definition.ImageID != nil && *definition.ImageID != "" {
		infraGce.ImageID = *definition.ImageID
	} else {
		infraGce.ImageID = defaultGceImageID
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
	// SSHSourceRanges is a pointer-to-slice; copy the dereferenced slice only
	// when the pointer is non-nil to avoid a nil-pointer range.
	if instance.SSHSourceRanges != nil {
		infraGce.SSHSourceRanges = append([]string(nil), *instance.SSHSourceRanges...)
	}

	// require service account credentials on the gcp provider so a
	// misconfigured provider fails at buildinfra time rather than deferring the
	// failure to the adopt step, where an empty credential silently drops the
	// caller into the interactive oauth path and hangs for 5 minutes
	if gcpProvider.ServiceAccountCredentials == nil || *gcpProvider.ServiceAccountCredentials == "" {
		if gcpProvider.ID == nil {
			return nil, fmt.Errorf("gcp provider has no service account credentials")
		}
		return nil, fmt.Errorf("gcp provider %d has no service account credentials", *gcpProvider.ID)
	}
	decryptedCredentials, err := encryption.Decrypt(r.EncryptionKey, *gcpProvider.ServiceAccountCredentials)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt gcp provider service account credentials: %w", err)
	}
	infraGce.ServiceAccountCredentials = decryptedCredentials

	// rehydrate the persisted SSH key onto the rebuilt provider so a re-deploy
	// reuses it instead of minting a fresh pair and rotating the instance's
	// authorized key away from the key this control plane holds. The stored key
	// is encrypted at rest, so decrypt it first; it is unset on a clean first
	// create, in which case the provider generates a new pair.
	if instance.SSHKey != nil && *instance.SSHKey != "" {
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

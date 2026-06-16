package v0

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/threeport/threeport/pkg/encryption/v0"
	util "github.com/threeport/threeport/pkg/util/v0"
)

// setupGcpGceValidateDB returns an in-memory sqlite DB with the
// GcpGceMachineRuntimeInstance table migrated, plus the encryption key set in
// the env so the encrypt hooks can run on writes.
func setupGcpGceValidateDB(t *testing.T) *gorm.DB {
	t.Helper()
	key, err := encryption.GenerateKey()
	require.NoError(t, err)
	t.Setenv(encryption.KeyEnvVar, key)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&GcpGceMachineRuntimeInstance{},
		&AttachedObjectReference{},
	))
	return db
}

// createProvisionedGceInstance seeds a GCE machine runtime instance with the
// placement and association fields set and returns the row reloaded from DB,
// mirroring the PATCH handler's load-then-update flow.
func createProvisionedGceInstance(t *testing.T, db *gorm.DB, name string) GcpGceMachineRuntimeInstance {
	t.Helper()
	instance := &GcpGceMachineRuntimeInstance{
		Instance:                         Instance{Name: util.Ptr(name)},
		GcpProviderID:                    util.Ptr(uint(1)),
		GcpGceMachineRuntimeDefinitionID: util.Ptr(uint(2)),
		MachineRuntimeInstanceID:         util.Ptr(uint(3)),
		Region:                           util.Ptr("us-central1"),
		Zone:                             util.Ptr("us-central1-a"),
		NetworkID:                        util.Ptr("default"),
		SSHUser:                          util.Ptr("user"),
	}
	require.NoError(t, db.Create(instance).Error)

	var loaded GcpGceMachineRuntimeInstance
	require.NoError(t, db.First(&loaded, *instance.ID).Error)
	return loaded
}

// TestGcpGceMachineRuntimeInstance_BeforeUpdate_PlacementFieldsImmutable seeds a
// provisioned GCE instance and asserts every placement and association field is
// rejected on update through a live update tx. These fix where and from what the
// VM was provisioned, so changing them after creation would orphan resources or
// reparent the instance.
func TestGcpGceMachineRuntimeInstance_BeforeUpdate_PlacementFieldsImmutable(t *testing.T) {
	tests := []struct {
		name    string
		payload *GcpGceMachineRuntimeInstance
	}{
		{"region", &GcpGceMachineRuntimeInstance{Region: util.Ptr("other-region")}},
		{"zone", &GcpGceMachineRuntimeInstance{Zone: util.Ptr("other-zone")}},
		{"network id", &GcpGceMachineRuntimeInstance{NetworkID: util.Ptr("other-network")}},
		{"definition", &GcpGceMachineRuntimeInstance{GcpGceMachineRuntimeDefinitionID: util.Ptr(uint(99))}},
		{"provider", &GcpGceMachineRuntimeInstance{GcpProviderID: util.Ptr(uint(99))}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupGcpGceValidateDB(t)
			loaded := createProvisionedGceInstance(t, db, fmt.Sprintf("gce-immutable-%d", i))

			err := db.Model(&loaded).Updates(tt.payload).Error
			require.Error(t, err, "changing %s must be rejected", tt.name)
			assert.Contains(t, err.Error(), tt.name+" cannot be changed after creation")
		})
	}
}

// TestGcpGceMachineRuntimeInstance_BeforeUpdate_AllowsInPlaceMutableFields seeds
// a provisioned GCE instance and asserts the ssh source ranges and ssh user are
// accepted on update, since a pulumi up applies them in place to the firewall
// and instance metadata.
func TestGcpGceMachineRuntimeInstance_BeforeUpdate_AllowsInPlaceMutableFields(t *testing.T) {
	tests := []struct {
		name    string
		payload *GcpGceMachineRuntimeInstance
	}{
		{"ssh user", &GcpGceMachineRuntimeInstance{SSHUser: util.Ptr("newuser")}},
		{"ssh source ranges", &GcpGceMachineRuntimeInstance{SSHSourceRanges: &[]string{"10.0.0.0/8"}}},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupGcpGceValidateDB(t)
			loaded := createProvisionedGceInstance(t, db, fmt.Sprintf("gce-mutable-%d", i))

			err := db.Model(&loaded).Updates(tt.payload).Error
			require.NoError(t, err, "changing %s must be accepted", tt.name)
		})
	}
}

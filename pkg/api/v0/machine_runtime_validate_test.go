package v0

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	util "github.com/threeport/threeport/pkg/util/v0"
)

// TestMachineRuntimeDefinition_hooks_ReturnNil covers all six no-op GORM
// hooks on MachineRuntimeDefinition, asserting each returns nil regardless
// of the tx argument.
func TestMachineRuntimeDefinition_hooks_ReturnNil(t *testing.T) {
	// exercise every no-op hook on a zero-value receiver
	m := &MachineRuntimeDefinition{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"beforeCreate", func() error { return m.beforeCreate(nil) }},
		{"beforeUpdate", func() error { return m.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return m.beforeDelete(nil) }},
		{"afterCreate", func() error { return m.afterCreate(nil) }},
		{"afterUpdate", func() error { return m.afterUpdate(nil) }},
		{"afterDelete", func() error { return m.afterDelete(nil) }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// each hook should return nil unconditionally
			assert.NoError(t, tc.fn())
		})
	}
}

// TestMachineRuntimeInstance_beforeCreate covers the credential-required
// branch: at least one of SSHKey or SSHPassword must be set, otherwise the
// hook returns a bad-request error naming the instance.
func TestMachineRuntimeInstance_beforeCreate(t *testing.T) {
	tests := []struct {
		name    string
		key     *string
		pw      *string
		wantErr bool
	}{
		{"both nil rejects", nil, nil, true},
		{"only key accepts", util.Ptr("ssh-key"), nil, false},
		{"only password accepts", nil, util.Ptr("pw"), false},
		{"both set accepts", util.Ptr("ssh-key"), util.Ptr("pw"), false},
		{"empty strings accept", util.Ptr(""), util.Ptr(""), false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// build an instance with a name so the error format has something to interpolate
			m := &MachineRuntimeInstance{
				Instance: Instance{Name: util.Ptr("test-mri")},
				SSHKey:   tc.key,
				SSHPassword: tc.pw,
			}

			// invoke the hook directly; nil tx is fine because the body never dereferences it
			err := m.beforeCreate(nil)

			// verify accept vs reject matches the credential presence expectation
			if tc.wantErr {
				require.Error(t, err)
				assert.True(
					t,
					strings.Contains(err.Error(), "must have at least one of SSHKey or SSHPassword"),
					"error should name the missing credential requirement, got: %v", err,
				)
				assert.Contains(t, err.Error(), "test-mri", "error should name the instance")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestMachineRuntimeInstance_hooks_ReturnNil covers the no-op hooks on
// MachineRuntimeInstance (beforeUpdate, beforeDelete, afterCreate,
// afterUpdate, afterDelete), asserting each returns nil.
func TestMachineRuntimeInstance_hooks_ReturnNil(t *testing.T) {
	// zero-value receiver is sufficient for no-op hooks
	m := &MachineRuntimeInstance{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"beforeUpdate", func() error { return m.beforeUpdate(nil) }},
		{"beforeDelete", func() error { return m.beforeDelete(nil) }},
		{"afterCreate", func() error { return m.afterCreate(nil) }},
		{"afterUpdate", func() error { return m.afterUpdate(nil) }},
		{"afterDelete", func() error { return m.afterDelete(nil) }},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// each hook should return nil unconditionally
			assert.NoError(t, tc.fn())
		})
	}
}

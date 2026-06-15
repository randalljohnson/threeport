package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestThreeportServiceAccountRoles_AssertsInstanceAdminRole asserts the GCP
// service account role list grants compute.instanceAdmin.v1, the least
// privilege role the machine provider needs to manage Compute Engine
// instances.
func TestThreeportServiceAccountRoles_AssertsInstanceAdminRole(t *testing.T) {
	// confirm the role list grants the compute-instance-admin role so the
	// machine provider can create and manage compute engine instances
	assert.Contains(t, threeportServiceAccountRoles, "roles/compute.instanceAdmin.v1")
}

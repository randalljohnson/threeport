package v0

import (
	"fmt"

	util "github.com/threeport/threeport/pkg/util/v0"
	"gorm.io/gorm"
)

// BeforeDelete prevents deletion of an OCI provider that has active runtime instances.
func (o *OciProvider) BeforeDelete(tx *gorm.DB) error {
	// check for active OKE runtime instances using this provider
	var instances []OciOkeKubernetesRuntimeInstance
	if result := tx.Where(
		"oci_provider_id = ?", *o.ID,
	).Find(&instances); result.Error != nil {
		return fmt.Errorf("failed to check for OCI OKE runtime instances: %w", result.Error)
	}

	if len(instances) > 0 {
		return &util.HttpError{
			StatusCode: 409,
			Message: fmt.Sprintf(
				"oci provider has %d active OKE runtime instance(s) - cannot be deleted",
				len(instances),
			),
		}
	}

	return nil
}

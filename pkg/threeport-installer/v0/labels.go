package v0

// Labels stamped on resources the installer creates. They drive
// label-scoped operations like the dev reinstall sweep.
const (
	// LabelManagedBy marks every resource the installer creates so
	// tooling can list the full set of installer-managed objects with
	// a single selector.
	LabelManagedBy      = "app.kubernetes.io/managed-by"
	LabelManagedByValue = "threeport-installer"

	// LabelPersistent opts a managed resource out of destructive
	// sweeps. Applied to stateful objects whose loss would break the
	// control plane (cockroachdb data, certificate authority, external
	// load balancer ip) and that an in-place spec update can't fix.
	LabelPersistent      = "threeport.io/persistent"
	LabelPersistentValue = "true"
)

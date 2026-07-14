package v0

// IngressRule describes a single firewall ingress rule in a portable way.
// Universal across cloud providers (mapping to AWS security-group rules,
// GCP firewall rules with priority+direction, Azure NSG rules, OCI security
// list ingress). Concrete provider layers translate to their native shape.
//
// Design notes:
//   - Protocol is a string to keep IANA names ("tcp", "udp", "icmp") and
//     bare protocol numbers ("112" for VRRP) both expressible.
//   - Ports is a slice of strings to allow both bare ports ("80") and
//     ranges ("8000-9000"). Empty or nil ports means "all ports for this
//     protocol", which is the correct interpretation for protocols without
//     ports (icmp, esp, vrrp).
//   - SourceRanges is a slice of CIDR strings. Empty or nil means "any
//     source" (0.0.0.0/0 semantics).
//   - Description is optional; providers that support labels/notes on
//     rules (GCP firewall description, AWS SG rule description) use it.
//
// Reserve for later without breaking wire compat: Direction (defaults to
// ingress today; egress rules would be a separate slice), Priority (GCP
// numeric priority; AWS uses ordering; leave to provider defaults for
// now).
type IngressRule struct {
	// Protocol is the L4 protocol name ("tcp", "udp", "icmp") or bare
	// protocol number ("112" for VRRP, "50" for ESP).
	Protocol *string `json:",omitempty" validate:"required"`

	// Ports are the destination ports the rule allows. Empty means
	// "all ports for this protocol". Supports bare ports and ranges
	// (e.g. "80", "8000-9000").
	Ports *[]string `json:",omitempty" validate:"optional"`

	// SourceRanges are the source CIDR blocks the rule allows. Empty
	// means any source.
	SourceRanges *[]string `json:",omitempty" validate:"optional"`

	// Description is a human-readable note attached to the rule where the
	// provider supports it.
	Description *string `json:",omitempty" validate:"optional"`
}

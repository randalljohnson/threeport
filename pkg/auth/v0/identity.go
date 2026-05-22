package v0

// OUControlPlane identifies threeport control-plane components in client
// certs.
const OUControlPlane = "controlplane"

// OUDatabase identifies certs issued for the threeport database.
const OUDatabase = "database"

// OrgCore identifies core threeport in client certs; modules use their
// own ApiNamespace. Matches lib.CoreApiNamespace.
const OrgCore = "threeport.io"

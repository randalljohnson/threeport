# Gateway Manifests

Threeport renders these manifests for a gateway and for the support-services
operator components a runtime installs.

## Upstream Copies

`vendir sync` writes `upstream` from `vendir.yml`.  The files are exact
copies.  Do not edit them.

`platform_v1alpha1_ingresscomponent.yaml` is an IngressComponent and
installs nginx.  `getExternalDnsYaml()` uses the same file for a domain
name and sets `externalDNS`.

`platform_v1alpha1_certificatescomponent.yaml` is a CertificatesComponent
and installs cert-manager.  `confirmGatewayControllerDeployed()` includes
it for EKS and kind.

`cafe-ingress.yaml` is the nginx Ingress sample.  Go sets the host, paths,
service and ingress class, and drops the sample annotation.

`transport-server-tcp.yaml` is the nginx TransportServer sample.  Go sets
the listener, upstream and ingress class `nginx`.

`global-configuration.yaml` is the nginx GlobalConfiguration sample.
`confirmGatewayPortsExposed()` keeps one for a Kubernetes runtime and
rewrites its listeners from the TCP ports of every gateway on the runtime.
Go names it `nginx-ingress` and sets its namespace.

## Templates

`templates` holds custom resources Threeport writes.  The `apiVersion` is
the CRD version the installed controller serves.

`issuer.yaml` is an Issuer and `certificate.yaml` is a Certificate.  Both
are included when any port on the definition has TLS enabled.
`createGatewayDefinitionYamlDocument()` renders them for one gateway
definition.

## Test Fixtures

`service-test.yaml` is a Service.  The gateway instance tests use it as
the workload a gateway routes to.

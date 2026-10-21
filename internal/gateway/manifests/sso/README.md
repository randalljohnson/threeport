# Support-services Operator Requests

Threeport applies these custom resources once for a Kubernetes runtime.
The operator creates the controllers from them.  Vendir writes the files
under `upstream`.

`platform_v1alpha1_ingresscomponent.yaml` is an IngressComponent and
installs nginx.  `getExternalDnsYaml()` uses the same file for a domain
name and sets `externalDNS`.

`platform_v1alpha1_certificatescomponent.yaml` is a CertificatesComponent
and installs cert-manager.
`confirmGatewayControllerDeployed()` includes it for EKS and kind.

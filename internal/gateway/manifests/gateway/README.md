# Gateway Definition Manifests

`createGatewayDefinitionYamlDocument()` renders these manifests for one
gateway definition.

`cafe-ingress.yaml` is the nginx Ingress sample under `upstream`.  Go sets
the host, paths, service and ingress class, and drops the sample annotation.

`transport-server-tcp.yaml` is the nginx TransportServer sample.  Go sets
the listener, upstream and ingress class `nginx`.

`issuer.yaml` is an Issuer and `certificate.yaml` is a Certificate.  Both
are included when any port on the definition has TLS enabled.

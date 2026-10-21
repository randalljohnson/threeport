package manifests

import _ "embed"

// boilerplate: vendir sync writes the yaml this file embeds. Do not edit it.

//go:embed upstream/sso/platform_v1alpha1_ingresscomponent.yaml
var IngressComponent []byte

//go:embed upstream/sso/platform_v1alpha1_certificatescomponent.yaml
var Certificates []byte

//go:embed upstream/nginx/tcp/global-configuration.yaml
var GlobalConfiguration []byte

//go:embed upstream/nginx/ingress/cafe-ingress.yaml
var Ingress []byte

//go:embed upstream/nginx/tcp/transport-server-tcp.yaml
var TransportServer []byte

//go:embed gateway/issuer.yaml
var Issuer []byte

//go:embed gateway/certificate.yaml
var Certificate []byte

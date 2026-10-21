# Runtime TCP Manifests

`confirmGatewayPortsExposed()` keeps one GlobalConfiguration for a
Kubernetes runtime.  It rewrites its listeners from the TCP ports of every
gateway on the runtime.

`global-configuration.yaml` under `upstream` is the nginx sample for that
object.  Go names it `nginx-ingress` and sets its namespace and listeners.

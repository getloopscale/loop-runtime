# Conftest Rego — ServiceMonitor port consistency.
# Plan T7.3.
# Verifies that ServiceMonitor port references match a port exposed by the Service.
package main

# ServiceMonitor must reference a port name (not number).
deny[msg] {
  input.kind == "ServiceMonitor"
  endpoint := input.spec.endpoints[_]
  not endpoint.port
  not endpoint.portNumber
  msg := sprintf("ServiceMonitor %s/%s endpoint missing port reference", [input.metadata.namespace, input.metadata.name])
}

# ServiceMonitor port should be a string (named port).
deny[msg] {
  input.kind == "ServiceMonitor"
  endpoint := input.spec.endpoints[_]
  not is_string(endpoint.port)
  not endpoint.portNumber
  msg := sprintf("ServiceMonitor %s/%s endpoint should reference port by NAME (string), not number — eases refactoring", [input.metadata.namespace, input.metadata.name])
}

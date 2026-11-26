# Conftest Rego — security context enforcement for LoopRuntime charts.
# Plan T7.3 — charts-sota-implementation-plan Phase 7.
# Verifies that all Deployments use loopruntime-common.containerSecurityContext defaults.
package main

# Deployments must run as non-root.
deny[msg] {
  input.kind == "Deployment"
  c := input.spec.template.spec.containers[_]
  not c.securityContext.runAsNonRoot
  msg := sprintf("Container %s in %s/%s does not set securityContext.runAsNonRoot=true", [c.name, input.metadata.namespace, input.metadata.name])
}

# Deployments must drop ALL capabilities.
deny[msg] {
  input.kind == "Deployment"
  c := input.spec.template.spec.containers[_]
  not c.securityContext.capabilities.drop
  msg := sprintf("Container %s in %s/%s does not drop capabilities (loopruntime-common.containerSecurityContext)", [c.name, input.metadata.namespace, input.metadata.name])
}

deny[msg] {
  input.kind == "Deployment"
  c := input.spec.template.spec.containers[_]
  drops := c.securityContext.capabilities.drop
  not array_contains(drops, "ALL")
  msg := sprintf("Container %s in %s/%s must drop capabilities ALL", [c.name, input.metadata.namespace, input.metadata.name])
}

# Pod must use seccompProfile RuntimeDefault.
deny[msg] {
  input.kind == "Deployment"
  not input.spec.template.spec.securityContext.seccompProfile.type
  msg := sprintf("Pod %s/%s must set securityContext.seccompProfile.type", [input.metadata.namespace, input.metadata.name])
}

deny[msg] {
  input.kind == "Deployment"
  profile := input.spec.template.spec.securityContext.seccompProfile.type
  profile != "RuntimeDefault"
  msg := sprintf("Pod %s/%s seccompProfile.type must be RuntimeDefault, got %s", [input.metadata.namespace, input.metadata.name, profile])
}

# Helper.
array_contains(arr, item) {
  arr[_] == item
}

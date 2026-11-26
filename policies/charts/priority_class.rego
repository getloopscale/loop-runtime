# Conftest Rego — PriorityClass enforcement for platform-critical components.
# Plan T7.3.
# Platform components (operators, API) MUST set priorityClassName for proper
# scheduling. User-facing components (dashboard) MAY skip.
package main

platform_critical_components := {
  "loopruntime-api",
  "loopruntime-tenant-operator",
  "loopruntime-app-operator",
  "loopruntime-preview-operator",
  "loopruntime-platform-operator",
}

deny[msg] {
  input.kind == "Deployment"
  name := input.metadata.name
  is_platform_critical(name)
  not input.spec.template.spec.priorityClassName
  msg := sprintf("Platform-critical Deployment %s must set priorityClassName (e.g., system-cluster-critical)", [name])
}

is_platform_critical(name) {
  platform_critical_components[name]
}

is_platform_critical(name) {
  some component
  platform_critical_components[component]
  endswith(name, component)
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package k8s

// ControlPlaneNamespace is the one namespace every runtime.getloopscale.com CR lives in.
//
// It is an ADDRESS, not a policy. Both k8s/tenancy and k8s/workload write CRs
// there, so it belongs to the plumbing they share rather than to either of them —
// before the B-058 split it was `tenantNamespace`, an unexported constant of the
// tenancy files that the workload files reached into.
//
// ADR-0018 C1 fixes it at compile time. It used to be a var read from
// TENANT_NAMESPACE at package init, and the misplacement bug that produced came
// from a second reader disagreeing with the first. Its guards are
// k8s/tenancy/tenant_namespace_const_test.go and
// infra/tests/config/regression_adr_test.go.
const ControlPlaneNamespace = "loop-runtime-system"

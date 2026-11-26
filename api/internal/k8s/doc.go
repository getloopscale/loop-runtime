// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package k8s is Kubernetes client PLUMBING for the API: scheme registration,
// rest.Config resolution, typed client construction, and the control-plane
// namespace those three address. Nothing else.
//
// # The admission criterion, and what it refuses
//
// Membership here is not "it uses client-go". That criterion cannot say no, and
// B-058 measured what it had let in: on 2026-09-04 this package held 7 non-test
// files and 1520 non-test lines serving three unrelated responsibilities — the
// Tenant CR and tenant eviction, the LoopRuntimeApplication and PreviewEnvironment CRs,
// and secret sync. They shared a client library, not a subject.
//
// The criterion is: PLUMBING NAMES NO DOMAIN. A file here may not carry a
// responsibility in its file name, nor export an identifier that carries one.
// Whoever cannot state their code in terms of scheme, config, client and namespace
// belongs in one of the homes below:
//
//   - k8s/tenancy    — the Tenant CR and tenant eviction
//   - k8s/workload   — the LoopRuntimeApplication and PreviewEnvironment CRs
//   - k8s/secretsync — K8s Secrets on mgmt plus ExternalSecret CRs on runtime
//
// That sentence is what the old criterion could not say. "It uses client-go" admits
// tenant_cr_manager.go, application_cr_manager.go and secret_k8s_manager.go alike;
// "plumbing names no domain" refuses all three, and refuses the next one too.
//
// The gate `loop-runtime-ops quality package-admission` enforces both halves — no domain
// vocabulary up here, and every home still populated, so refusing into nowhere does
// not read as compliance. See docs/architecture.md.
package k8s

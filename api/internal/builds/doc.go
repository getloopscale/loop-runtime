// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package builds owns the build lifecycle: submitting a build
// (BuildOrchestrator), driving it to a terminal state from the workflow's
// status (BuildReconcileLoop), timing out builds stuck in a phase
// (BuildReaper) and deleting expired build records (BuildGC).
//
// Those parts stay in one package on purpose. Every path that ends a build —
// the orchestrator's completion, the reconcile loop's retry exhaustion, the
// reaper's timeout — goes through the same finalization kernel
// (buildFinalizer: state machine transition under the environment lock,
// SSE event, outbox deploy.failed). Splitting them into packages would mean
// exporting that kernel for three callers, which is the one thing it exists
// to keep in a single place.
//
// What changes for other reasons lives elsewhere: step reporting in
// builds/stepwatch, and the Kubernetes and SQL adapters in
// builds/buildsadapter and api/internal/db/store.
package builds

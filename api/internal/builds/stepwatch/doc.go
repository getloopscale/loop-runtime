// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package stepwatch turns Argo Workflow node phases into the per-step build
// events the CLI streams (build.step.*), and classifies a failed step into
// the error class and suggestion shown to the user.
//
// It is separate from package build because it changes for a different
// reason: build owns the build lifecycle (submit, reconcile, reap, finalize),
// while this package owns how a running build's steps are reported. The
// Kubernetes side — polling Workflows from the build cluster — is
// adapters.K8sWorkflowNodeSource, which feeds ArgoNodeEvent values in through
// the WorkflowNodeSource port.
package stepwatch

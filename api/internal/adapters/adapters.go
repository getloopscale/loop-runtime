// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package adapters provides bridge types between infrastructure and domain interfaces.
//
// All adapters follow DIP: they translate between layers without leaking
// infrastructure details into business logic.
//
// T6.3 of deep-review-remediation-plan (closes find_arch_004): the original
// 449-LOC adapters.go was split into purpose-scoped files for SRP:
//
//   - pgx_adapter.go         — pgx.Tx ↔ db.DBTX bridges; PoolEnvironmentLocker.
//   - build_adapter.go       — BuildOrchestratorAdapter (route ↔ service translation).
//   - build_store_adapter.go — db.BuildStore reshaped for sse/routes consumers.
//   - release_adapter.go     — StatusReleaseStoreAdapter.
//   - tenant_adapter.go      — TenantDBDeleterAdapter, TenantK8sEvictorAdapter.
//
// NOTE: All Fake* types have been REMOVED from production code.
// Real implementations are required:
//   - WorkflowSubmitter → api/internal/builds/argo/workflow_submitter.go
//   - GitOpsPusher      → api/internal/gitops/gitea_pusher.go
//   - SyncClient        → api/internal/release/argocd/sync_client.go
//   - SourceStore       → api/internal/source/gitea_store.go
package adapters

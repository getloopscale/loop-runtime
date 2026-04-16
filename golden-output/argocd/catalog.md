# ArgoCD Golden Test Catalog

Source project: `referencias/argo-cd`
Target: LoopZero `api/internal/services.SyncCallbackActivator`
Extraction date: 2026-03-29

## Source Tests Extracted (13 total)

| ID | File | Function | Domain | Type |
|----|------|----------|--------|------|
| src_sync_callbacks_test_is_app_sync_status_refreshed | notification_controller/controller/controller_test.go | TestIsAppSyncStatusRefreshed | sync-callbacks | unit |
| src_application_health_test_set_application_health | controller/health_test.go | TestSetApplicationHealth | application-health | unit |
| src_application_health_test_all_missing_resources | controller/health_test.go | TestSetApplicationHealth_AllMissingResources | application-health | unit |
| src_application_health_test_no_resources | controller/health_test.go | TestSetApplicationHealth_NoResource | application-health | unit |
| src_sync_phases_test_sync_phase_none | gitops-engine/pkg/sync/sync_phase_test.go | TestSyncPhaseNone (+ variants) | sync-phases | unit |
| src_sync_phases_test_operation_succeeded | controller/appcontroller_test.go | TestProcessRequestedAppOperation_Successful | sync-phases | unit |
| src_error_recovery_test_operation_failed_no_retries | controller/appcontroller_test.go | TestProcessRequestedAppOperation_FailedNoRetries | error-recovery | unit |
| src_error_recovery_test_operation_failed_with_retries | controller/appcontroller_test.go | TestProcessRequestedAppOperation_FailedHasRetries | error-recovery | unit |
| src_error_recovery_test_sync_timeout | controller/appcontroller_test.go | TestProcessRequestedAppOperation_SyncTimeout | error-recovery | unit |
| src_application_health_test_health_improves | controller/health_test.go | TestSetApplicationHealth_HealthImproves | application-health | unit |
| src_sync_callbacks_notification_list_services | test/e2e/notification_test.go | TestNotificationsListServices | sync-callbacks | e2e |
| src_sync_callbacks_notification_settings_vars | util/notification/settings/settings_test.go | TestInitGetVars | sync-callbacks | unit |
| src_application_health_test_child_app_degraded | controller/health_test.go | TestChildAppHealth | application-health | unit |
| src_sync_callbacks_notification_controller_init | notification_controller/controller/controller_test.go | TestInit | sync-callbacks | unit |
| src_error_recovery_test_sync_timeout_controller_cache | notification_controller/controller/controller_test.go | TestInitTimeout | error-recovery | unit |

## Contracts Defined (4 total)

| ID | Domain | Type | Summary |
|----|--------|------|---------|
| contract_argocd_sync_callback_payload | sync-callbacks | interface | Webhook payload format: app_name, sync_status, health_status, revision |
| contract_argocd_app_name_format | sync-callbacks | naming | {tenantID}-{environment}, split on last hyphen |
| contract_argocd_health_statuses | application-health | enum | Healthy/Progressing/Degraded/Missing/Unknown/Suspended semantics |
| contract_argocd_sync_completion_idempotency | sync-callbacks | behavior | Callback endpoint safe to call multiple times |

## Golden Tests Written (12 total)

All golden tests are in:
`golden-output/argocd/golden_suite/portable_tests/sync_callback_activator_golden_test.go`

| ID | Test Function | Priority | Behavioral Contract |
|----|---------------|----------|---------------------|
| golden_parse_argo_app_name_valid | TestGolden_ParseArgoAppName_ValidFormats | high | Last-hyphen split, tenant may contain hyphens |
| golden_parse_argo_app_name_invalid | TestGolden_ParseArgoAppName_InvalidFormats | high | Empty/no-hyphen/leading-hyphen return ok=false |
| golden_on_sync_completed_activates_pending | TestGolden_OnSyncCompleted_ActivatesPendingRelease | critical | Synced+Healthy callback activates PENDING release |
| golden_on_sync_completed_revision_mismatch | TestGolden_OnSyncCompleted_RevisionMismatch_ReleasSkipped | high | Wrong revision → release stays PENDING |
| golden_on_sync_completed_empty_revision | TestGolden_OnSyncCompleted_EmptyRevision_ActivatesWithoutRevisionCheck | medium | Empty revision bypasses revision check |
| golden_on_sync_completed_wrong_tenant | TestGolden_OnSyncCompleted_WrongTenant_ReleasIgnored | critical | Cross-tenant isolation preserved |
| golden_on_sync_completed_wrong_env | TestGolden_OnSyncCompleted_WrongEnvironment_ReleaseIgnored | high | Cross-environment isolation preserved |
| golden_on_sync_completed_no_pending | TestGolden_OnSyncCompleted_NoPendingReleases_IsNoOp | high | No pending releases → no error, no side effects |
| golden_on_sync_completed_invalid_app_name | TestGolden_OnSyncCompleted_InvalidAppName_ReturnsError | high | Invalid app name → error returned |
| golden_on_sync_completed_multiple_releases | TestGolden_OnSyncCompleted_MultiplePendingReleases_AllActivated | medium | Multiple projects in same env all activated |
| golden_on_sync_completed_release_no_commit | TestGolden_OnSyncCompleted_ReleaseHasNoGitCommit_ActivatesAnyRevision | medium | Release with no GitCommit activates regardless |
| golden_on_sync_completed_idempotent | TestGolden_OnSyncCompleted_CalledTwice_SecondCallIsNoOp | high | Double-callback is safe no-op |

## Key Behavioral Findings from ArgoCD Source

### Health Status Semantics (for LoopZero's convergence decision)

From `controller/health_test.go`:
- **Healthy**: All non-hook resources are Healthy. An app with zero resources is also Healthy.
- **Degraded**: At least one non-hook resource reports Degraded (e.g., failed Job, crashing Pod).
- **Missing**: All expected resources are declared but none exist in the cluster.
- **Hook exclusion**: Resources annotated with `argocd.argoproj.io/hook` (PreSync, Sync, PostSync) are excluded from health aggregation. A failed PreSync hook job does NOT degrade the app.
- **Child app propagation**: A Degraded child Application propagates to the parent. A Missing or Unknown child does NOT.

### Sync Status vs Health Status

These are two independent dimensions:
- `sync_status=Synced` means git desired state matches cluster actual state.
- `health_status=Healthy` means all resources are running correctly.
- An app can be Synced but Degraded (resources applied but failing).
- LoopZero MUST require BOTH Synced AND Healthy before activating a release.

### Notification Delivery Contract

From `notification_controller/controller/controller_test.go` and `test/e2e/notification_test.go`:
- Notifications fire when the sync status is refreshed (finishedAt < reconciledAt).
- A notification can fire for intermediate states (Progressing, OutOfSync).
- The LoopZero webhook handler correctly ignores non-completion states via `if payload.SyncStatus != "Synced" || payload.HealthStatus != "Healthy"` guard.
- ArgoCD may retry notification delivery — the handler MUST be idempotent.

### Sync Timeout Behavior

From `controller/appcontroller_test.go#TestProcessRequestedAppOperation_SyncTimeout`:
- ArgoCD terminates stuck operations with phase=Failed and message containing "Operation terminated, triggered by controller sync timeout".
- LoopZero's ReleaseConvergenceLoop fallback handles this case by timing out PENDING releases that exceed `SyncTimeout`.

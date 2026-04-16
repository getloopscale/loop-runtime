# Golden Suite Manifest: SyncCallbackActivator

## Source

- **System Under Test:** `api/internal/services/sync_callback_activator.go`
- **Golden Test File:** `golden-output/argocd/golden_suite/portable_tests/sync_callback_golden_test.go`
- **Package:** `unit` (compiles when copied to `api/tests/unit/`)

## ArgoCD Reference Patterns

| ArgoCD Source | Pattern Extracted | Applied In |
|---|---|---|
| `notification_controller/controller/controller_test.go` | Table-driven tests with `name`/`expectedValue` pairs; `assert`/`require` from testify; namespace filtering (`checkAppNotInAdditionalNamespaces`) | `TestParseArgoAppName_LastHyphenSplit`, `TestParseArgoAppName_EdgeCases`, `TestOnSyncCompleted_WrongTenant` |
| `controller/sync_test.go` | Fake controllers with injected dependencies (`newFakeController`); state transition verification after sync; revision history validation | `syncCallbackFixture` struct; `TestOnSyncCompleted_HappyPath` (verify PENDING -> ACTIVE); `TestOnSyncCompleted_RevisionMismatch` |
| `test/e2e/notification_test.go` | Given/When/Then fluent style; operations on empty state produce no errors; webhook configuration verification | `TestOnSyncCompleted_NoPendingReleases_NoError` (idempotent on empty state) |

## Behavioral Contracts Tested

| # | Test Function | Contract | Verdict |
|---|---|---|---|
| 1 | `TestParseArgoAppName_LastHyphenSplit` | Last-hyphen split: `"tenant-abc-production"` -> `("tenant-abc", "production", true)` | PASS |
| 2 | `TestParseArgoAppName_EdgeCases` | Empty, no hyphen, leading/trailing hyphen, single hyphen, only hyphens -> `false` | PASS |
| 3 | `TestOnSyncCompleted_HappyPath_ActivatesPendingRelease` | Synced+healthy callback activates PENDING release for matching tenant+env; emits SSE event; updates env target | PASS |
| 4 | `TestOnSyncCompleted_WrongTenant_ReleaseNotActivated` | Release with different tenant is NOT activated; no side effects | PASS |
| 5 | `TestOnSyncCompleted_RevisionMismatch_ReleaseSkipped` | Release with wrong GitCommit stays PENDING; logged as warning | PASS |
| 6 | `TestOnSyncCompleted_NoPendingReleases_NoError` | No PENDING releases -> no error, no side effects (idempotent) | PASS |
| 7 | `TestOnSyncCompleted_InvalidAppName_ReturnsError` | Invalid app names return error containing "invalid app name" | PASS |
| 8 | `TestOnSyncCompleted_Idempotent_SecondCallIsNoOp` | Second call after activation is a no-op (release already ACTIVE, not returned by FindPendingReleases) | PASS |

## How to Run

```bash
# Copy to api/tests/unit/ and run:
cp golden-output/argocd/golden_suite/portable_tests/sync_callback_golden_test.go api/tests/unit/
cd api && go test -v -run "TestParseArgoAppName|TestOnSyncCompleted" ./tests/unit/
```

## Fake Dependencies

| Fake | Real Interface | Behavior |
|---|---|---|
| `fakeSyncReleaseStore` | `services.ReleaseConvergenceStore` | In-memory; tracks statuses; filters out non-PENDING on subsequent calls |
| `fakeSyncEnvTargetStore` | `db.EnvironmentTargetStore` | Counts `SetActive` calls; no-op for other methods |
| `fakeSyncEventStore` | `Append(ctx, buildID, eventType, data)` | Records event types in slice |
| `fakeSyncLocker` | `services.EnvironmentLocker` | Executes callback immediately with nil tx (no real lock) |

## State Machine Transitions Verified

- `PENDING` -> `ACTIVE` via `SyncCompleted` event (happy path, tests 3 and 8)
- `PENDING` stays `PENDING` when tenant/env mismatch (test 4) or revision mismatch (test 5)
- Already `ACTIVE` release is not returned by `FindPendingReleases` (idempotency, test 8)

## Validated On

- Date: 2026-03-29
- Go: 1.25.0
- All 8 tests (22 subtests) PASS

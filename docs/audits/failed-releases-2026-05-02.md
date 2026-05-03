# Failed Releases Audit — 2026-05-02

> fix-all-platform-issues-plan T0.3 — root-cause triage of failed/stuck release records.

## State at audit time

| Status | Count | Most recent | Most recent project |
|--------|------:|-------------|---------------------|
| CANCELLED | 70 | 2026-04-30 23:19 | solo-server |
| GIT_MIRROR_FAILED | 8 | 2026-04-29 06:02 | repotest |
| SYNC_FAILED | 3 | 2026-04-29 14:16 | dogfood-iter9 |
| SUPERSEDED | 3 | 2026-04-29 16:18 | dogfood-fb |
| **ACTIVE** | **0** | — | — |

## Classification

### CANCELLED (70) — NOT a bug

70 CANCELLED records is high but reflects deliberate operator cancellation
during dev iterations (each `loopzero deploy` aborted by user OR superseded by
newer deploy → status=CANCELLED). These are NORMAL operational artifacts.
**Action:** archive via T2.3 migration 000008. Do NOT investigate further.

### GIT_MIRROR_FAILED (8) — REAL BUG, single project

All 8 GIT_MIRROR_FAILED records are for project `repotest`, all timestamped
2026-04-29 between 05:42 and 06:02. Hypothesis: a stuck push to the gitops
mirror repo for repotest, retried 8 times, all failing. Either:
- Gitea credentials expired during that window
- Concurrent push race (non-fast-forward rejection)
- repotest-gitops repo had inconsistent state

**Sample release IDs for log investigation (T4.1):**
- rel-c8baf8f27108309d (2026-04-29 06:02:25)
- rel-6ab2908f702a7f9f (2026-04-29 05:53:47)
- rel-057b456a2958c4df (2026-04-29 05:42:15)

**Action:** T4.1 — pull operator + API logs around these timestamps,
identify error signature, implement targeted fix.

### SYNC_FAILED (3) — DIFFERENT BUG

3 SYNC_FAILED across different projects (dogfood-iter9, dogfood-iter8, repotest)
on 2026-04-29. `git_mirror_status='pending'` for these — meaning the SYNC step
failed BEFORE git mirror was attempted. Hypothesis: ArgoCD sync API call
failed (auth or network), distinct from T4.1 git push failure.

**Sample release IDs for log investigation (T4.2):**
- rel-59e746fb206ae746 (dogfood-iter9, 2026-04-29 14:16)
- rel-7c20968843afde96 (dogfood-iter8-1777467514, 2026-04-29 12:59)
- rel-4ca17acf1770d188 (repotest, 2026-04-29 06:10)

**Action:** T4.2 — pull logs, identify ArgoCD sync API error signature,
implement targeted fix.

## Cross-references

- T2.3 migration 000008 — archive CANCELLED + failed records older than 7 days
- T4.1 — RCA + fix git mirror push failure (8 records)
- T4.2 — RCA + fix ArgoCD sync API failure (3 records)
- T4.3 — E2E deploy validation (must produce 1 ACTIVE release)

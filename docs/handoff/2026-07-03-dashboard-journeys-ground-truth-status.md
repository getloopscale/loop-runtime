# Dashboard Journeys — Ground-Truth Status (2026-07-03)

Corrects the stale `2026-06-05-dashboard-vercel-like-integration-roadmap.md` progress
table with **grep-verified** current state (Unbreakable Rule 12: grep, not memory) and
records the 2026-07-03 delivery session. The month-old roadmap listed several journeys
as "pending" that are in fact DONE — trusting it would have re-implemented shipped work.

## Session deliveries — 9 PRs, 6 journeys

| Journey | PR(s) | Notes |
|---|---|---|
| J-06 Overview | loopzero#168 (merged) · loopzero-cloud#141 | Engine `GET /api/v1/builds/stats` deploys aggregate + migration 000049; BFF webhook `plan_price` + KPI strip. T2 MEET-2026-07-03-003. |
| J-07 D4 bulk delete | loopzero-cloud#140 (merged) | Multi-select + type-to-confirm + best-effort partial-failure. #57 (search aria-label) also confirmed green (committed patch-package patch was un-applied locally). |
| J-08 Deploy CTA | loopzero-cloud#142 | `PageShell.primaryAction` "Deploy" on the project Overview, reuses RedeployDialog. Review: READY_TO_MERGE. |
| J-14 Export CSV | loopzero-cloud#143 | Audit log RFC-4180 CSV export (`lib/audit-csv.ts`). |
| J-13 Billing invoices | loopzero-cloud#144 | Owner-only `GET /v1/billing/invoices`. T2 MEET-2026-07-03-004 caught 2 real defects pre-code: `invoice_pdf` PII capability-URL (security veto → dropped) + empty-AccountID IDOR (fail-closed guard). |
| **J-18 Status metrics (complete)** | loopzero#169 · loopzero#171 · loopzero-cloud#145 | Concrete `PrometheusQueryClient` (was `nil` → 503) + error_rate unit fix (loopzero#170: was `rate(5xx)` req/s, now ratio ×100 %) + KPI-strip frontend wiring. Lights up when `LOOPZERO_PROMETHEUS_ADDRESS` is set. |

Filed + fixed during the session: **loopzero#170** (J-18 error_rate unit mismatch) → loopzero#171.

## Ground-truth journey status (grep-verified)

| Journey | Roadmap said | Reality (code) |
|---|---|---|
| J-06 Overview | 🔴 não iniciado | ✅ delivered (PRs above) |
| J-07 Projects | 🟡 wizard pendente | ✅ DoD met + bulk delete; wizard is aspirational (needs OAuth-redirect wizard-state DISCOVER) |
| J-08 Project Detail | 🟡 | ✅ Deploy CTA added; "View all deployments" already existed |
| J-11 Domains | 🟡 polish P1 | ✅ **DONE** (DoD met) |
| J-13 Billing | 🟡 LIVE básico | ✅ invoices delivered |
| J-14 Audit | 🟡 export pendente | ✅ CSV export delivered |
| J-17 Deployments | 🟡 ações pendentes | ✅ **DONE** — cancel/rollback/redeploy/promote buttons, filter chips, timeline, sub-tabs all shipped |
| J-18 Status | 🟡 métricas live | ✅ delivered (backend + fix + frontend); needs LIVE Prometheus to display |
| J-19 Runtime Logs | 🟡 falta download | ✅ **DONE** — `lib/log-download.ts` + Download button in `log-viewer-toolbar.tsx` |
| J-20 Scale | 🟡 cost preview | ✅ **DONE** (DoD met) |
| J-21 Readiness | 🟡 UI checklist | ✅ **DONE** (DoD met) |
| J-22 Secrets | 🟡 bulk import | ✅ **DONE** (DoD met) |
| J-23 Previews | 🟡 | 🟡 PARTIAL — active list works; closed-preview history needs a backend query; route redirects to /environments (single-slot model) |

## Genuinely-remaining backlog (all substantial backend T2 journeys)

| Journey | Gap | Reusable pattern / blocker |
|---|---|---|
| **J-16 Settings** | webhooks CRUD + General (workspace name) + API-logs — 4/5 sub-sections are `SubPagePlaceholder` | **Strong reusable pattern:** the memory-webhooks system (`internal/routes/memory_webhooks.go` + `memory_webhook_deliveries.go` + `internal/memory_webhook_worker` + `dashboard/src/pages/memory/webhooks.tsx`) is a complete webhook CRUD + delivery worker + delivery-log reference to mirror for workspace/deploy webhooks. Needs a deploy-event trigger + T2. |
| **J-12 Team** | pending-invites list | NOT a contained query — the invite flow involves Kratos identity-create-on-first-login + Resend email + an acceptance state. Backend + infra (Kratos/Resend). |
| J-18 (display) | KPI values only render with a reachable Prometheus | Operator/infra: set `LOOPZERO_PROMETHEUS_ADDRESS` (+ merge loopzero#169/#171/#145). |
| J-24 Configuration | loopzero.yaml editor + commit via App token | 🔴 NOT built — backend GET/PATCH config + GitHub-App commit (T2 GitOps write) + new dashboard route. |
| J-15 Profile | display-name edit | Deferred — Kratos self-service flow. |

## Recommended next steps

1. **Merge the 7 open PRs** (order: loopzero#171 → loopzero#169; then loopzero-cloud #141/#142/#143/#144/#145). All conflict-free (the 4 loopzero-cloud CHANGELOG conflicts from #140's merge were resolved 2026-07-03).
2. **Next P0 journey:** J-16 Settings webhooks — highest value, and de-risked by the memory-webhooks reference implementation. Run the full cycle with a T2 governance meeting (billing/webhooks + external delivery = T2), as done for J-13. Alternatively J-12 Team invites (smaller UI, but needs Kratos/Resend infra).
3. **J-18 display:** provision a reachable Prometheus (`LOOPZERO_PROMETHEUS_ADDRESS`) to light up the Status KPIs + histograms.

## Provenance

Ground-truth established via `grep`/`Read` code-walk (Rule 12), three parallel journey-audit agents, and per-journey verification. The 2026-06-05 roadmap's progress table remains the strategic-intent record; this doc is the delivered-reality snapshot as of 2026-07-03.

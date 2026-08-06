---
type: baseline
title: "Steady-State Baseline — M7 QA-readiness complete (TEMPLATE)"
---

# Steady-State Baseline — M7 QA-readiness complete (TEMPLATE)

> **TEMPLATE — do not commit filled.** Operator copies this file to `docs/operations/baselines/{date}-qa-readiness-complete.md` post-Step 6 of M7 runbook + fills the `<TODO operator>` markers from LIVE cluster state.
>
> Per `.claude/rules/steady-state-baseline.md § When to capture`: M7 is a **Pre-validation + Pre-release** trigger; this baseline anchors the regression-detection between this canonical state and any future drift.

## Metadata

- **Captured at:** `<TODO operator: date -Iseconds>`
- **Captured by:** `<TODO operator: name>`
- **Milestone:** M7 of `qa-readiness-roadmap-2026-06-23`
- **Plan:** `.claude/knowledge-base/plans/m7-cleanroom-validation-readiness-plan.md`
- **ADR:** `.claude/knowledge-base/decisions/2026-06-23-validation-milestone-sponsor-hard-stop.md`
- **Runbook:** [`docs/operations/m7-cleanroom-validation-runbook.md`](../m7-cleanroom-validation-runbook.md)

## Prior baseline cross-references (predecessors)

- M6 closure baseline: [`docs/operations/baselines/2026-06-22-pre-cleanroom-qa-dogfood-cleanup.md`](./2026-06-22-pre-cleanroom-qa-dogfood-cleanup.md) (closest prior — qa-dogfood-cleanup-2026-06-21 baseline)
- M3 baseline anchor: [`docs/operations/baselines/2026-06-20-pre-cleanroom-t3-1.md`](./2026-06-20-pre-cleanroom-t3-1.md)
- M2 baseline anchor: [`docs/operations/baselines/2026-06-18-pre-cleanroom-post-promote-p0.md`](./2026-06-18-pre-cleanroom-post-promote-p0.md)
- Phase 4-5 baseline: [`docs/operations/baselines/2026-06-15-cleanroom-phase4-5-green-100pct-coverage-roadmap.md`](./2026-06-15-cleanroom-phase4-5-green-100pct-coverage-roadmap.md)
- Index: `docs/operations/baselines/index.md`

## Layer 1 — Code state

```bash
git rev-parse HEAD             # <TODO operator: paste output>
git branch --show-current      # <TODO operator: paste output — expected develop>
git log --oneline origin/develop..HEAD | head  # <TODO operator: paste output — expected empty>
git status --short             # <TODO operator: paste output — expected empty>
git describe --tags --abbrev=0 # <TODO operator: paste output — expected vX.Y.Z>
```

## Layer 2 — Runtime state (Helm + K8s)

```bash
helm list -A -o json | jq -r '.[] | "\(.namespace)/\(.name) chart=\(.chart) revision=\(.revision) status=\(.status)"'
# <TODO operator: paste output — expected ~30+ releases all status=deployed>

kubectl get pods -A -o jsonpath='{range .items[*]}{.metadata.namespace}/{.metadata.name} {.spec.containers[*].image}{"\n"}{end}' | head -40
# <TODO operator: paste first 40 lines>

kubectl get tenants,environments,loopzeroapplications,exposedroutes,previewenvironments -A -o yaml > /tmp/tenant-crs-baseline.yaml
# <TODO operator: count rows; expected canonical fixture state>

kubectl get applications.argoproj.io -A
# <TODO operator: paste output — expected canonical app set Synced+Healthy>

kubectl get clustercredentials -A
# <TODO operator: paste output — expected 3+ cells connected>
```

## Layer 3 — Config state

```bash
kubectl get configmap -n loopzero-system ory-kratos-config -o jsonpath='{.metadata.resourceVersion}'
# <TODO operator: paste resourceVersion>

kubectl get configmap -n loopzero-system ory-hydra-config -o jsonpath='{.metadata.resourceVersion}'
# <TODO operator: paste resourceVersion>

kubectl get secret -n loopzero-system loopzero-service-auth -o jsonpath='{.metadata.resourceVersion}'
# <TODO operator: paste resourceVersion (do NOT paste values)>

kubectl exec -n loopzero-system postgresql-0 -- psql -U postgres -d loopzero -c "SELECT version, dirty FROM schema_migrations"
# <TODO operator: paste output — expected highest version, dirty=false>

kubectl get configmap -n loopzero-system loopzero-platform-topology -o jsonpath='{.data}' | jq .
# <TODO operator: paste output — expected canonical wiring>
```

## Layer 4 — LIVE functional probes (the SLO floor)

The probes that ARE the regression detectors. Each is one line: command + expected result.

### M7 DoD probes (canonical)

- [ ] `Step 0 — go test ./charts/ -run TestM7Preflight -count=1` → exit 0; 7/7 subtests PASS (HARD STOP #1).
- [ ] `Step 1 — task setup` → exit 0 after ~25min; 4 clusters Running.
- [ ] `Step 2 — task doctor` → `PASS=<N>` with N≥38 + `FAIL=0` (HARD STOP #2).
- [ ] `Step 3 — bash seed-post-cleanroom-all.sh --verify` → exit 0; 12 steps all PASS (HARD STOP #3).
- [ ] `Step 4 — bash dogfood-all.sh --live-only --json` → 13/13 wrappers PASS (HARD STOP #4 partial).
- [ ] `Step 5 — bash live-proof-dev-public.sh` → 9/9 checks PASS (HARD STOP #4 partial).
- [ ] `Step 6 — bash run-chaos.sh × 2` → 2/2 scenarios complete lifecycle (HARD STOP #4 partial).
- [ ] `Step 7 — Sponsor JUNTOS browser walkthrough` → sponsor verbatim "Funcionou!" (HARD STOP #4 sponsor).

### Engine probes (canonical surfaces)

- `GET https://app-dev.loopzero.example/api/v1/health/deep` → HTTP 200; `<TODO operator: paste latency p50>`.
- `GET https://app-dev.loopzero.example/api/v1/projects` (with JWT) → HTTP 200; `<TODO operator: paste row count>`.
- `GET https://cloud-dev.loopzero.example/api/v1/version` → HTTP 200 with build SHA matching Layer 1 git rev-parse.
- `POST https://cloud-dev.loopzero.example/api/v1/webhooks/github` (with HMAC) → HTTP 204; `loopzero_github_webhook_deliveries_total{status="accepted"}` increments by 1.
- `GET https://auth-dev.loopzero.example/self-service/login/api` → HTTP 200; flow object present.

### Dashboard probes (J-XX cross-cutting)

- `https://app-dev.loopzero.example/login` → renders without OIDC redirect loop.
- `https://app-dev.loopzero.example/projects` → renders ≥ 1 project row.
- `https://app-dev.loopzero.example/deployments` → renders SSE stream; no buffer-until-EOF behavior.
- `https://app-dev.loopzero.example/audit` → renders entries; WHEN + ACTOR columns populated; NO `INSERT tenant_members` SQL fragment exposed.

### Observability probes

- `kubectl exec -n monitoring loki-0 -- wget -q -O- 'http://localhost:3100/loki/api/v1/labels'` → returns canonical label set (`namespace, pod, container, app, level`).
- `curl http://prometheus.monitoring.svc.cluster.local:9090/api/v1/query?query=up` → `data.result[]` non-empty.

## Sponsor verbatim quote (post Step 7)

```
<TODO operator: paste exact sponsor approval verbatim — e.g., "Funcionou!" or equivalent green-light phrase>
```

## Anomalies / known gaps (carried forward)

`<TODO operator: list any non-blocker drift observed; cross-link to GH followup issues>`

## Cross-references

- Mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
- Steady-state rule: `.claude/rules/steady-state-baseline.md`
- Runbook: [`docs/operations/m7-cleanroom-validation-runbook.md`](../m7-cleanroom-validation-runbook.md)
- Run-file: `.claude/knowledge-base/roadmap-runs/M7-2026-06-23.md`
- Index: `docs/operations/baselines/index.md` (append a row pointing at this file post-fill)

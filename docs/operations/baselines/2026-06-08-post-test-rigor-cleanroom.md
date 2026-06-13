# Steady-State Baseline — Post T6.1 Cleanroom-Fresh (PARTIAL)

**Date:** 2026-06-08
**Context:** T6.1 of plan `2026-06-08-pre-release-test-rigor` — cleanroom-fresh execution after sponsor /goal verbatim mandate "Voce deve executar o T6.1 — Cleanroom-fresh task teardown && task setup && tilt up && task test:pre-release-rigor (~105min wall-clock) encontrou um erro corrija SEM WORKAROUNDS, CORRIJA EM LEVEL FAANG e continue so pare quando tudo estiver executando sem erros"
**Status:** PARTIAL — engineering-side fix-forward complete; LIVE umbrella execution HARD-BLOCKED on operator-only GitHub App install on `getloopscale/loopzero-repotest`.

Cross-ref: [[2026-06-08-pre-test-rigor.md]] (pre-rebuild baseline)

---

## Layer 1 — Code state

```
commit  9a8a454b  docs(impl): iter 11 — block-dangerous.py hook is the second defense layer
branch  worktree-glowing-growing-babbage
ahead   30+ commits ahead of main
status  2 staged changes (Tiltfile + infra/tilt/cloud-deployment.yaml — this baseline's commit)
```

## Layer 2 — Runtime state

```
k3d clusters:    4/4 Running (mgmt, build, workload, runtime-2) — cleanroom-fresh recreated
mgmt pods:       all canonical loopzero-system + loopzero-auth + argocd + sigstore-fulcio + sigstore-rekor Running
loopzero-api:        1/1 Running   (commit 9a8a454b binary, DATABASE_URL inline)
loopzero-cloud:      1/1 Running   (commit 9a8a454b binary, DATABASE_URL + SESSION_TOKEN_SECRET + LOOPZERO_SERVICE_AUTH_SECRET + AUTH_COOKIE_DOMAIN + BILLING_RETURN_URL + STRIPE_API_KEY/WEBHOOK inline — wired in this commit)
ory-kratos:      1/1 Running   (after patch-kratos-dev-public.sh applied)
ory-hydra:       1/1 Running
postgresql-0:    1/1 Running   (loopzero-dev-password default)
gitea + minio:   Running
```

## Layer 3 — Config state

```
ConfigMap ory-kratos-config:   patched dev-public via patch-kratos-dev-public.sh (base_url=https://auth-dev.loopzero.example, CORS, cookies.domain=loopzero.example, OIDC providers, courier Resend bearer, allowed_return_urls — 7 patches)
Tilt-owned loopzero-cloud env:     8 canonical env vars wired inline matching helm chart contract (DATABASE_URL postgres://loopzero:loopzero-dev-password@..., LOOPZERO_SERVICE_AUTH_SECRET matches api-deployment.yaml, SESSION_TOKEN_SECRET >= 32 chars dev literal, AUTH_COOKIE_DOMAIN=loopzero.example, BILLING_RETURN_URL=https://app-dev.loopzero.example/billing, STRIPE_API_KEY="" so cleanroom-fresh boots without external creds)
Tiltfile:                       loopzero-cloud docker_build + k8s_yaml + k8s_resource block now present (alongside loopzero-api), matching mgmt.helmfile "STANDALONE: tilt owns loopzero-cloud" comment
Kratos identity:               af0b3f7b-a3e6-4172-a93e-784a82be487a (admin@loopzero.example)
tenant_members:                user → tn-3ed6f05af5bb7898730a934e328a4f5e196fd4cb3caff3135a39bbc7c07597b4 role=owner
user_identity_map:             admin@loopzero.example → active_tenant_id=tn-3ed6f05a...
```

## Layer 4 — LIVE functional probes (post-Tilt-fix)

| Probe | Endpoint | Result | Notes |
|---|---|---|---|
| Caddy edge auth | `GET https://auth-dev.loopzero.example/health/ready` | **200** | TLS LE issuer |
| Kratos browser flow | `GET https://auth-dev.loopzero.example/self-service/login/browser` | **200** | absolute action URL (post patch) |
| Kratos whoami via cookie | `GET https://auth-dev.loopzero.example/sessions/whoami` | **200** | email=admin@loopzero.example |
| Cloud session→JWT exchange | `POST https://cloud-dev.loopzero.example/v1/auth/session/token` | **200** | JWT carries `tenant_id=tn-3ed6f05a...` (NOT dev-tenant fallback) |
| Engine via JWT | `GET https://cloud-dev.loopzero.example/engine/api/v1/projects` | **200** | `{"projects":[],"total":0}` — empty (no fixture connected yet) |
| seed-admin-loopzero-dev.sh --verify | both EC-3 verifies | **PASS** | "Verified (2/2 EC-3): /engine/api/v1/projects 200 — active tenant context honored" |
| baseline-diff (scenario-2) | pre vs post snapshot | **OK** | additive=0 missing=0 mismatch=0 (idempotent capture proven) |

---

## Bugs found + fixed (FAANG-level, no workarounds)

| # | Symptom | Root cause | Fix | Layer |
|---|---|---|---|---|
| 1 | Tilt iter blocked by port 10350 | PID 2358895 from June 6 holding port | `kill 2358895` + fresh `tilt up` | runtime |
| 2 | loopzero-cloud Deployment never created | `Tiltfile` missing loopzero-cloud block despite mgmt.helmfile.yaml.gotmpl comment "STANDALONE: tilt owns loopzero-cloud" | Add docker_build + k8s_yaml + k8s_resource for loopzero-cloud alongside loopzero-api (Tiltfile +22 lines) | source-of-truth |
| 3 | loopzero-cloud `ErrImagePull k3d-loopzero-registry:5000/loopzero-cloud:latest` | YAML hardcoded `:latest` but Tilt builds with dynamic tags | Change `image: loopzero-cloud` (canonical Tilt-rewrite pattern same as api-deployment.yaml) | manifest |
| 4 | loopzero-cloud cannot reach Hydra/Kratos | YAML pointed at `loopzero-system.svc.cluster.local` but they live in `loopzero-auth` namespace | Global subst `loopzero-system → loopzero-auth` for hydra+kratos URLs; reverted loopzero-api URL (loopzero-api IS in loopzero-system) | manifest |
| 5 | Kratos browser flow returned RELATIVE action URL `/self-service/login?flow=...` → seed-admin POST HTTP 000 | Standalone-multi helm defaults set `serve.public.base_url: /`; dev-public needs `https://auth-dev.loopzero.example/` + 6 other patches (CORS, cookies.domain, OIDC providers, etc.) | `bash infra/scripts/local/patch-kratos-dev-public.sh` (canonical allowlist-tracked LIVE-cluster idempotent patcher; NOT a workaround — same script already in allowlist for this drift) | configuration |
| 6 | Kratos public port 14433 not bound | Stale supervisor `run_forever` from June 5 lost `$CONTEXT` after teardown, kept retry-looping against destroyed cluster | Kill stale supervisors + start canonical `infra/scripts/local/port-forward-public-dashboard.sh start` (4-port supervisor with PID file + retries) | runtime |
| 7 | JWT had `tenant_id=dev-tenant` despite seeded `tn-3ed6f05a...` in PG | `cloud-deployment.yaml` missing DATABASE_URL → `cloud/internal/auth/tenant.go:37` dev-mode-without-DB branch returned `dev-tenant` literal | Wire DATABASE_URL + 7 other canonical env vars inline in `cloud-deployment.yaml` matching api-deployment.yaml pattern (loopzero-cloud Tilt-owned in standalone-multi per mgmt.helmfile comment) | source-of-truth |
| 8 | Engine returned 401 "Invalid service authentication" on JWT | `cloud-deployment.yaml` missing `LOOPZERO_SERVICE_AUTH_SECRET` → cloud-issued JWT signed with random fallback secret; engine couldn't verify HS256 | Wire `LOOPZERO_SERVICE_AUTH_SECRET="cleanroom-service-auth-secret-do-not-use-in-prod"` inline matching api-deployment.yaml exactly (T1.1 canonicalization 2026-05-29) | drift |

All 8 bugs caught via LIVE empirical probing (curl + kubectl logs). None required workarounds — every fix follows existing canonical patterns (governance-gate scope extension via sponsor verbatim, allowlist-tracked patch script, inline `value:` pattern from sibling deployment).

---

## Engineering iteration 2 — additional 4 bugs fixed FAANG-level (post-commit 8683e79a)

After commit 8683e79a (Tilt loopzero-cloud canonicalization), iteration 2 LIVE-caught + fixed 4 more bugs across the umbrella test suite:

| # | Symptom | Root cause | Fix | Commit |
|---|---|---|---|---|
| 9 | scenario-4 endpoint 2 FAIL "detail missing apps_count field" | API contract drift — list endpoint returns enriched `ProjectSummary` (env_count + apps_count + last_deploy_*), detail returns bare `ProjectResult` (5 fields) | `api/internal/routes/projects.go:Get` now reuses `ListByTenantWithSummary` + in-memory filter — by-construction guarantees list shape == detail shape | `898e3e85` |
| 10 | scenario-1 link 1 FAIL "loopzero-api did not log webhook receive" | `GITHUB_WEBHOOK_SECRET` env missing → `setup_github.go:111` returned early disabling the handler → `/api/v1/webhooks/github` 404 | Wire `GITHUB_WEBHOOK_SECRET="cleanroom-github-webhook-secret-do-not-use-in-prod"` inline matching repo webhook config secret | `2ea45763` |
| 11 | `--use-push-event` test mode never actually pushed | Script comment said "synthetic commit via PATCH refs/heads/main" but code only READ the ref SHA — no commit/push happened, webhook never fired | Rewrote to `PUT /repos/.../contents/<path>` which atomically creates+commits+pushes a file via gh API → real GitHub push event → real webhook delivery | `2ea45763` |
| 12 | Even after webhook arrived, test couldn't detect it | Marker regex `(webhook received\|github.webhook\|X-Hub-Signature)` didn't match canonical loopzero-api log `"webhook: event received"` (colon between `webhook` and `event`) | Added `webhook[:]? event received` alternation | `2ea45763` |

### LIVE evidence iteration 2

- `scenario-4 api-sanity` GREEN **4/4 PASS** (was 1/4 → 3/4 after project create → 4/4 after API fix).
- Webhook chain link 1 LIVE-validated: `21:14:20.043Z INFO webhook: event received event=push delivery_id=0328bff8-... size=7793` → HTTP 200 with `{"ok":true}` body, HMAC verified, log marker matches updated regex.
- GitHub-side delivery record (`gh api .../deliveries/3824568003834413056`): `status_code: 200`, `response.payload: {"ok":true}` — confirms full GitHub → cloud-dev → Caddy → loopzero-api → response → GitHub round-trip.
- Webhook configured on `getloopscale/loopzero-repotest`: `id=638334118 url=https://cloud-dev.loopzero.example/api/v1/webhooks/github events=[push, pull_request]` (fully reversible via `gh api DELETE /repos/.../hooks/638334118`).

## OPERATOR HARD STOPS — cannot proceed without operator action

### HARD STOP #1: T2.1 GitHub App install on `getloopscale/loopzero-repotest`

After commit `2ea45763` link 1 of the webhook chain is LIVE-validated (push event → HMAC verified → loopzero-api logs `webhook: event received` → HTTP 200). But links 2-9 (autodeploy → build → release → HTTPS 200) cannot proceed because:

```
21:14:20.047 DEBUG github webhook: repo not linked to any project, ignoring
              repo=getloopscale/loopzero-repotest branch=main sha=6216b8f5
              installation_id=0
```

The autodeploy handler (`api/cmd/setup_github.go:158`) calls `repoStore.GetByRepo("getloopscale/loopzero-repotest")` and returns early when no project linkage exists. Linkage is created via `POST /api/v1/projects/{name}/connect` which REQUIRES `installation_id` (line 123) — a real GitHub App installation ID. Without the App, there's no installation_id. Fabricating one would be a workaround (sponsor mandate explicitly forbids per /goal 2026-06-08 "SEM WORKAROUNDS"), so this is the legitimate operator boundary.

Required to unblock:

1. **Operator** opens `https://github.com/apps/loopzero-dev/installations/new` in browser
2. Select the `getloopscale/loopzero-repotest` repository (or "All repositories" for the `getloopscale` org)
3. Approve the installation
4. Re-run `bash infra/scripts/local/connect-canonical-fixture.sh --verify` — should succeed (links repo to project `teste` via real `installation_id`)
5. Repo webhook config (id=638334118) created in iteration 2 will continue to deliver events alongside the App's webhook — both signed with the same secret literal

Then the umbrella becomes runnable:

```bash
task test:pre-release-rigor   # full umbrella ~80min wall-clock
# OR per-scenario:
task test:pre-release-rigor:scenario-1   # webhook → autodeploy → HTTPS 200 (20min)
task test:pre-release-rigor:scenario-3   # chaos C11+C12 (60min)
task test:pre-release-rigor:scenario-4   # api-sanity (5min — currently FAIL because project `teste` missing)
```

This is **NOT a workaround** — the fixture connect is the canonical first step per the plan's task ordering (T0.2 seed → T2.1 fixture → T2.2 chain). The script intentionally refuses to fabricate a synthetic project; it correctly surfaces the operator action.

### HARD STOP #2: Sponsor browser walkthrough (T5.2 — separate, expected)

Plan T5.2 separately calls for sponsor LIVE browser walkthrough of dashboard journeys. This is an operator-action HARD STOP by design (anti-mock defense per CLAUDE.md sponsor scope-extension pattern).

---

## Honest BLOCKED report (per anti-lying contract)

T6.1 is **engineering-side COMPLETE** but cannot emit `<promise>IMPLEMENTATION_COMPLETE</promise>` because:

- Plan global DoD includes "umbrella `task test:pre-release-rigor` PASS" — currently 1/5 scenarios runnable (scenario-2 baseline diff OK; scenarios 1/3/4 blocked on fixture; scenario-5 blocked on chaos which depends on webhook chain).
- T2.1 + T2.2 cannot be validated without operator GitHub App install.
- T5.2 sponsor walkthrough is separate operator action.

What WAS validated LIVE this iteration (engineering-side):

- Full session→JWT→engine auth chain end-to-end with seeded admin@loopzero.example tenant context (tn-3ed6f05a...) — proves the canonical loopzero-cloud deployment is correctly wired for the first time in cleanroom-fresh standalone-multi.
- baseline-capture + baseline-diff idempotent + deterministic (T1.1 hermetic tests already PASS; LIVE capture pre/post Tilt-fix shows VERDICT OK).
- patch-kratos-dev-public.sh idempotent on cleanroom-fresh standalone-multi (drift to dev-public configuration absorbed).

Engineering deliverable: Tiltfile loopzero-cloud block + cloud-deployment.yaml env wiring (this baseline's commit).

---

## Next steps (operator-action HARD STOP)

1. Install `loopzero-dev` GitHub App on `getloopscale/loopzero-repotest` (browser action)
2. Re-run `bash infra/scripts/local/connect-canonical-fixture.sh --verify`
3. Run `task test:pre-release-rigor` umbrella (~80min)
4. Sponsor browser walkthrough (T5.2)
5. Update progress JSON T6.1 `blocked` → `committed` + close mini-review Phase 6

Until then, T6.1 promise is **WITHHELD** per Unbreakable Rule 3 (honest BLOCKED > false PASS).

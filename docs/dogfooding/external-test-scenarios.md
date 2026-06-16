# External Dogfooding — Test Scenarios

> **Audience:** External validation team (engineers / SREs / product testers who have **never** seen this codebase)
> **Goal:** Independently verify LoopZero (CLI-first managed cloud, Kubernetes-backed) does what it promises end-to-end
> **Snapshot:** 2026-06-16, HEAD `b03ea24b`
> **Estimated total time:** 6–10 hours for full coverage; 30 min for Smoke Test

---

## 0. How To Use This Document

Each scenario follows the same shape:

```
### S-{NN} — {short name}
**Journey:** J-{NN}  (links to internal feature ID, ignore for validation)
**Pre-conditions:** what must be true before starting
**Steps:** numbered actions you perform
**Expected:** what should happen at each step
**Acceptance criteria:** observable PASS/FAIL conditions
**Corner cases:** explicit edge conditions to try
**Common failure modes:** anti-patterns the team has historically hit — bias your testing here
```

**Reporting:** copy [§ 12 Bug Report Template](#12-bug-report-template) into a new issue for every FAIL or anomaly. Do NOT rely on email/chat — written, structured reports only.

**Verdict for full session:** apply [§ 13 Verdict Matrix](#13-verdict-matrix) at end.

---

## 1. Environment Setup (one-time, ~15 min)

### Canonical test credentials (SEMPRE use estas)

For every scenario that needs login, use the **same canonical user** to keep audit trails consistent and avoid creating disposable accounts that pollute dev-public env:

| Field | Value |
|---|---|
| **Email** | `admin@loopzero.example` |
| **Password** | `loopzero-dev-password` |
| **State** | active (LIVE-validated 2026-06-16 via Kratos public API) |

> **Why a single canonical user:** mirrors the production "production canary" pattern — one well-known account whose behavior we recognize at a glance in audit logs / traces / Stripe. Multi-account testing is reserved for **CC-1 Multi-tenant isolation** (§ 10) where the *second* account is created intentionally for the isolation test, then deleted.

> **If the canonical user does not exist or login fails** (e.g., after a Cleanroom reset): the seed script `bash infra/scripts/local/seed-admin-loopzero-dev.sh` creates the identity but does NOT attach a password (Kratos canonical drops plaintext `config.password` silently). Operator must follow up with the canonical PUT-with-bcrypt step documented in `feedback_post_cleanroom_seed_admin_canonical` + `project_canonical_dev_user_credentials` memory entries.

> **NEVER hardcode these in source files.** They live in this dogfooding doc + `~/.loopzero/dev-credentials.json` (mode 0600) only. CI / production scripts read from the JSON file.

### S-01 — Account creation (only for SM-1 first run OR after seed-admin reset)

**Pre-conditions:** Browser (Chrome / Firefox / Safari latest). No prior LoopZero account.

> **Note:** if the seeded `paulo@loopzero.test` account already exists (most common state), **skip S-01 entirely** and go straight to **SM-1**.

**Steps:**
1. Open `https://app-dev.loopzero.example/register`
2. Choose **either** email/password **or** OAuth (Google / GitHub)
3. Complete verification (email link if email/password path)

**Acceptance:**
- Account created successfully
- Logged into dashboard at `/projects` (empty state OK)
- Browser console (F12) shows **0 errors** (warnings OK)

**Corner cases:**
- (a) Register with same email twice → expect clear "already exists" error, not a stack trace
- (b) Cancel OAuth flow mid-callback → expect redirect to `/login` with friendly notice
- (c) Email verification link clicked twice → second click should be idempotent (no error, lands at dashboard)

**Common failure modes:**
- CSRF redirect loop on `/login` (`feedback_post_cleanroom_kratos_patch_dev_public`)
- Verification page shows blank PinInput without auto-sending the code (`feedback_j05_aprendizado_orphan_handler_accept_route_not_wired`)
- OAuth `redirect_uri` mismatch → 403 / blank page

---

### S-02 — CLI install + login

**Pre-conditions:** Node.js 20+ installed; account from S-01.

**Steps:**
1. `npm install -g @loopzero/cli` (or follow install docs)
2. `loopzero login` → opens browser to OAuth/device-code flow
3. Approve in browser
4. Return to terminal → `loopzero whoami`

**Acceptance:**
- `loopzero whoami` prints `{email, tenant, plan}` correctly
- Exit code **0**
- `~/.loopzero/` config file created with 0600 mode
- `loopzero --version` matches latest published version

**Corner cases:**
- (a) Run `loopzero login` while already logged in → idempotent, does not corrupt session
- (b) Kill `loopzero login` mid-flow (Ctrl+C) → next `loopzero login` recovers cleanly
- (c) `loopzero logout` → `loopzero whoami` returns exit code **4** (auth error)

**Common failure modes:**
- Device flow verification_uri points to cluster-internal DNS instead of public (`feedback_j05_aprendizado_hydra_urls_canonical_via_helmfile_overlay`)
- CLI silent error swallow on bad LOOPZERO_URL (`feedback_j05_aprendizado_cli_loopzero_url_subshell_env_propagation`)

---

### S-03 — First project init

**Pre-conditions:** S-02 done. A monorepo on disk with at least one app to deploy (any language: Node, Python, Go, Rust).

**Steps:**
1. `cd /your/monorepo`
2. `loopzero init`
3. Answer interactive prompts (framework auto-detect, env name, etc.)
4. Inspect generated `loopzero.yaml`

**Acceptance:**
- `loopzero.yaml` created at repo root
- Framework correctly auto-detected (verify against actual `package.json` / `pyproject.toml` / `go.mod` / `Cargo.toml`)
- File is valid YAML (parse it manually)
- `loopzero init` exit code **0**

**Corner cases:**
- (a) Run `loopzero init` twice → second run should refuse or offer "overwrite?" prompt
- (b) Empty repo (no manifest files) → expect clear error explaining what's needed
- (c) Multi-language monorepo (Node + Python in subdirs) → expect both detected

---

## 2. Smoke Test (~30 min) — Pre-flight before deep dives

If any of these 5 scenarios fails, **stop and report immediately**. The system is broken; deeper testing is not informative.

### SM-1 — Dashboard reachable + auth works

**Steps:**
1. Open `https://app-dev.loopzero.example/`
2. Login

**Acceptance:** dashboard renders, no console errors, redirect after login goes to `/projects`.

### SM-2 — CLI can deploy a hello-world

**Steps:**
1. `loopzero init` in a hello-world repo (any language)
2. `loopzero deploy --env production`
3. Wait until command exits

**Acceptance:** exit code **0**, terminal prints a `live URL`, `curl <live-url>` returns HTTP **2xx**.

### SM-3 — Live URL is reachable + TLS valid

**Steps:**
1. `curl -v <live-url>` from the URL printed in SM-2
2. `openssl s_client -connect <host>:443 -servername <host>` (or browser dev tools)

**Acceptance:** TLS cert is Let's Encrypt-issued, **NOT** self-signed, **NOT** expired. HTTP code 2xx (or 3xx on root path).

### SM-4 — Rollback works

**Steps:**
1. Make a trivial change (edit `index.html` / `main.py` / `main.go`) and `loopzero deploy` again
2. `loopzero rollback`
3. `curl <live-url>` → check content reverted

**Acceptance:** rollback finishes in **< 30 seconds wall clock**, content reverts to pre-2nd-deploy version, exit code **0**.

### SM-5 — Logs streaming works

**Steps:**
1. `loopzero logs --follow`
2. From another terminal: `curl <live-url>` 3 times
3. Observe the logs terminal

**Acceptance:** the 3 requests appear in the streaming logs within **< 5 seconds each**, structured (JSON or key=value), with a `request_id` / `correlation_id` field.

---

## 3. Auth Journeys (J-01 to J-05, J-15)

### S-04 — Login (J-01)

**Pre-conditions:** account from S-01.

**Steps:**
1. `https://app-dev.loopzero.example/login`
2. Enter email
3. Enter password OR click "Continue with Google/GitHub"
4. Submit

**Acceptance:**
- Successful: dashboard loads
- Wrong password (5 attempts): clear "invalid credentials" error
- OAuth: callback returns to dashboard with session

**Corner cases:**
- (a) Login while already logged in (open `/login` in new tab) → redirect to `/projects`, not new login flow
- (b) Type email with leading/trailing whitespace → should be trimmed, not "user not found"
- (c) Browser cookies disabled → expect explicit error message, not silent failure
- (d) Token expiry mid-session (wait 60+ min idle) → next action triggers refresh OR redirect to `/login` (NOT 500)

**Common failure modes:**
- OAuth provider email collision with seeded admin@loopzero.example silently rejects login (`feedback_kratos_seed_identity_blocks_oidc`)
- CSRF loop on login flow (`feedback_post_cleanroom_kratos_patch_dev_public`)

### S-05 — Register (J-02)

**Pre-conditions:** fresh email (not in S-01).

**Steps:**
1. `/register`
2. Email + password OR OAuth
3. Verify email (click link)

**Acceptance:**
- Verification link is **single-use** (clicking twice → idempotent, not error)
- Account lands in `/projects` empty state
- Plan tier defaulted to **Free** (Starter)
- Browser console clean

**Corner cases:**
- (a) Register email that's also in OAuth provider (Google) → expect clear "use OAuth" hint OR silent merge (whichever is documented)
- (b) Weak password ("password123") → expect rejection with specific guidance, not a stack trace
- (c) Email with `+tag` (`name+test@example.com`) → must be accepted (canonical email)

### S-06 — Verification (J-03)

**Steps:**
1. After register (S-05), click verification link from email
2. If pin/code path: enter the code

**Acceptance:**
- Code is 6 digits, single-use, **valid for ≥ 10 min**
- Wrong code: clear "incorrect" with retry available, NOT account lockout on first miss
- Correct code: lands at dashboard

**Corner cases:**
- (a) Code expires (wait 11+ min, then try) → expect "expired, click to resend" link
- (b) Resend code 3 times rapidly → rate-limit kicks in after N, with clear message
- (c) Navigate to `/verification` directly without flow → friendly redirect to `/register`

### S-07 — Recovery (J-04, DELIVERED)

**Steps:**
1. `/recovery` (Forgot password)
2. Enter email
3. Click email link
4. Set new password

**Acceptance:**
- Recovery link **single-use** + valid ≥ 1 hour
- New password must satisfy policy (length, complexity)
- After reset, old password no longer works (auth attempt → "invalid credentials")
- Auto-login OR redirect to `/login` after reset

**Corner cases:**
- (a) Enter unknown email → response is **identical** to known email (anti-enumeration; OWASP A07)
- (b) Click recovery link twice → second click shows "already used" friendly message
- (c) Set same password as current → either accepted OR refused with explicit "must be different"

### S-08 — Device Success (J-05, DELIVERED)

**Steps:**
1. CLI: `loopzero login` (chooses device flow)
2. Browser opens `/oauth2/device` with code
3. Approve in browser

**Acceptance:**
- `verification_uri` in CLI output points to **public** domain (`auth-dev.loopzero.example` or similar), **NOT** cluster-internal DNS
- After approve in browser, CLI receives token within **< 10 sec**
- "Success" page renders with clear "you can close this tab"

**Corner cases:**
- (a) Refresh `/oauth2/device` page after code expired → expect "code expired" + "request new" link
- (b) Submit wrong code 3 times → expect rate-limit
- (c) Approve in browser AFTER CLI timeout → CLI doesn't hang; browser shows "session expired"

**Common failure modes:**
- Hydra verification_uri leaks cluster-internal DNS (`feedback_j05_aprendizado_hydra_urls_canonical_via_helmfile_overlay`)
- Accept handler 502 silent without error log (`feedback_j05_aprendizado_cloud_accept_error_visibility_slog`)

### S-09 — Profile (J-15)

**Steps:**
1. Click avatar (top right) → "Profile"
2. Update name + avatar + change password
3. Submit

**Acceptance:**
- Changes persist after refresh
- Email change requires re-verification
- Password change invalidates all other sessions (logout flow elsewhere)
- F12 console clean

---

## 4. Project Lifecycle (J-07, J-08, J-09)

### S-10 — Projects List (J-07, DELIVERED)

**Pre-conditions:** at least 2 projects exist (deploy a couple first via CLI).

**Steps:**
1. `/projects`
2. Observe table columns: Name, Apps, Framework, Last deploy

**Acceptance:**
- Columns populated for each project (no `—` placeholders if data exists)
- "Last deploy" shows relative time (e.g., "2 hours ago"), not raw timestamp
- "Load more" pagination shows N of M total (≥ 50 page size)
- Clicking project name navigates to `/projects/:name`

**Corner cases:**
- (a) Empty project list → friendly empty state with "Connect your repo" CTA
- (b) Project with 0 apps → "Apps: 0", framework blank or "—" (not crash)
- (c) Project that has never deployed → "Last deploy: Never" (not "Invalid Date")
- (d) 100+ projects → pagination must work, no infinite scroll lag

**Common failure modes:**
- Endpoint contract narrowing: cards show `—` because backend SQL dropped fields (`feedback_endpoint_contract_narrow_consumer_audit`)

### S-11 — Project Detail Overview (J-08, DELIVERED)

**Steps:**
1. Click into a project from S-10
2. Inspect Overview cards

**Acceptance:**
- 4 cards: **Environments** (count), **Apps** (count), **Last deploy** (status + time), **Live URL** (clickable)
- All numbers match actual state (cross-check with CLI `loopzero project info`)
- Tabs visible: **Deployments**, **Environments**, **Domains**, **Scale**, **Readiness**, **Secrets**, **Settings**

**Corner cases:**
- (a) Project with 0 deployments → "Last deploy: Never", Live URL: "Not yet deployed"
- (b) Project mid-deploy → Last deploy status shows "Deploying…" (animated)
- (c) Project with failed last deploy → status badge red + "Failed", but other cards still populate

### S-12 — Project Connect (J-09, DELIVERED)

**Steps:**
1. From `/projects` click "Connect repo"
2. Choose GitHub
3. Authorize LoopZero GitHub App
4. Pick a repo from the list
5. LoopZero auto-deploys

**Acceptance:**
- Authorization redirects back to LoopZero cleanly (no double-prompt)
- Repo list shows only repos the user has access to
- Auto-deploy starts within **< 30 sec** of selection
- First deploy completes within **< 6 min**

**Corner cases:**
- (a) GitHub App suspended (revoked) → expect clear error + reauthorize CTA
- (b) Repo with no `loopzero.yaml` and no detectable framework → expect "we couldn't auto-detect; here's how" guide
- (c) Repo larger than 500MB → expect clear size-limit error
- (d) Force-push between selection and first deploy → final deploy uses the latest commit

**Common failure modes:**
- Webhook secret missing → silent 404 (`feedback_post_cleanroom_seed_github_app_canonical`)
- Git bundle author empty causes deploy failure on first commit

---

## 5. Deploy Chain (J-17, J-18, J-19)

### S-13 — Deployments Realtime (J-17, DELIVERED)

**Steps:**
1. Open `/projects/:name/deployments`
2. From terminal: `loopzero deploy`
3. Watch the dashboard table

**Acceptance:**
- New deployment row appears within **< 3 sec** of CLI submit (SSE-driven, no manual refresh needed)
- Status badges transition: `Queued` → `Building` → `Releasing` → `Active` (or red `Failed`)
- Terminal status is **absorbing** — once `Active` / `Failed`, badge does NOT regress
- "Refresh" button (manual) is always present in top-right

**Corner cases:**
- (a) 5 concurrent deploys → all 5 rows appear, statuses update independently
- (b) Browser tab background for 5+ min then return → status caught up correctly (SSE reconnect)
- (c) Network blip mid-stream → SSE reconnects within **< 10 sec** without page reload

**Common failure modes:**
- SSE buffered until EOF (`feedback_loopzero_cloud_sse_proxy_buffers_until_eof`)
- Named-event mismatch causes 0 events in client (`feedback_eventsource_named_events_vs_onmessage`)
- Refresh button missing for SSE pages

### S-14 — Status / Runtime Health (J-18, DELIVERED)

**Steps:**
1. `/projects/:name` (Overview)
2. Find the "Runtime health" card / synthesis area

**Acceptance:**
- 4 signals: **Replicas** (running/desired), **Last deploy** (success/fail), **5xx rate** (%), **Active alerts** (count)
- Live updates every **≤ 30 sec** without manual refresh
- Card shows empty-state design (not `—`) when project has never deployed

**Corner cases:**
- (a) Project mid-rollout → Replicas shows transitional (e.g., 2/3) with subtle progress indicator
- (b) Project that scaled to 0 (KEDA) → Replicas "0/0 — idle", not "unhealthy"
- (c) Project deleted moments ago → card shows "Project not found", not stale signals

### S-15 — Runtime Logs Streaming (J-19, DELIVERED)

**Steps:**
1. `/projects/:name/logs`
2. Trigger some app traffic from outside
3. Watch the streaming pane

**Acceptance:**
- New log lines appear within **< 5 sec** of source emission
- Time-range picker (`Last 1h`, `Last 6h`, `Last 24h`, `Last 7d`) works — switching applies immediately
- Search filter (substring match) works as you type, debounced
- "Pause" button stops auto-scroll without dropping incoming lines
- Each log line has timestamp + level (INFO/WARN/ERROR) + message + structured fields if present
- Log line copy button works (clipboard copy)

**Corner cases:**
- (a) Switch project mid-stream → SSE re-subscribes cleanly
- (b) Empty log range (project never logged in last 1h) → "No logs in this range" message, not infinite spinner
- (c) Time-range = `Last 7d` on busy app → pagination/virtualization keeps UI smooth (no browser hang)
- (d) Browser console open with `Preserve log` → confirm no JS errors during 5 min of streaming

**Common failure modes:**
- `?since=` query param ignored by backend (`feedback_streamruntimelogs_since_query_param_ignored`)
- `?env=` missing causes 400 (`feedback_j19_runtime_logs_stream_missing_env_param`)

---

## 6. Power Tools (J-20, J-21, J-22, J-11, J-24, J-25)

### S-16 — Scale (J-20, DELIVERED 2026-06-15)

**Steps:**
1. `/projects/:name/scale`
2. Adjust **Replicas slider**
3. Toggle **HPA** (Horizontal Pod Autoscaler) → expand CPU/memory inputs
4. Toggle **KEDA** scale-to-zero
5. Click "Save" → type-to-confirm dialog
6. Type `scale <projectname>` to confirm

**Acceptance:**
- Slider range bounded by plan: **Free 1–3**, **Pro 1–10**, **Team 1–50**
- KEDA toggle is **grayed-out + tooltip "Upgrade to Pro"** when plan is Free
- HPA inputs: CPU target **[10, 95]**, Memory target **[10, 90]** (out-of-range → inline error)
- **Server-authoritative cost preview**: $X.XX/month updates as slider moves
- Type-to-confirm dialog requires the literal phrase **`scale <projectname>`** (delete-style convention)
- Submit returns HTTP **200** (success) or **402** with `PLAN_UPGRADE_REQUIRED` (Free trying KEDA) or **400** with `HPA_TARGET_OUT_OF_BOUNDS` (invalid input)

**Corner cases:**
- (a) Type wrong projectname in dialog → Submit button stays **disabled**
- (b) Slider to plan max + KEDA enabled simultaneously → both saved correctly
- (c) Concurrent edit from another tab → second tab gets stale-version warning OR last-write-wins with notice
- (d) Browser refresh mid-edit → unsaved changes warning OR auto-discard (whichever is documented)

**Common failure modes:**
- Cost preview computed client-side and tampered → server must recompute (`AssertKEDAAllowed` + `ComputeMonthlyCost`)

### S-17 — Readiness Score (J-21, DELIVERED 2026-06-15)

**Steps:**
1. `/projects/:name/readiness`
2. Inspect score card + checklist

**Acceptance:**
- **ScoreCard**: 0–100 weighted, with "Last visit was X" delta arrow (↑ / ↓ / =)
- **Checklist**: 8 signals, each one of three states:
  - **Pass** (green check)
  - **Fail** (red X) with "Fix it" link
  - **Unknown** (yellow `?`) with "Coming soon" tooltip OR "Configure" link
- **CRITICAL**: signals in **Unknown** state must NOT count toward score numerator OR denominator (don't penalize operator outage)
- Click signal → drawer opens, URL updates `?signal=X` (deep-linkable)
- FAQ panel collapses/expands
- A11y: each badge has `aria-label` describing state (screen-reader testable)

**Corner cases:**
- (a) New project with no deployments → all signals **Unknown**, score should be displayed as "—" or "Configure to see score", NOT 0/100
- (b) Project with broken backend → signals fall to Unknown (not Fail) within 30 sec
- (c) Score boundary 89.5 → must round to 90 (`Math.ceil`), not 89
- (d) Switch projects rapidly → no flicker (settled-gate); cached values clear on switch

**Common failure modes:**
- Empty signals interpreted as Failed instead of Unknown (J-18 v0.15.0 bug class)

### S-18 — Secrets CRUD (J-22, DELIVERED 2026-06-11)

**Steps:**
1. `/projects/:name/secrets`
2. Create a new secret: name `DATABASE_URL`, value `postgres://...`
3. Bulk create: paste a `.env`-style block
4. Edit existing → rotate value
5. Delete → type-to-confirm

**Acceptance:**
- Secret values **NEVER** displayed after creation (write-only contract); UI shows `••••••••` masked
- Create → POST `/api/v1/secrets` → 200 + secret appears in list
- Bulk create → POST `/api/v1/secrets/bulk` → 200 + N rows added; `results` array length === input length (no silent drop)
- Delete → type-to-confirm requires exact secret name
- Plan limit enforced: Free 10 / Pro 50 / Team 250 — try to exceed → 402 with friendly error
- Workload pod gets the new env value after **next** deploy (NOT immediately — by design; verify via `loopzero logs`)

**Corner cases:**
- (a) Secret name with special chars (`X@Y`, `_PREFIX`, `123_START`) → server validates, friendly rejection if invalid
- (b) Bulk paste with 3 valid + 2 malformed → `results: [{name, status}]` shows per-line status, NOT all-or-nothing
- (c) **Zero-leak validation** (CRITICAL): after creating a secret, `kubectl describe pods` and `kubectl logs loopzero-api` must NOT contain the value substring (test team should attempt this with sentinel value)
- (d) Bulk with duplicate names → 409 with the colliding name reported

**Common failure modes:**
- Bulk handler results length mismatch (T1.1 of J-22 plan)
- Audit log captures plaintext (zero-leak invariant from `feedback_secret_canonical_no_hardcode`)

### S-19 — Domains (J-11, DELIVERED 2026-06-15)

**Steps:**
1. `/projects/:name/domains`
2. Click "Add domain" → enter e.g., `staging.mycompany.com`
3. NSInstructionsCard appears with 3-step DNS guide + Copy button per nameserver
4. Update DNS at the user's registrar
5. Click "Reverify" twice in quick succession

**Acceptance:**
- NSInstructionsCard shows **2 nameservers** (matches CF zone)
- Each nameserver has a per-line Copy button → clipboard works
- Reverify button **debounces** rapid double-clicks (ref-based, client-side) AND server rate-limits (max 1 req/30 sec → 429 after that)
- Polling transitions states: `PENDING_NS` → `NS_ACTIVE` → `PROVISIONING` → `ACTIVE`
- Terminal states (`ACTIVE`, `FAILED`, `REMOVING`) are **absorbing** — they do NOT regress on SSE replay
- Browser tab in background (`document.hidden`) → polling pauses, resumes on return

**Corner cases:**
- (a) DNS misconfigured (wrong NS values) → after 5 min, state goes to `FAILED` with troubleshoot drawer auto-opening
- (b) Apex domain (`mycompany.com`, not subdomain) → flow same OR explicit support message (whichever is documented)
- (c) Domain previously deleted from another tenant (race condition) → expect clear error, not 500
- (d) Network blip during reverify → graceful retry, no double-add

### S-20 — Live URL Discoverability (J-25, DELIVERED 2026-06-12)

**Steps:**
1. `/projects/:name` (Overview)
2. Locate **Live URL Hero** card
3. Click "Visit" button → opens URL in new tab
4. Inspect Deployment row "Visit" icon

**Acceptance:**
- Live URL Hero shows the canonical URL (e.g., `https://tn-abc-myapp-production.prod-dev.loopzero.example`)
- "Visit" button targets `target="_blank"` with `rel="noopener noreferrer"`
- URL is reachable (HTTP 2xx OR 4xx app-handler 404 = OK; HTTP 5xx infra error = FAIL)
- TLS cert is Let's Encrypt-issued and matches the FQDN
- Active release indicator (green dot) visible on the row when deploy is `Active`

**Corner cases:**
- (a) Project with **no** active release → Hero shows "Not yet deployed" state, NOT broken URL link
- (b) Project with custom domain attached → Hero prefers custom domain over default
- (c) Multi-entry-app project (e.g., `api` + `worker`) → Hero picks the server entry-app, NOT the static-site one (canonical entry-app resolver)

### S-21 — Configuration / Monaco (J-24)

**Steps:**
1. `/projects/:name/configuration` (or similar)
2. Edit `loopzero.yaml` in the Monaco editor

**Acceptance:**
- Monaco editor renders with syntax highlighting + auto-complete
- YAML validation runs as you type → errors shown inline
- Save button stays disabled until changes are valid
- After save, **next deploy** picks up the new config (NOT immediate runtime change)

**Corner cases:**
- (a) Paste malformed YAML → schema error highlighted with line:col reference
- (b) Edit + leave page without save → unsaved changes warning
- (c) Two tabs open editing same file → conflict resolution (last-write-wins OR merge UI)

> **Note:** J-24 is not yet delivered as of this snapshot — testing it is optional. If the page renders, it's a bonus.

---

## 7. Team & Billing (J-12, J-13, J-14)

### S-22 — Team RBAC (J-12)

**Steps:**
1. `/team` (or `/settings/members`)
2. Invite a teammate by email
3. Assign role: **Viewer** / **Developer** / **Admin**
4. Teammate logs in → verify they see only allowed projects/actions

**Acceptance:**
- Invitation email arrives within **≤ 2 min**
- Roles enforced server-side (NOT just UI-hidden) — Viewer must get 403 on POST `/api/v1/secrets`, NOT silent success
- Admin can revoke teammate → next request from teammate → 401 / redirect

**Corner cases:**
- (a) Invite already-member email → "already a member" notice, not new invite
- (b) Invite email with `+tag` alias → must work
- (c) Cancel invitation before accept → token invalidated (clicking link → "invitation expired")

> **Note:** J-12 may be Early Access or pending — if the page doesn't load, skip and report.

### S-23 — Billing / Stripe parity (J-13)

**Pre-conditions:** on Free plan.

**Steps:**
1. `/settings/billing`
2. Click "Upgrade to Pro"
3. Stripe Checkout opens
4. Use Stripe test card `4242 4242 4242 4242`
5. Return to dashboard

**Acceptance:**
- Stripe Checkout redirects to `cloud-dev.loopzero.example/billing?success=true`
- Plan tier updates to **Pro** within **≤ 10 sec** (webhook-driven)
- "Manage Subscription" button opens Stripe Customer Portal
- Downgrade flow (Pro → Free) works + scheduled at period end

**Corner cases:**
- (a) Stripe webhook delivered out-of-order (`customer.subscription.created` before `checkout.session.completed`) → plan tier still ends at Pro (idempotent + commutative)
- (b) Card declined (`4000 0000 0000 0002`) → friendly error, no partial state
- (c) Refresh `?success=true` URL → does NOT double-charge OR double-upgrade

**Common failure modes:**
- Webhook handler `WHERE stripe_subscription_id` only (not commutative) — both branches must be tested
- Reactivation polling fails when baseline == target

### S-24 — Audit Log (J-14)

**Steps:**
1. `/audit` or `/settings/audit`
2. Filter by **action** (e.g., `deploy.created`), **resource_type**, **since=24h`

**Acceptance:**
- Mutating actions from S-12 (deploy), S-16 (scale), S-18 (secrets), S-19 (domains) all appear
- Each row: timestamp, actor, action, resource, correlation_id (clickable to drill down)
- Filters are URL-persisted (deep-linkable)
- Pagination works for 1000+ entries

**Corner cases:**
- (a) `since=7d` on busy account → virtualized list, no lag
- (b) Filter by tenant_id (multi-tenant admin) → cross-tenant visibility blocked at API level (test via direct curl from another account)

---

## 8. Previews (J-23, DELIVERED 2026-06-07)

### S-25 — Per-PR preview environment

**Pre-conditions:** connected GitHub repo from S-12.

**Steps:**
1. Open a Pull Request on the connected repo
2. Wait for LoopZero GitHub Check
3. Click the preview URL from the Check

**Acceptance:**
- Preview env created within **≤ 6 min** of PR open
- Preview URL format: `<tenant>-<project>-<entry-app>-preview-pr-N.preview-dev.loopzero.example`
- TLS valid (Let's Encrypt wildcard `*.preview-dev.loopzero.example`)
- Sticky destination — second commit to same PR updates same URL
- PR close → preview env deleted within **≤ 5 min**

**Corner cases:**
- (a) Force-push to PR → preview updates to latest commit
- (b) Open 5 PRs on same project → 5 distinct preview URLs, no collisions
- (c) Close + reopen PR → new preview env (not zombie state)
- (d) Long-lived PR (deploy 50+ times) → no orphan resources accumulating

**Common failure modes:**
- Wildcard cert not propagated → 526/503 from edge
- Orphan namespaces after PR close (`feedback_orphan_argocd_application_pattern`)

---

## 9. Environments & Promotion (J-10, DELIVERED 2026-06-07)

### S-26 — Environment lifecycle

**Steps:**
1. `/projects/:name/environments`
2. Create new env `staging`
3. Deploy to staging via CLI: `loopzero deploy --env staging`
4. Promote staging → production via UI

**Acceptance:**
- Create env: POST `/api/v1/environments` → 201 + list refreshes
- Cross-project env name collision → 409 with friendly message
- Promote: POST `/api/v1/projects/:name/promote` with body `{"from":"staging","to":"production"}` → 200
- Promote skips rebuild (image promotion only)

**Corner cases:**
- (a) Free plan attempt 2nd env → 402 `PLAN_UPGRADE_REQUIRED`
- (b) Env name with special chars (`x@y`, ` staging `) → server validates, friendly rejection
- (c) Promote when source env has no active release → 404 with clear message
- (d) Delete `production` env (canonical reserved name) → 400 with "cannot delete reserved"

---

## 10. Cross-Cutting Corner Cases

These apply to **every** scenario above. Bias your testing here — these are the bugs that crossed multiple journeys historically.

### CC-1 — Multi-tenant isolation (CRITICAL — security)

For every endpoint:
1. Login as Tenant A
2. Note a resource ID (project, secret, env)
3. Login as Tenant B
4. Try to access Tenant A's resource ID directly via:
   - URL navigation (`/projects/tenant-a-project`)
   - `curl` with Tenant B's session cookie

**Expected:** HTTP **404 NOT FOUND** (not 403 — 403 leaks existence). For mutations, same: 404.

**Failure mode:** any 200 response with Tenant A's data = **critical bug**. Report immediately.

### CC-2 — Idempotency (CRITICAL — correctness)

For every mutating endpoint (POST/PUT/DELETE):
1. Submit request with `Idempotency-Key: test-1234`
2. Submit same request again with same key
3. Verify second response is **identical** to first (status + body)
4. Verify only ONE side-effect occurred (e.g., only 1 deploy started, not 2)

**Failure mode:** double-execution = **critical bug** for billing/state operations.

### CC-3 — Concurrent operations

For every mutating endpoint:
1. Open 2 terminals
2. Submit same `loopzero deploy` simultaneously (within 100ms)
3. Expected: 1 returns **202 Accepted**, 1 returns **409 CONFLICT** with `CONCURRENT_ACTIVATION` code
4. NEVER both 202 (would race on advisory lock)

### CC-4 — Exit codes (CLI)

Every CLI command must return:
- **0** success
- **1** usage error (bad flags)
- **2** build failure
- **3** deploy failure
- **4** auth error
- **5** partial failure

No other codes. Test each path. Document any unexpected code.

### CC-5 — JSON output stability

Every command supports `--json`. Output is a contract:
1. Run `loopzero whoami --json` → save schema
2. Run `loopzero project info --json` → save schema
3. After re-login or pull latest CLI version → schemas MUST be identical (field names, types)

### CC-6 — No internal terms in user-facing output

Forbidden in CLI output, error messages, JSON, dashboard copy:
- `ArgoCD` / `argocd`
- `GitOps` / `gitops-push`
- `CRD`
- `tenant_id` (use `team` or `account`)
- `advisory lock` / `pg_advisory`
- `Gitea`
- `MinIO`
- `Kaniko`
- `kubebuilder`
- `envtest`

If you see any of these in user-facing text → bug.

### CC-7 — Error messages are actionable

Every error must:
- State what went wrong (not "an error occurred")
- Suggest next action (e.g., "Run `loopzero whoami` to verify auth")
- Include a correlation ID for support
- NOT include stack traces (those go to logs only)

### CC-8 — Browser console is clean

For every dashboard page:
1. Open DevTools (F12) → Console tab → "Preserve log"
2. Navigate to the page
3. Wait 30 sec
4. Trigger 1 interaction (click button, type in input)
5. Expected: **0 JS errors** (warnings OK, deprecation notices OK)

### CC-9 — Network failures

For every async UI:
1. Open DevTools → Network tab → throttling = "Offline"
2. Trigger the action (e.g., "Create secret")
3. Expected: clear "offline" / "network error" toast, retry button, NO infinite spinner
4. Restore network → next attempt works

### CC-10 — Slow network

For every async UI:
1. DevTools → Network → throttling = "Slow 3G"
2. Navigate page
3. Expected: skeleton loaders (not blank screen), then content within reasonable time

### CC-11 — Mobile / responsive

Open every page on:
- Phone width (375px)
- Tablet width (768px)
- Desktop (1280px+)

No horizontal scroll, no text overflow, no overlapping CTAs.

### CC-12 — Accessibility smoke

For every page:
1. Tab through with keyboard only (no mouse)
2. Expected: focus indicator visible, focus order logical, all CTAs reachable
3. Run Axe DevTools extension → **0 critical violations**

---

## 11. Failure Mode Catalog — Historical Bug Patterns

The test team should bias testing toward these patterns. Each one has caused production bugs before.

| Pattern | Where to look | What to check |
|---|---|---|
| **SSE buffered until EOF** | Logs streaming, deploy status | Open Network tab → response is text/event-stream and chunks arrive (not single response at end) |
| **Endpoint contract narrowing** | Project detail / list cards | Compare CLI `loopzero project info` against dashboard — fields should match, not show `—` |
| **Hardcoded values in source** | Any config that says `loopzero.local` / `prod-dev.loopzero.example` | Should be env-driven, not literal in JSX |
| **Silent error swallow** | CLI commands | Force a failure → must surface error message, not silent exit 0 |
| **Stale image in cluster** | After deploy, runtime version | Verify deployed image SHA matches CI build SHA (CLI `loopzero deploy` last line, vs dashboard "Last deploy" sha) |
| **Race condition in webhook handlers** | Stripe billing | Out-of-order webhook delivery should not corrupt state |
| **Cross-tenant via URL guessing** | Any `/projects/:name` page | Manually try `/projects/<another-tenant-project>` after login as different account |
| **Default values overriding env** | Any settings UI | Save a non-default value, refresh — must persist (not silently revert) |
| **Token expiry not handled** | All authenticated pages | Wait 60+ min idle, then click anything → graceful refresh OR redirect, never 500 |
| **Empty state = Failed** | Readiness Score, status cards | New project should show "Unknown / Coming soon", not red "Failed" |

---

## 12. Bug Report Template

Copy this verbatim for every FAIL:

```markdown
## Bug — [one-line summary]

**Reporter:** [name]
**Date / Time UTC:** [YYYY-MM-DD HH:MM]
**Severity:** Critical / High / Medium / Low
**Scenario reference:** [S-NN or SM-N from doc]

### Pre-conditions
- Browser: [Chrome 130 / Firefox 132 / Safari 18]
- OS: [macOS 14 / Windows 11 / Linux ...]
- CLI version: `loopzero --version` output: [...]
- Account state: Free / Pro / Team
- Anything else relevant

### Steps to reproduce
1. ...
2. ...
3. ...

### Expected behavior
[What the acceptance criteria said should happen]

### Actual behavior
[What actually happened — be specific]

### Evidence
- Screenshot: [paste image]
- Browser console output: [paste]
- Network tab failed request:
  - URL: ...
  - Method: ...
  - Request body: ...
  - Status code: ...
  - Response body: ...
- CLI stderr (if relevant): [paste]
- Correlation ID from error message: [if present]
- Time UTC when bug occurred: [YYYY-MM-DD HH:MM:SS — for log correlation]

### Reproducibility
- [ ] Reproduces 100%
- [ ] Reproduces sometimes (X out of Y attempts)
- [ ] Reproduced once, can't reproduce now

### Impact
[Who is affected, what they can't do, what data is at risk]

### Workaround (if known)
[Any way to avoid hitting the bug]
```

---

## 13. Verdict Matrix

At end of full session, compile this matrix:

| Section | Scenarios run | PASS | FAIL | BLOCKED | NOT_TESTED |
|---|---|---|---|---|---|
| 1. Setup (S-01 → S-03) | 3 | | | | |
| 2. Smoke Test (SM-1 → SM-5) | 5 | | | | |
| 3. Auth Journeys (S-04 → S-09) | 6 | | | | |
| 4. Project Lifecycle (S-10 → S-12) | 3 | | | | |
| 5. Deploy Chain (S-13 → S-15) | 3 | | | | |
| 6. Power Tools (S-16 → S-21) | 6 | | | | |
| 7. Team & Billing (S-22 → S-24) | 3 | | | | |
| 8. Previews (S-25) | 1 | | | | |
| 9. Environments (S-26) | 1 | | | | |
| 10. Cross-Cutting (CC-1 → CC-12) | 12 | | | | |
| **Total** | **43** | | | | |

### Verdict bands

| If… | Verdict | Action |
|---|---|---|
| 0 FAIL + 0 BLOCKED + ≥ 90% PASS | **READY** | Ship to broader beta |
| 0 Critical FAIL + 0 High FAIL + ≥ 80% PASS | **READY WITH CAVEATS** | Document Medium/Low FAILs, ship to broader beta with known issues list |
| ≥ 1 Critical FAIL OR > 3 High FAIL | **NOT READY** | Pause beta; engineering must fix Critical+High first, re-test those scenarios |
| BLOCKED ≥ 5 scenarios (cannot test due to env issues) | **NOT REPORTABLE** | Engineering must fix env first; re-run session |

### Critical = automatic NOT READY

These are critical regardless of count — any single one fails the session:
- Multi-tenant isolation breach (CC-1)
- Idempotency double-charge / double-deploy (CC-2)
- Secret value leaked in logs / describe (S-18 corner case c)
- TLS missing or self-signed in production-style env (SM-3)
- Auth bypass on protected route (any unauthenticated 200)

---

## 14. Session Closure

When done, submit:

1. **Filled verdict matrix** (§ 13)
2. **List of all bug reports** filed (§ 12 format) — link to issue tracker
3. **Overall verdict** with 2-3 sentence summary
4. **Recommendation** to engineering: ship / hold / re-test specific scenarios

Send the verdict report to: `getloopscale@gmail.com` (or your designated contact).

---

## 15. References (internal — for engineering follow-up)

These are pointers for the engineering team reading this doc later — external testers can ignore.

- Journey plan inventory: `CLAUDE.md § Dashboard Journeys` (20 journey plans)
- Architecture: `CLAUDE.md § Architecture Overview` (Four Planes)
- State machines: `CLAUDE.md § State Machines` (11 machines)
- Multi-cell routing: `CLAUDE.md § Multi-Cell Routing`
- Known systemic patterns: `CLAUDE.md § Known Systemic Patterns`
- Common mistakes: `CLAUDE.md § Common Mistakes`
- Cleanroom + LIVE TEST mandate: `.claude/rules/cleanroom-up-live-test-mandate.md` (INQUEBRÁVEL)
- Per-feature LIVE regression workflow: `feedback_post_feature_live_regression_workflow_2026_06_09`
- Recent deep audit: `api/review-output/final_report.md` (Deep Review Loop 2026-06-01, addendum 2026-06-16)
- Latest test gate results at HEAD `b03ea24b`:
  - `/code-quality` PASS (100/100, zero findings)
  - `go test ./...` 39/39 packages ok
  - `chaos-test --full` 13/13 PASS (542s on live cluster)
  - `task doctor` PASS=42 WARN=5 FAIL=0
  - Deep Review Loop 0 critical open

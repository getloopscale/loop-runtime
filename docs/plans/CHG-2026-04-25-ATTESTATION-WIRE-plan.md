# CHG-2026-04-25-ATTESTATION-WIRE — Executable Plan

> **Status:** DRAFT — pending T2 governance meeting
> **Owner:** TBD (assigned post-meeting)
> **Target completion:** 1-2 dev sessions after gate approval
> **Decision record (post-meeting):** `docs/meetings/adr/0014-attestation-wiring.md`

## 0. Goal

Wire the supply-chain attestation pipeline so that:
1. `releases.image_digest`, `releases.image_signature`, `releases.image_signed_at` are **persisted** for every release where the build workflow signed the image.
2. `GET /api/v1/builds/:id/attestation` returns **real, queryable data** instead of always 404.
3. Future Sprint 6 work (admission policy that rejects unsigned images) has a truth source it can rely on.

**Non-goals:**
- SBOM generation (syft) — separate change.
- Sigstore Rekor transparency log integration — out of scope.
- Backfilling attestation for legacy releases (NULL stays NULL pre-change; new releases get populated forward).
- Cosign keyless / OIDC flow — staying on key-based for now (cluster has no OIDC provider).

---

## 1. Pre-conditions (must be true before Phase 1 starts)

| # | Pre-condition | How to verify |
|---|---|---|
| P-1 | T2 governance meeting concluded with verdict APPROVED | `docs/meetings/records/MEET-YYYY-MM-DD-NNN.md` exists with status APPROVED |
| P-2 | ADR-0014 drafted and accepted | `docs/meetings/adr/0014-attestation-wiring.md` reviewed by Platform Security Engineer |
| P-3 | Governance gate file lists exactly the 16 affected files | `cat .claude/governance-gate.local.md` shows scope |
| P-4 | Local k3d cluster is green (`loopzero deploy` E2E completes) | `infra/tests/e2e/local-install-deploy.sh` exits 0 |
| P-5 | Working tree is clean (no uncommitted unrelated changes) | `git status --short` returns empty |

**Gate:** if any of P-1..P-5 fails, STOP. The plan does not authorize implementation without all five.

---

## 2. Test strategy (TDD — written before code)

Per `.claude/rules/tdd-workflow.md`: **failing test first, then implementation.**

Test pyramid for this change:

| Level | What | Where |
|---|---|---|
| Unit | `cosignSignTemplate` declares `outputs.parameters` with two named outputs and the correct paths | `api/internal/argo/cosign_test.go` |
| Unit | `buildImageTemplate` declares an output param exporting `containerimage.digest` | `api/internal/argo/buildkit_test.go` |
| Unit | `WorkflowSubmitter.GetStatus` parses `status.nodes[*].outputs.parameters[]` into `WorkflowStatus.Outputs` | `api/internal/argo/workflow_submitter_test.go` (uses fixture YAML) |
| Unit | `BuildAppRelease` populates `ImageDigestSHA256/Signature/SignedAt` when outputs present; nil when absent | `api/internal/services/manifest_builder_test.go` |
| Unit | `Release` struct round-trips three nullable signature fields through `Insert`/`scanRelease` | `api/internal/db/release_store_test.go` (against testcontainers PG) |
| Unit | `attestationStore.GetBuildAttestation` returns populated `AttestationInfo` for signed release; returns `signed: false` for legacy NULL release; returns sentinel for unknown build | `api/internal/db/attestation_store_test.go` |
| Component | `phaseACreateRelease` persists 3 columns when `AppRelease[0].ImageDigestSHA256 != ""`; tolerates NULL when empty (non-fatal on failure) | `api/internal/services/container_delivery_test.go` |
| Component | `GET /api/v1/builds/:id/attestation` returns populated body for signed build; `{signed: false}` for unsigned; 404 for unknown | `api/internal/routes/build_attestation_test.go` (httptest) |
| Component | `BuildReconcileLoop.onWorkflowSucceeded` enriches `BuildCallbackRequest.Apps[*]` from workflow outputs | `api/internal/services/build_reconcile_complete_test.go` |
| E2E | `infra/tests/e2e/local-install-deploy.sh` extended: after `loopzero deploy` returns ACTIVE, query `/attestation` and assert `signed=true`, `image_signed_at != null`, `image_digest =~ /^sha256:[a-f0-9]{64}$/` | `infra/tests/e2e/local-install-deploy.sh` |

**Failure-path tests (mandatory per Rule 1 + spec §12):**

| Failure | Expected behavior | Test |
|---|---|---|
| Cosign step fails (workflow Failed) | No release created, no callback fires, `/attestation` returns 404 | unit on `BuildReconcileLoop` |
| Cosign succeeds but output file missing (CWT misconfigured) | Release created with NULL signature columns, deploy still completes, `/attestation` returns `signed: false`, log a warning | unit + component |
| Persistence of signature columns fails (DB constraint, transient error) | Deploy still completes (signature persistence is non-fatal); `image_signed_at` stays NULL; warning logged | unit on `phaseACreateRelease` |
| `/attestation` for build owned by another tenant | 404 (NOT 403 — must not leak existence) | route test |

---

## 3. Phases

Each phase has a clear **DoD** with mechanical verification commands. Do not advance to the next phase until **all** DoD items pass.

### Phase 1 — Pipeline contract: CWT outputs (foundation)

**Why first:** Everything downstream depends on the workflow actually emitting the data. If this is wrong, all later phases are based on imaginary inputs.

**Steps:**

1.1. Add output param block to `cosignSignTemplate` (cosign.go):
- After `cosign sign` succeeds, write `${TAG}@${DIGEST}` to `/tmp/cosign-sig.txt` and `${DIGEST}` to `/tmp/cosign-digest.txt`.
- Declare `Outputs: []Parameter{{Name: "signature", ValueFrom: &ValueFrom{Path: "/tmp/cosign-sig.txt"}}, {Name: "digest", ValueFrom: ...}}` on the forge.Container.
- Both files written by the cosign container only (path is in the container's own `/tmp`, NOT in `/workspace` shared volume — security).

1.2. Add output param block to `buildImageTemplate` (buildkit.go):
- After BuildKit pushes, parse `containerimage.digest` from `/workspace/metadata-${app}.json` and write to `/tmp/build-digest.txt`.
- Declare `Outputs: [{Name: "digest", ValueFrom: {Path: "/tmp/build-digest.txt"}}]`.

1.3. Validate template structure with kubeconform/argo lint:
- `helm template charts/loopzero-platform | kubeconform --strict` must pass.
- `argo lint` against rendered WorkflowTemplate must pass (if argo-cli installed in CI).

1.4. **Acceptance criteria (Phase 1):**
- AC-1.1: `api/internal/argo/cosign_test.go` includes `TestCosignSignTemplate_DeclaresOutputs` — verifies presence of two named outputs with correct paths.
- AC-1.2: `api/internal/argo/buildkit_test.go` includes `TestBuildImageTemplate_DeclaresDigestOutput`.
- AC-1.3: A real build run (in local k3d) produces a workflow whose `kubectl get workflow ... -o jsonpath='{.status.nodes.*.outputs.parameters}'` returns non-empty for the cosign + build steps.
- AC-1.4: All existing tests still pass (`cd api && go test ./...`).

**Phase 1 DoD:**
```bash
# All must return 0
cd /home/paulo/loopzero/api
go test ./internal/argo/... -run 'Cosign|BuildImage|Outputs' -v && \
helm template /home/paulo/loopzero/charts/loopzero-platform | kubeconform --strict && \
go test ./... && \
echo "Phase 1 DONE"
```
Plus manual: trigger one fresh deploy, then `kubectl get workflow $WF -o jsonpath='{.status.nodes}' | jq` shows outputs.parameters populated.

---

### Phase 2 — Workflow status parsing (bridge)

**Why second:** Code-only change to bridge between the now-emitting workflow and the consumers; no behavior change yet at the persistence layer.

**Steps:**

2.1. Extend `services.WorkflowStatus` (in `build_orchestrator.go` or its types file):
```go
type WorkflowStatus struct {
    Phase      string
    FinishedAt time.Time
    // NEW: per-step outputs. Key: step name (e.g., "sign-image-api",
    // "build-image-api"). Value: param name → string value.
    Outputs map[string]map[string]string
}
```

2.2. Update `WorkflowSubmitter.GetStatus` to parse `status.nodes[*].outputs.parameters[]`:
- Iterate `status.nodes` (an unstructured map).
- For each node where `type == "Pod"` and `outputs.parameters` is non-empty, populate the `Outputs` map keyed by the node's `displayName` (which matches the template name).
- Be defensive: missing `outputs` ⇒ entry not added. Missing `parameters` ⇒ empty inner map. Missing `value` ⇒ skip that param.

2.3. **Acceptance criteria (Phase 2):**
- AC-2.1: `TestGetStatus_ParsesOutputs` (uses fixture YAML of a real workflow) — verifies `Outputs["sign-image-api"]["signature"] == "registry.../app:abc@sha256:xyz"`.
- AC-2.2: `TestGetStatus_NoOutputs_ReturnsEmptyMap` — workflow without output params yields `Outputs == nil` or empty map (not nil-map panic).
- AC-2.3: All consumers of `WorkflowStatus` compile (no breaking change at struct level — new field is additive).
- AC-2.4: Sidecar-fix logic (lines 233-243 in current file) untouched.

**Phase 2 DoD:**
```bash
cd /home/paulo/loopzero/api
go test ./internal/argo/... -run 'TestGetStatus' -v && \
go vet ./... && \
go test ./... && \
echo "Phase 2 DONE"
```

---

### Phase 3 — Domain types: AppRelease + Release

**Why third:** Pure type changes — no logic moves yet. Sets the shape that Phase 4-5 will populate/consume.

**Steps:**

3.1. Extend `AppRelease` in `build_orchestrator.go`:
```go
type AppRelease struct {
    Name        string
    ImageDigest string  // existing — note: this is the IMAGE TAG (legacy naming)
    URL         string
    Type        string
    Rendering   string
    DurationMs  int

    // NEW (CHG-2026-04-25-ATTESTATION-WIRE):
    ImageDigestSHA256 string     `json:"image_digest_sha256,omitempty"`
    ImageSignature    string     `json:"image_signature,omitempty"`
    ImageSignedAt     *time.Time `json:"image_signed_at,omitempty"`
}
```

3.2. Extend `db.Release` in `release_store.go`:
```go
type Release struct {
    // ... existing fields ...
    ImageDigest    *string    // NEW
    ImageSignature *string    // NEW
    ImageSignedAt  *time.Time // NEW
}
```

3.3. Update `Insert` and `scanRelease` (or equivalent) to handle the three new nullable columns. Use `sql.NullString` / `pgx.Pgtype` as established in the codebase.

3.4. **Acceptance criteria (Phase 3):**
- AC-3.1: `TestRelease_RoundtripSignatureFields` — insert a release with all 3 fields populated, fetch back, assert equality. Uses real PG via testcontainers per Rule 7.
- AC-3.2: `TestRelease_NullSignatureFields_LegacyCompat` — insert without signature fields, fetch back, all three are nil. Confirms legacy releases pre-change still scan cleanly.
- AC-3.3: All existing release tests pass unchanged.

**Phase 3 DoD:**
```bash
cd /home/paulo/loopzero/api
go test ./internal/db/... -run 'Release' -v && \
go build ./... && \
echo "Phase 3 DONE"
```

---

### Phase 4 — Reconciler enrichment (read side)

**Steps:**

4.1. In `build_reconcile_complete.go`, after `r.workflowClient.GetStatus(...)` returns success, extract per-app outputs:
```go
for i, app := range buildApps {
    signStep := fmt.Sprintf("sign-image-%s", app.Name)
    buildStep := fmt.Sprintf("build-image-%s", app.Name)
    if sigOutputs, ok := workflowStatus.Outputs[signStep]; ok {
        if sig := sigOutputs["signature"]; sig != "" {
            buildApps[i].ImageSignature = sig
            now := time.Now().UTC()
            buildApps[i].ImageSignedAt = &now
        }
    }
    if buildOutputs, ok := workflowStatus.Outputs[buildStep]; ok {
        if dig := buildOutputs["digest"]; dig != "" {
            buildApps[i].ImageDigestSHA256 = dig
        }
    }
}
```

4.2. **Acceptance criteria (Phase 4):**
- AC-4.1: `TestOnWorkflowSucceeded_EnrichesAppsWithSignature` — feeds a fake `WorkflowStatus` with outputs, asserts callback receives populated AppRelease entries.
- AC-4.2: `TestOnWorkflowSucceeded_NoOutputs_LeavesAppsUntouched` — workflow without outputs ⇒ AppRelease has empty signature fields, BUT the callback still fires successfully (deploy must not break for legacy workflows).
- AC-4.3: `image_signed_at` is set to `time.Now().UTC()` (server time) — NOT to the workflow finish time, NOT parsed from the cosign output. Rationale: the ONLY trustworthy timestamp is when the API committed the row; pod clocks are not authoritative.

**Phase 4 DoD:**
```bash
cd /home/paulo/loopzero/api
go test ./internal/services/... -run 'OnWorkflowSucceeded|BuildReconcile' -v && \
echo "Phase 4 DONE"
```

---

### Phase 5 — Persistence (write side)

**Steps:**

5.1. In `container_delivery.go:phaseACreateRelease`, before the `c.releaseStore.Insert(...)` call:
```go
// CHG-2026-04-25-ATTESTATION-WIRE: persist signature columns if the
// build workflow declared them. Non-fatal: if signature data is
// missing or persistence fails partially, the release still goes
// through with NULL columns. This is safer than failing the deploy
// on an observability gap.
if len(result.Apps) > 0 {
    first := result.Apps[0]
    if first.ImageDigestSHA256 != "" {
        digest := first.ImageDigestSHA256
        release.ImageDigest = &digest
    }
    if first.ImageSignature != "" {
        sig := first.ImageSignature
        release.ImageSignature = &sig
    }
    if first.ImageSignedAt != nil {
        release.ImageSignedAt = first.ImageSignedAt
    }
}
```

5.2. **Acceptance criteria (Phase 5):**
- AC-5.1: `TestPhaseACreateRelease_PersistsSignatureColumns` — when AppRelease has signature fields, the inserted Release has matching populated columns.
- AC-5.2: `TestPhaseACreateRelease_LegacyAppRelease_NullColumns` — empty signature fields ⇒ NULL columns; release insert succeeds.
- AC-5.3: `TestPhaseACreateRelease_PartialSignatureData` — only digest, no signature ⇒ digest column populated, signature NULL, signed_at NULL. (Defensive: never set signed_at without signature.)
- AC-5.4: **Multi-app builds**: only `Apps[0]`'s signature is persisted on the release. Document that `releases.image_*` represents the "primary app" for now; multi-app attestation is out of scope (revisit when admission policy lands).

**Phase 5 DoD:**
```bash
cd /home/paulo/loopzero/api
go test ./internal/services/container_delivery... -v && \
echo "Phase 5 DONE"
```

---

### Phase 6 — AttestationStore impl (read API)

**Steps:**

6.1. Create `api/internal/db/attestation_store.go`:
```go
type PostgresAttestationStore struct {
    db *pgxpool.Pool
}

func (s *PostgresAttestationStore) GetBuildAttestation(
    ctx context.Context, buildID string,
) (*routes.AttestationInfo, error) {
    // Query: latest release for this build_id, ORDER BY created_at DESC LIMIT 1.
    // If none, return ErrBuildNotFound. If found, map to AttestationInfo with
    // signed = (image_signed_at IS NOT NULL).
}
```

6.2. In `api/cmd/setup_routes.go` (or wherever routes wire up), construct the store and pass it to `NewBuildAttestationHandler`.

6.3. Verify tenant isolation: the `/attestation` route is behind `requireAuth`. The handler must additionally check that the build belongs to the caller's tenant — either via a join in the SQL (`WHERE build.tenant_id = $2`) or via a separate ownership check before query.

6.4. **Acceptance criteria (Phase 6):**
- AC-6.1: `TestAttestationStore_SignedRelease_Returns200` — populated release ⇒ AttestationInfo with `signed: true`, all 3 fields set.
- AC-6.2: `TestAttestationStore_LegacyRelease_ReturnsSignedFalse` — release with NULL signature ⇒ `signed: false`, other fields empty.
- AC-6.3: `TestAttestationStore_UnknownBuild_Returns404` — non-existent build ⇒ sentinel error → 404.
- AC-6.4: `TestAttestationHandler_CrossTenant_Returns404` — tenant A queries tenant B's build ⇒ 404 (not 403). Critical for non-leakage.
- AC-6.5: `TestAttestationHandler_Unauthenticated_Returns401` — no token ⇒ 401 (existing requireAuth covers this; verify in test).

**Phase 6 DoD:**
```bash
cd /home/paulo/loopzero/api
go test ./internal/db/attestation_store_test.go -v && \
go test ./internal/routes/build_attestation_test.go -v && \
echo "Phase 6 DONE"
```

---

### Phase 7 — E2E + observability

**Steps:**

7.1. Extend `infra/tests/e2e/local-install-deploy.sh`:
- After Phase 5 (curl assertion succeeds), add Phase 6:
  ```bash
  log "── Phase 6: attestation assertion"
  ATTEST=$(curl -sf -H "Authorization: Bearer $TOKEN" "$API_URL/api/v1/builds/$BUILD_ID/attestation")
  echo "$ATTEST" | jq -e '.signed == true' || fail "expected signed=true" 5
  echo "$ATTEST" | jq -re '.image_digest' | grep -E '^sha256:[a-f0-9]{64}' || fail "image_digest is not a sha256" 5
  echo "$ATTEST" | jq -re '.signed_at' | grep -qE '^[0-9]{4}-' || fail "signed_at not present" 5
  log "  ✓ Attestation present and well-formed"
  ```
- Capture `BUILD_ID` from the `loopzero deploy` output (it's already visible in the build callback flow).

7.2. Add a Prometheus metric `loopzero_release_signed_total{tenant, signed}` — counter incremented in `phaseACreateRelease` post-insert. Confirms in production that signing is actually happening.

7.3. **Acceptance criteria (Phase 7):**
- AC-7.1: Smoke script E2E passes end-to-end with the new attestation assertion (no `-k`, signed=true, sha256 digest format).
- AC-7.2: `loopzero_release_signed_total{signed="true"}` increments by 1 after each successful signed deploy in the test cluster (verify via `kubectl exec ... promtool query instant ...` or curl `/metrics`).

**Phase 7 DoD:**
```bash
infra/tests/e2e/local-install-deploy.sh && echo "Phase 7 DONE"
```

---

### Phase 8 — Documentation + cleanup

**Steps:**

8.1. Write `docs/meetings/adr/0014-attestation-wiring.md` (post-meeting):
- Context (the gaps + the "lying endpoint" observation).
- Decision (CWT outputs vs. lazy verify vs. log scraping — chosen path).
- Trust model (output file isolation, why `time.Now().UTC()` and not workflow finish time).
- Backward compat contract (NULL = legacy, never invalid).
- Consequences (admission policy unblocked, multi-app attestation deferred).

8.2. CHANGELOG entry under `[Unreleased] / Added`:
> Supply chain attestation now persisted on every release. `GET /api/v1/builds/:id/attestation` returns real signature data instead of always 404. (#CHG-2026-04-25-ATTESTATION-WIRE)

8.3. Delete `.claude/governance-gate.local.md` to relock production code.

**Phase 8 DoD:**
```bash
test -f /home/paulo/loopzero/docs/meetings/adr/0014-attestation-wiring.md && \
grep -q "CHG-2026-04-25-ATTESTATION-WIRE" /home/paulo/loopzero/CHANGELOG.md && \
test ! -f /home/paulo/loopzero/.claude/governance-gate.local.md && \
echo "Phase 8 DONE"
```

---

## 4. Rollback plan

Every phase is **independently revertible**:

| Phase | Revert command | Effect |
|---|---|---|
| 1 | `git revert <CWT-commit>` + helm dep update + helm upgrade | Workflows revert to no-output; downstream code with output parsing degrades gracefully (Phase 2-7 all tolerate missing outputs). |
| 2 | `git revert <parser-commit>` | `WorkflowStatus.Outputs` always nil; downstream tolerates. |
| 3 | `git revert <types-commit>` | Code won't compile if Phase 4-7 still applied; revert in reverse order. |
| 4 | `git revert <reconciler-commit>` | AppRelease no longer enriched; Phase 5 sees empty fields ⇒ NULL columns. |
| 5 | `git revert <persistence-commit>` | New releases stop persisting signature columns; existing populated rows untouched. |
| 6 | `git revert <store-commit>` | `/attestation` returns 404 again (back to current state). |
| 7 | Smoke script revert | Smoke no longer asserts signed=true; pre-change behavior restored. |

**Hot-rollback target:** Phase 5 (persistence) is the highest-risk phase because it interacts with the deploy hot path. If a deploy regression is detected in production, reverting Phase 5 alone restores deploys without losing observability of already-signed releases. **MTTR target: < 10 min** (single git revert + redeploy API).

---

## 5. Risk register

| # | Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|---|
| R-1 | Cosign output file written to shared volume ⇒ tamper risk | Low | High (security) | Phase 1 explicitly uses container-local `/tmp` paths; Platform Security Engineer reviews CWT diff. |
| R-2 | Persistence error fails the release insert ⇒ deploys break | Low | High (availability) | Phase 5 wraps signature persistence in non-fatal logic; if column write fails, log + insert with NULLs. AC-5.2 covers. |
| R-3 | CWT output param parsing differs between Argo versions | Low | Medium | Phase 2 uses defensive parsing (any missing field ⇒ skip). Pin Argo version in CWT chart's Chart.yaml. |
| R-4 | Multi-app build attestation: `Apps[0]` is arbitrary | Medium | Medium | Phase 5 AC-5.4 documents; ADR-0014 records as known limitation; future work tracked. |
| R-5 | Attestation cross-tenant leak via misconfigured handler | Low | High (data leak) | Phase 6 AC-6.4 explicitly tests; API/Domain Architect reviews route. |
| R-6 | Old releases (NULL columns) misread as "signed but unverifiable" | Low | Low | `signed: false` when `image_signed_at IS NULL`. Test AC-6.2 covers. |
| R-7 | Workflow output capture failure mode invisible (no metric) | Medium | Low | Phase 7 metric `loopzero_release_signed_total{signed=...}` makes it visible in dashboards. |

---

## 6. Success metrics (post-deploy)

Measured 7 days after merge, on production DigitalOcean:

| Metric | Target | Measurement |
|---|---|---|
| % new releases with `image_signed_at IS NOT NULL` | > 99% | `SELECT count(*) FILTER (WHERE image_signed_at IS NOT NULL) * 100.0 / count(*) FROM releases WHERE created_at > now() - interval '7 days'` |
| `/attestation` endpoint p99 latency | < 50ms | Grafana/Prometheus `loopzero_http_request_duration_seconds{route="/api/v1/builds/{id}/attestation",quantile="0.99"}` |
| Deploy regression rate (pre vs. post) | No increase | Compare `loopzero deploys failed` count 7-day window before/after |
| Number of attestation cross-tenant 404s in audit log | 0 | Spec implies these are bugs; audit review weekly. |

---

## 7. Open questions for the meeting

These should be resolved before the gate is written. Listing here so the Meeting Orchestrator can drive resolution.

| # | Question | Stakeholder |
|---|---|---|
| Q-1 | Is `time.Now().UTC()` (API insert time) acceptable for `image_signed_at`, or must we use the workflow finish time? Trade-off: clock skew vs. trust model. | Platform Security Engineer |
| Q-2 | Multi-app builds: persist `Apps[0]` only, or extend `releases` schema with a per-app sidecar table? | API/Domain Architect |
| Q-3 | Should signature persistence be non-fatal (current design) or fatal (no NULL columns ever after CWTs upgraded)? Trade-off: deploy reliability vs. data quality. | Production Design Reviewer |
| Q-4 | Tenant ownership check on `/attestation`: SQL join or pre-query lookup? | API/Domain Architect |
| Q-5 | Do we expose `image_digest` (sha256) in the `loopzero audit` CLI output, or only via `/attestation`? | DX Engineer |

---

## 8. Out of scope (explicit non-goals revisited)

To avoid scope creep mid-implementation:

- **NOT in this change:** SBOM generation (syft), Rekor transparency log, keyless cosign with OIDC, admission policy that rejects unsigned images, backfill of legacy releases, multi-app attestation table, signature verification at runtime by the API, public attestation endpoint without auth, retroactive signing of already-built images.
- **In this change but minimal:** Prometheus metric (one counter only), CHANGELOG entry, ADR-0014.

---

## 9. Plan completion checklist

Plan is "DONE" (and the change is "shipped") when **every** box below is checked:

- [ ] Pre-conditions P-1..P-5 verified
- [ ] Phase 1 DoD passed (CWT outputs)
- [ ] Phase 2 DoD passed (parser)
- [ ] Phase 3 DoD passed (types)
- [ ] Phase 4 DoD passed (reconciler)
- [ ] Phase 5 DoD passed (persistence)
- [ ] Phase 6 DoD passed (store + handler)
- [ ] Phase 7 DoD passed (E2E + metric)
- [ ] Phase 8 DoD passed (ADR + CHANGELOG + gate cleanup)
- [ ] All tests in `cd api && go test ./...` pass
- [ ] `helm template charts/loopzero-platform | kubeconform --strict` passes
- [ ] Smoke E2E script (`infra/tests/e2e/local-install-deploy.sh`) passes
- [ ] Async reviewers (Platform Security, Helena Duarte for pipelines) sign off in PR comments
- [ ] PR merged, deployed to dev DO, attestation visible for one new release
- [ ] `.claude/governance-gate.local.md` deleted

---

## Appendix A — Files touched (governance-gate scope)

```
api/internal/argo/cosign.go
api/internal/argo/cosign_test.go
api/internal/argo/buildkit.go
api/internal/argo/buildkit_test.go
api/internal/argo/workflow_submitter.go
api/internal/argo/workflow_submitter_test.go
api/internal/services/build_orchestrator.go
api/internal/services/build_reconcile_complete.go
api/internal/services/build_reconcile_complete_test.go
api/internal/services/container_delivery.go
api/internal/services/container_delivery_test.go
api/internal/services/manifest_builder.go
api/internal/services/manifest_builder_test.go
api/internal/db/release_store.go
api/internal/db/release_store_test.go
api/internal/db/attestation_store.go               # NEW
api/internal/db/attestation_store_test.go          # NEW
api/internal/routes/build_attestation.go            # minor: tenant check
api/internal/routes/build_attestation_test.go
api/cmd/setup_routes.go                             # wire concrete store
api/internal/metrics/metrics.go                     # add release_signed_total
infra/tests/e2e/local-install-deploy.sh             # extend with Phase 6
docs/meetings/adr/0014-attestation-wiring.md       # NEW
CHANGELOG.md
```

Total: 17 modified + 4 new = **21 files**, ~250 LoC + ~150 LoC tests.

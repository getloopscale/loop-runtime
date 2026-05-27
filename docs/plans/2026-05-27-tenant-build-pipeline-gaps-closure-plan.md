# Plan: Tenant Build Pipeline — Gaps Closure (M07 → 3.9 ceiling)

> **Version 1.0** — Fecha 4 gaps actionable do M07 audit 2026-05-26 (Build Pipeline) por meio de engineering deliverables remaining: cosign keyless infrastructure activation (5 sub-deliverables), SBOM-to-image attestation linking (SLSA L3), Harbor mirror observability, OTel sampling docs. Outcome: M07 maturity sobe de 3.4 → ~3.9 (teto ADR D3 cap por M11 #25 string-aliases globally intractable). Critical-path real é GAP-1 (cosign keyless deadline 2026-08-10 ticking).

## Context

**O que existe hoje (AS-IS):**
- Tenant build pipeline funcional E2E: BuildKit + cosign keyed + syft SBOM + push DOCR
- Validações: cleanroom dogfood 106/0/255 GREEN (2026-05-25), L2 prod-identical drill iter 55 (2026-05-18)
- M07 maturity score 2026-05-26: **3.4/4.0** (baseline). Status update 2026-05-27 estima **3.7** se re-audit hoje post-shipped fixes.

**Evidência motivacional:**
- ADR-2026-05-12 cosign keyless hard deadline **2026-08-10** (74 dias). Script `check-cosign-cutover-deadline.sh` BLOQUEIA prod builds post-deadline.
- M07 audit § 7 Status Update 2026-05-27 enumera 5 sub-deliverables remaining + 3 LOW/MEDIUM gaps históricos.
- SLSA L3 compliance gap (SBOM-attestation linking) bloqueia Enterprise tier compliance posture.
- MEET-2026-05-27-batch-sponsor-approvals ratificou direção; engineering deliverables são consequência.

**O que já foi shipped (este sprint):**
- ✅ `infra/pulumi-platform/src/services/sigstore.ts` — Pulumi `Sigstore` ComponentResource (offline TUF mandatory)
- ✅ `infra/helmfile/runtime.helmfile.yaml.gotmpl` — Helmfile stanza DELETED per GitOps two-writers veto
- ✅ T-30d/T-15d/T-7d warnings em deadline script
- ✅ Trivy + TruffleHog re-wired em `task quality:gates`
- ✅ VAP `loopzero-require-image-digest` warn-mode shipped

## Objective

M07 sobe a teto ADR D3 (~3.9) com todos engineering deliverables shipped + zero outstanding gaps actionable. **Done quando:**

- [ ] Cosign keyless infrastructure provisioning engineering-complete (5 sub-deliverables)
- [ ] SBOM-to-image cryptographically linked via `cosign attest --predicate`
- [ ] Harbor mirror failures visible em Loki + alert
- [ ] OTel sampling policy documented em build-pipeline runbook
- [ ] M07 re-audit (R11 of roadmap) confirma ≥3.7 (escolar para 3.9 post-T2.2 activation)
- [ ] Zero regression em existing journeys (`/dogfood full` PASS)

## ADRs

### D1 — SLSA L3 attestation via `cosign attest --predicate` (não custom predicate spec)

**Decisão:** Use Sigstore's standard SLSA Provenance + CycloneDX SBOM attestation predicates (in-toto v1.0). NOT custom LoopZero-specific predicate.

**Rationale:** Compliance auditors (PCI DSS, SOC 2 Type II, ISO 27001) recognize SLSA + in-toto standards. Custom predicates require auditor whitelisting per audit. Adopters: GitHub Container Registry, Distroless images, npm provenance — all use in-toto SLSA.

**Consequences:**
- ✅ Standard predicate types: `https://slsa.dev/provenance/v1` for build provenance, `https://cyclonedx.org/bom` for SBOM linkage
- ✅ `cosign verify-attestation` works out-of-box for downstream consumers
- ⚠️ Requires `cosign` v2.2+ (already pinned in LoopZero per constants.go)
- ⚠️ Trillian/Rekor entries grow ~2x (signature + attestation per image) — covered in GAP-1.a storage model

### D2 — Harbor mirror failure surfaces via Argo Workflow exit hooks (não polling)

**Decisão:** Use Argo Workflows `exitHandler` template on `mirror-harbor-{app}` step. Exit handler queries step result + emits Loki log line + Prometheus counter increment when failed.

**Rationale:** `continueOn.failed=true` (current) preserves overall Workflow success but loses signal. Polling approach (separate cronjob) adds latency + complexity. Exit hooks are native Argo Workflows pattern + zero new infrastructure.

**Consequences:**
- ✅ Signal preserved without breaking "best-effort" semantics
- ✅ Loki log enables Grafana alert + dashboard
- ⚠️ Exit handler adds ~5s to Workflow overall (acceptable)

### D3 — OTel sampling policy = head-based 10% sampling for build workflows

**Decisão:** Sample 10% of Workflows for full trace span recording. 100% sampling on errors (tail-based).

**Rationale:** Build workflows produce ~30 spans each (clone → cache → build → sign → SBOM → push). At 100% sampling × 100 builds/day, OTel ingestion costs $/mo grow significantly. 10% head + 100% error matches industry practice (Datadog, Honeycomb defaults).

**Consequences:**
- ✅ 90% cost reduction on OTel storage
- ⚠️ Random sample may miss specific build for forensic — operator can re-build with `LOOPZERO_BUILD_FORCE_TRACE=true` env var (escape hatch)
- ✅ Errors always traced — debugging path preserved

### D4 — Cosign keyless activation gated by offline TUF ceremony OUTPUT (não preview-time validation only)

**Decisão:** Per ADR-2026-05-27-T2.2-OFFLINE-TUF: Pulumi component REJECTS construction without `tufPreMintedKeys` Secret reference. The ceremony output Secret MUST exist in `sigstore-system` namespace BEFORE `pulumi up`.

**Rationale:** Defense-in-depth against "deploy now, ceremony later" anti-pattern that produces sigstore-strictly-worse-than-keyed state per SBR conditional veto.

**Consequences:**
- ✅ Activation impossible without ceremony having occurred
- ⚠️ Operator runbook becomes mandatory pre-activation step
- ⚠️ First activation blocked until multi-party ceremony scheduled

## Dependency Graph

```
Phase 0 — Cleanup + AS-IS verification
   │
   ├─▶ Phase 1 — Cosign keyless engineering deliverables (GAP-1)
   │     ├─ T1.1 Storage growth model
   │     ├─ T1.2 Prometheus alerts (4)
   │     ├─ T1.3 Chaos drill manifest
   │     ├─ T1.4 OIDC trust policy
   │     └─ T1.5 TUF ceremony runbook
   │
   ├─▶ Phase 2 — SBOM-attestation linking (GAP-2) [PARALLEL with Phase 1]
   │     ├─ T2.1 cosignAttestTemplate in argo/
   │     ├─ T2.2 Wire attestation step into workflow_builder DAG
   │     └─ T2.3 Tests + verification
   │
   ├─▶ Phase 3 — Harbor mirror observability (GAP-3) [PARALLEL]
   │     └─ T3.1 Exit handler + Loki emission + Prometheus counter
   │
   ├─▶ Phase 4 — OTel sampling docs (GAP-4) [PARALLEL]
   │     └─ T4.1 docs/operations/build-pipeline-otel.md
   │
   └─▶ Phase 5 — Hermetic tests + cross-validation
         └─ T5.1 Render tests + go test ./api/internal/argo/...
   │
   ▼
Phase 6 — Dogfood QA (operator LIVE — HARD STOP)
```

**Parallel opportunities:** Phases 1-4 são independentes — qualquer ordem ou paralelos.
**Sequencing:** Phase 5 depende de Phases 1-4. Phase 6 depende de Phase 5.

---

## Phase 1: Cosign keyless engineering deliverables (GAP-1)

**Objective:** Complete remaining 5 sub-deliverables do ADR-2026-05-27-T2.2 § "Engineering deliverables".

### T1.1 — Storage growth model + PV alerts

#### Objective
Documentar Rekor growth rate + criar Prometheus alert at 80% PV usage.

#### Evidence
PDR critical insight: "$30-50/mo estimate is build-cluster compute only; storage not modeled" + SRE absolute condition.

#### Files to edit
```
docs/operations/sigstore-storage-model.md (NEW) — growth model + retention policy
infra/helm/admission-policies/templates/sigstore-storage-alerts.yaml (NEW) — PV alert at 80%
```

#### Deep file dependency analysis
- `docs/operations/sigstore-storage-model.md` — new file, references Sigstore component Helm release defaults from `sigstore.ts:HELM_VERSION = "0.7.16"`
- `infra/helm/admission-policies/templates/sigstore-storage-alerts.yaml` — new template; appended to existing alert chart (alongside `policy-controller-alerts.yaml`)

#### Deep Dives
- **Growth model formula:** Rekor entry ~1KB per signed image. At 20k tenants × 5 builds/tenant/day × 2 entries (sig + attestation) = 200k entries/day = 200MB/day = 70GB/year. Add buffer 2x = **140GB/year PV size**.
- **Retention:** Append-only, no native compaction. Archive entries >365d to S3 + delete locally (preserves transparency log + frees PV).
- **Alert thresholds:** 80% PV → warning; 95% → critical (page operator).
- **Compaction script:** Bash script in `infra/scripts/security/rekor-archive-old-entries.sh` (~50 LOC).

#### Tasks
1. Write `sigstore-storage-model.md` with formula, retention strategy, compaction script reference
2. Create `sigstore-storage-alerts.yaml` PrometheusRule with 2 alerts (80% warn, 95% critical)
3. Create compaction script stub (operator implements details post-activation)
4. Hermetic render test

#### TDD
```
RED:     test_sigstore_storage_alerts_chart_renders() — helm template + kubeconform
RED:     test_alerts_have_thresholds_80_95() — yq query confirms exact pct
RED:     test_compaction_script_exists() — file exists + executable
GREEN:   Implement files
REFACTOR: None expected
VERIFY:  helm template infra/helm/admission-policies | yq 'select(.kind == "PrometheusRule")'
```

#### Acceptance Criteria
- [ ] Storage model doc has explicit calculation for 20k tenants
- [ ] 2 Prometheus alerts render (warn + critical)
- [ ] Compaction script stub + reference documented
- [ ] Pass: kubeconform --strict
- [ ] Pass: helm lint

#### DoD
- [ ] 2 new files + 1 modified template committed
- [ ] Hermetic render test passes

---

### T1.2 — 4 Prometheus alerts for Sigstore stack

#### Objective
Wire `loopzero_cosign_sign_failure_total`, `fulcio_cert_issuance_duration`, `rekor_log_append_failure`, TUF root expiry countdown.

#### Evidence
SRE absolute condition: "4 Prometheus alerts wired... severity=critical+runbook link"

#### Files to edit
```
infra/helm/admission-policies/templates/sigstore-alerts.yaml (NEW) — 4 alerts
```

#### Deep file dependency analysis
- New template in existing chart (`infra/helm/admission-policies/`), gated by `.Values.alerts.sigstoreEnabled` flag (default false, operator flips post-Sigstore activation)

#### Deep Dives
- **Alert 1: `LoopZeroCosignSignFailures`** — `rate(loopzero_cosign_sign_failure_total[5m]) > 0` for 5m → critical
- **Alert 2: `LoopZeroFulcioCertIssuanceSlow`** — `histogram_quantile(0.99, fulcio_cert_issuance_duration_seconds) > 30` for 10m → warning
- **Alert 3: `LoopZeroRekorLogAppendFailure`** — `rate(rekor_log_append_failure_total[5m]) > 0` for 2m → critical
- **Alert 4: `LoopZeroTufRootExpirySoon`** — `(tuf_root_expiry_timestamp - time()) / 86400 < 30` → warning; <7 → critical. **Requires LoopZero-emitted metric reading TUF ceremony date from outputs.tufCeremonyDate Pulumi output.**

#### Tasks
1. Create alerts template gated by sigstoreEnabled flag
2. Add corresponding `loopzero_*` counter declarations in `pkg/metrics/sigstore.go` (NEW)
3. Wire counters into existing cosign + rekor code paths (deferred to operator activation — counters exist as no-ops until Sigstore live)
4. Hermetic test

#### TDD
```
RED:     test_4_alerts_present_in_render() — yq query for alert names
RED:     test_alerts_have_severity_labels() — critical OR warning per alert
RED:     test_alerts_have_runbook_url_annotation() — points at sigstore-alerts-runbook
GREEN:   Implement
REFACTOR: None
VERIFY:  helm template | grep "alert:" | wc -l == 4 (in sigstore-alerts.yaml block)
```

#### Acceptance Criteria
- [ ] 4 alerts render
- [ ] All have severity + runbook URL annotations
- [ ] Pass: kubeconform
- [ ] Pass: helm lint

#### DoD
- [ ] Template committed
- [ ] Counter declarations exist (no-op until Sigstore live)

---

### T1.3 — Chaos drill manifest `cosign-fulcio-kill`

#### Objective
Validate Fulcio outage handling: build pipeline must fail CLEANLY (typed error, no silent unsigned push).

#### Evidence
SRE absolute condition: "chaos drill killing Fulcio mid-build BEFORE deadline (validate exit-code 2 + retry)"

#### Files to edit
```
infra/tests/chaos/manifests/cosign-fulcio-kill.yaml (NEW) — chaos-mesh PodChaos manifest
```

#### Deep file dependency analysis
- New manifest alongside existing chaos manifests (`build-cluster-network-partition.yaml`, `pg-network-latency.yaml`, etc.)
- Pattern mirrors existing chaos manifests

#### Deep Dives
- **Scenario:** Kill Fulcio pod during active build → cosign-sign step fails → BuildKit Workflow exits non-zero → API marks build FAILED → no image pushed to DOCR
- **Hypothesis:** "Fulcio unreachable during build SHOULD fail cleanly (typed error CHAIN_BROKEN) NOT silent unsigned push"
- **SLO query:** `loopzero_cosign_sign_failure_total{reason="fulcio_unreachable"}` > 0 during chaos window
- **Cleanup:** Fulcio replicas auto-restored (PDB minAvailable=1 prevents accidental kill of all)

#### Tasks
1. Create manifest with annotations (hypothesis, slo-query, dod-test) matching existing pattern
2. Cleanup section + finalizers respected
3. Document in chaos-test SKILL.md scenarios list

#### TDD
```
RED:     test_chaos_manifest_has_annotations() — loopzero.io/hypothesis + slo-query + dod-test
RED:     test_chaos_targets_fulcio_pods_only() — labelSelector scoped to fulcio
GREEN:   Create manifest
VERIFY:  kubeconform --strict
```

#### Acceptance Criteria
- [ ] Manifest has 3 required annotations
- [ ] Cleanup defined
- [ ] Pass: kubeconform

#### DoD
- [ ] Manifest committed
- [ ] chaos-test SKILL.md scenarios list updated

---

### T1.4 — OIDC issuer trust policy in ClusterImagePolicy keyless variant

#### Objective
Pin issuer + subject claim in `loopzero-images-keyless` ClusterImagePolicy — reject any other signer.

#### Evidence
PSec condition: "OIDC issuer trust policy in ClusterImagePolicy: pin issuer + subject claim (workload SA identity), reject any other signer."

#### Files to edit
```
charts/loopzero-admission-policies/templates/cluster-image-policy.yaml — extend identityRegexp scope
infra/helm/admission-policies/templates/cluster-image-policy.yaml — mirror (dual chart system)
infra/helm/admission-policies/values.yaml — update clusterImagePolicyKeyless.identityRegexp
charts/loopzero-admission-policies/values.yaml — mirror
```

#### Deep file dependency analysis
- Existing ClusterImagePolicy template already has `identities[0].issuer` + `identities[0].subjectRegExp` — current value `^https://github\\.com/getloopscale/loopzero/` is GitHub Actions issuer (stale post-GHA removal)
- Update to LoopZero build cluster workload identity (post-Sigstore activation): `https://kubernetes.default.svc.cluster.local`
- subjectRegExp scopes to specific ServiceAccount: `^system:serviceaccount:argo:loopzero-build-sa$`

#### Deep Dives
- **OIDC issuer** for workload identity flow: Kubernetes API server's OIDC discovery URL — typically `https://kubernetes.default.svc.cluster.local` OR cluster-specific (DOKS provides per-cluster OIDC config)
- **Subject regexp:** Scopes to specific SA: `system:serviceaccount:argo:loopzero-build-sa` (loopzero-build-sa is the SA used by build pipeline workflows)
- **Defense in depth:** Even if attacker compromises Fulcio (mints cert), the cert subject MUST match this regex to be accepted by ClusterImagePolicy. Attacker would need to also forge K8s OIDC tokens.

#### Tasks
1. Update both chart system templates with operator-provided issuer + subject regex
2. Add values.yaml flags `clusterImagePolicyKeyless.oidcIssuerURL` + `clusterImagePolicyKeyless.subjectRegExp`
3. Document operator runbook step (post-Sigstore activation): grab cluster OIDC URL from kube-public/cluster-info
4. Render tests

#### TDD
```
RED:     test_keyless_policy_has_subject_regexp() — yq query for subjectRegExp present
RED:     test_dual_chart_systems_match() — both files have same values
RED:     test_default_subject_regex_scopes_to_loopzero_build_sa() — string match
GREEN:   Update templates + values
REFACTOR: None
VERIFY:  helm template + diff between chart systems
```

#### Acceptance Criteria
- [ ] Both chart systems updated identically (CLAUDE.md Rule 12 dual chart parity)
- [ ] Subject regex scopes to loopzero-build-sa
- [ ] Pass: `task quality:gates` (includes helm-overlap-parity)

#### DoD
- [ ] 4 files updated + parity gate PASS
- [ ] Operator runbook documents post-activation step

---

### T1.5 — TUF ceremony runbook

#### Objective
Operator can execute offline TUF ceremony without code-diving or asking engineers.

#### Evidence
ADR-2026-05-27-T2.2-OFFLINE-TUF § "Engineering deliverables #9": "Offline TUF ceremony runbook — `docs/operations/tuf-root-ceremony.md` (multi-party + hardware-backed + air-gapped procedure)."

#### Files to edit
```
docs/operations/tuf-root-ceremony.md (NEW)
```

#### Deep Dives
- **Procedure sections:**
  1. Preparation — assemble multi-party (3-of-5 quorum), confirm YubiKey FIPS hardware available, schedule ceremony room
  2. Air-gapped machine setup — clean Linux laptop, no network ever connected, full disk encryption
  3. Key generation — `tuf-cli` (sigstore/cosign root keygen), one party at a time, public key extraction
  4. Multi-party signing — each party signs root.json with their YubiKey
  5. Output sealing — root.json + signing keys to encrypted USB → kubectl create secret in sigstore-system ns
  6. Validation — `pulumi preview` accepts secret reference; `pulumi up` provisions Sigstore stack
  7. Rotation — annual recurrence at T+330d (calendared alert)

#### Tasks
1. Write runbook with all 7 sections
2. Reference Sigstore official multi-party ceremony guide (https://github.com/sigstore/root-signing)
3. Include checklist for each step
4. Document failure modes (party absent, hardware lost, etc.)

#### TDD
N/A — docs-only task. Validation via review.

#### Acceptance Criteria
- [ ] All 7 sections present
- [ ] Checklist actionable (operator can follow without help)
- [ ] References Sigstore canonical guide

#### DoD
- [ ] Doc committed
- [ ] Cross-referenced from ADR + roadmap

---

## Phase 2: SBOM-to-image attestation linking (GAP-2)

**Objective:** Cryptographically link SBOM to signed image via `cosign attest --predicate`.

### T2.1 — `cosignAttestTemplate()` Argo WorkflowTemplate step

#### Objective
New Workflow step generating SLSA attestation linking SBOM (from syft) to image digest (from buildkit) signed via cosign.

#### Evidence
M07 gap MEDIUM: "syft SBOM gerado independent; signature capturada independent; sem attestation file linking SBOM to signed image bytes. Compliance audits SLSA L3 podem requerer"

#### Files to edit
```
api/internal/argo/cosign_attest.go (NEW) — cosignAttestTemplate() function
api/internal/argo/cosign_attest_test.go (NEW) — unit tests
api/internal/argo/workflow_builder.go — add attestation step to DAG between syft + push
```

#### Deep file dependency analysis
- `cosign_attest.go` — new file, similar shape to `cosign.go`. Returns `wfv1.Template` for the attestation step.
- `workflow_builder.go` — `Build()` method composes DAG. Insert attestation step between `syft-scan-{app}` and `push-image-{app}` (attestation MUST happen on signed-not-yet-pushed image).
- Downstream effect: image lifecycle gains attestation bytes — `cosign verify-attestation` validates against image at deploy time (optional, future verification step).

#### Deep Dives
- **Template structure:** Container running cosign binary; pulls SBOM from S3 (using uri from syft step); calls `cosign attest --predicate /tmp/sbom.json --type cyclonedx --key /etc/cosign/key registry/path@sha256:digest`
- **Predicate type:** `https://cyclonedx.org/bom` (per ADR D1 — in-toto + SLSA standard)
- **Image reference:** Must be digest-pinned (from buildkit output)
- **Signing key:** Same as cosign-sign step (kms-key OR keyless post-Sigstore activation)
- **Output:** Rekor entry containing attestation + signature
- **Failure mode:** If attestation fails, build fails (signed image alone is INSUFFICIENT for SLSA L3 — must have linked attestation)

#### Tasks
1. Create `cosign_attest.go` with `cosignAttestTemplate(app AppSpec, prevStep string) wfv1.Template`
2. Add `attestImage(workflow_builder)` helper composing DAG step
3. Wire into `workflow_builder.go Build()` after syft + before push
4. Update `AppRelease` struct: add `ImageAttestationRekorURL` field (optional, for later verification)
5. Persist attestation Rekor entry URL in DB (`releases.image_attestation_rekor_url` — new column, forward-only migration)
6. Unit tests for template generation
7. Workflow_builder integration test confirming DAG dependency

#### TDD
```
RED:     test_cosign_attest_template_renders() — function returns valid wfv1.Template
RED:     test_attest_uses_cyclonedx_predicate_type() — args include --type cyclonedx
RED:     test_attest_references_image_digest() — uses {{tasks.build-image.outputs.parameters.digest}}
RED:     test_attest_uses_sbom_from_syft_step() — input artifact from syft S3 upload
RED:     test_workflow_builder_inserts_attest_step_after_syft() — DAG dep on syft
RED:     test_workflow_builder_inserts_attest_step_before_push() — push depends on attest
RED:     test_apprelease_has_attestation_rekor_url_field() — struct field exists
GREEN:   Implement cosign_attest.go + workflow_builder edit + migration
REFACTOR: None expected (mirror cosign.go pattern)
VERIFY:  cd api && go test ./internal/argo/... -run TestCosignAttest
```

#### Acceptance Criteria
- [ ] Function `cosignAttestTemplate()` exported
- [ ] Predicate type = `https://cyclonedx.org/bom` (D1 standard)
- [ ] DAG dependency: syft → attest → push (sequential)
- [ ] AppRelease.ImageAttestationRekorURL field added
- [ ] Forward-only migration adds DB column
- [ ] Pass: `cd api && go test ./internal/argo/`
- [ ] Pass: `cd api && go vet ./...`
- [ ] Pass: `task quality:gates`

#### DoD
- [ ] All RED tests written first
- [ ] All GREEN tests pass
- [ ] Zero new LoopZeroErrors leaked to user-facing output (Rule 3)
- [ ] DB migration is forward-only

---

### T2.2 — Wire attestation step into DAG composition

#### Objective
Existing `workflow_builder.go Build()` produces DAG with attestation step in correct position.

#### Files to edit
```
api/internal/argo/workflow_builder.go — add attestImage() invocation
api/internal/argo/workflow_builder_test.go — assert DAG dependency
```

#### Deep file dependency analysis
- `workflow_builder.go` — `Build()` method currently composes [clone, cache-restore, build-image-{app}, sign-image-{app}, syft-scan-{app}, push-image-{app}, ...]. Inserts: between syft + push.
- `workflow_builder_test.go` — 54 existing tests; new test asserting DAG ordering.

#### Tasks
1. Add `attestStep := attestImage(app, syftStep, signStep)` after syft
2. Wire push dependency: `pushStep.Dependencies = append(pushStep.Dependencies, attestStep.Name)`
3. New test: `TestWorkflowBuilder_AttestStepPositionInDAG`

#### TDD
(combined with T2.1)

#### Acceptance Criteria
- [ ] DAG dep test passes
- [ ] Existing workflow_builder_test.go 54 tests still pass

#### DoD
- [ ] No regression in existing tests

---

### T2.3 — Tests + verification stub

#### Objective
Hermetic test validates attestation template + future `cosign verify-attestation` command structure.

#### Files to edit
```
api/internal/argo/cosign_attest_verify_test.go (NEW) — verification command builder test
```

#### Tasks
1. Build verification command structure (used by future admission policy / `loopzero deploy --verify-attestation`)
2. Test produces correct cosign verify-attestation argv

#### TDD
```
RED:     test_verify_command_builds_correct_argv() — cosign verify-attestation --type cyclonedx --key X registry/image@sha256:digest
GREEN:   Implement helper function (not yet wired — admission policy uses post-T1.4)
```

#### Acceptance Criteria
- [ ] Helper function tested
- [ ] No regression

#### DoD
- [ ] Test PASS

---

## Phase 3: Harbor mirror observability (GAP-3)

**Objective:** Harbor mirror failures emit Loki log + Prometheus counter increment.

### T3.1 — Exit handler + Loki emission + Prometheus counter

#### Objective
`mirror-harbor-{app}` step failure produces visible signal without breaking Workflow.

#### Evidence
M07 gap LOW: "mirror-harbor-{app} continueOn.failed=true (workflow_builder.go:408,432). Falha silenciosa sem warning to Loki"

#### Files to edit
```
api/internal/argo/workflow_builder.go — add exit handler template + wire onExit per harbor step
api/internal/argo/workflow_builder_test.go — assert exit handler present
infra/helm/admission-policies/templates/sigstore-alerts.yaml — add Harbor mirror alert (or new file)
pkg/metrics/build_pipeline.go — add loopzero_harbor_mirror_failure_total counter
```

#### Deep file dependency analysis
- `workflow_builder.go` — current `mirror-harbor-{app}` step has `continueOn.failed=true`. Adds `onExit: harbor-mirror-failed-handler`. New template `harbor-mirror-failed-handler` runs only if step failed (exit code != 0): emits Loki structured log + increments Prometheus counter.
- `pkg/metrics/build_pipeline.go` — register new counter `loopzero_harbor_mirror_failure_total{app, tenant}`.

#### Deep Dives
- **Exit handler template:** Container running shell script that calls `loki-push-api` endpoint (Loki ingest URL via env) + increments counter via Pushgateway or directly via cgroup-isolated Prometheus client
- **Counter labels:** `app`, `tenant`, `harbor_endpoint` — bounded cardinality
- **Alert:** Fire if `rate(loopzero_harbor_mirror_failure_total[15m]) > 0.1` for 30m (10% failure rate sustained 30min → operator review)
- **Loki log shape:** structured JSON `{level: "warn", component: "build-pipeline", event: "harbor_mirror_failed", app, tenant, error_msg}`

#### Tasks
1. Add `harbor-mirror-failed-handler` template to workflow_builder
2. Wire `onExit: harbor-mirror-failed-handler` to each `mirror-harbor-{app}` step
3. Register counter in metrics package
4. Add Prometheus alert
5. Unit tests confirming exit handler attached
6. Update operator runbook with Loki query template

#### TDD
```
RED:     test_workflow_builder_attaches_exit_handler_to_harbor_step() — yq inspection
RED:     test_exit_handler_emits_loki_log_with_warn_level() — script content check
RED:     test_harbor_mirror_failure_counter_registered() — metric registry has counter
RED:     test_prometheus_alert_fires_at_10pct_rate() — alert spec check
GREEN:   Implement
REFACTOR: None
VERIFY:  cd api && go test ./internal/argo/... -run TestHarborMirror
```

#### Acceptance Criteria
- [ ] Exit handler template exists in generated Workflow
- [ ] Counter registered
- [ ] Alert renders
- [ ] Loki log emission script tested with mock endpoint

#### DoD
- [ ] All tests PASS
- [ ] Operator runbook updated with Loki query

---

## Phase 4: OTel sampling docs (GAP-4)

**Objective:** Build pipeline OTel sampling explicit documented.

### T4.1 — `docs/operations/build-pipeline-otel.md`

#### Objective
Operator understands sampling policy + escape hatch + cost implications.

#### Files to edit
```
docs/operations/build-pipeline-otel.md (NEW)
infra/helm/argo-workflows/values.yaml — confirm OTel config exists (read-only audit)
```

#### Tasks
1. Document sampling policy (D3 — 10% head + 100% errors)
2. Escape hatch: `LOOPZERO_BUILD_FORCE_TRACE=true` env var
3. Cost implications: ~$X/mo at 100 builds/day vs 100% sampling
4. Query template for "find trace for specific build" in Tempo

#### TDD
N/A — docs only.

#### Acceptance Criteria
- [ ] Sampling policy explicit
- [ ] Escape hatch documented
- [ ] Cost math present
- [ ] Tempo query template included

#### DoD
- [ ] Doc committed
- [ ] Cross-referenced from runbook + roadmap

---

## Phase 5: Hermetic tests + cross-validation

**Objective:** All previous engineering validated end-to-end.

### T5.1 — Full test suite + render validation

#### Objective
Pre-dogfood gate ensures all changes PASS hermetic validation.

#### Files to edit
```
N/A — only validation runs
```

#### Tasks
1. `cd api && go test ./internal/argo/...` — all tests PASS
2. `helm lint infra/helm/admission-policies` — zero warnings
3. `kubeconform --strict` on all rendered manifests
4. `task quality:gates` — full repo scan
5. `/cross-validation 2026-05-27-tenant-build-pipeline-gaps-closure` — zero CRITICO/GRAVE

#### TDD
N/A — meta-test phase.

#### Acceptance Criteria
- [ ] All Phase 1-4 tests pass
- [ ] Zero lint/render errors
- [ ] task quality:gates PASS
- [ ] cross-validation APROVADO

#### DoD
- [ ] All gates GREEN

---

## Phase 6: Dogfood QA (MANDATORY)

**Objective:** Validate engineering as operator would experience.

### Execution

```bash
# Pre-flight (operator)
task clean:all && task setup && tilt up
task doctor   # 41/0

# Validate tenant build pipeline still functional E2E
LOOPZERO_E2E=1 task e2e:journey
# Expected: ≥73/76 PASS (allowing 3-spec flake budget)

# Validate Phase 3 specifically: Harbor mirror exit handler
# (Manual: trigger build with Harbor unreachable; verify Loki log + counter increment)
```

### Acceptance Criteria

- [ ] Cleanroom dogfood ≥73/76 journey PASS
- [ ] Zero CRITICAL issues introduced by plan
- [ ] Harbor mirror manual chaos: Loki log emitted + counter visible em Prometheus
- [ ] Build callbacks contain new `ImageAttestationRekorURL` field populated (post-attestation success)

### If Dogfood Fails

1. Identify plan-caused vs pre-existing
2. Fix CRITICAL + HIGH from this plan
3. Re-run

---

## Coverage Matrix

| # | Gap / Requirement (M07 audit) | Task(s) | Resolution |
|---|---|---|---|
| 1.a | Storage growth model + PV alerts | T1.1 | Doc + 2 PrometheusRule alerts at 80%/95% |
| 1.b | 4 Prometheus alerts Sigstore stack | T1.2 | sign_failure + cert_issuance + log_append + TUF expiry |
| 1.c | Chaos drill cosign-fulcio-kill | T1.3 | chaos-mesh PodChaos manifest with hypothesis |
| 1.d | OIDC issuer trust policy | T1.4 | identityRegexp pinned to loopzero-build-sa workload identity |
| 1.e | TUF ceremony runbook | T1.5 | 7-section operator procedure |
| 2 | SBOM-to-image attestation linking | T2.1+T2.2+T2.3 | cosignAttestTemplate + DAG dep + verify helper |
| 3 | Harbor mirror failures silent | T3.1 | Exit handler + Loki log + Prometheus counter |
| 4 | OTel sampling not documented | T4.1 | docs/operations/build-pipeline-otel.md |

**Coverage: 8/8 gaps covered (100%)**

## Global Definition of Done

- [ ] All 5 phases (engineering) + Phase 6 (operator dogfood) completed
- [ ] All hermetic tests PASS (Go + helm lint + kubeconform)
- [ ] Zero `task quality:gates` regressions
- [ ] M07 re-audit (R11 of roadmap) confirms ≥3.7 maturity post-engineering
- [ ] **`/cross-validation 2026-05-27-tenant-build-pipeline-gaps-closure` APROVADO** — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASS** with zero CRITICAL/HIGH plan-caused
- [ ] Backward compatibility preserved (tenant builds work mid-transition AND post-activation)
- [ ] Cosign keyless engineering 100% ready; activation HARD STOP = operator runs ceremony + `pulumi up`

---

## Edge Case Review (inline)

### MUST FIX absorbed inline

**EC-1:** `cosign attest --predicate` against SBOM that doesn't exist (syft skip-on-missing) → attestation step fails → entire build fails.
- **Mitigation:** T2.1 attestStep MUST depend on syft step's actual output existing. Add precondition check: if syft skip-on-missing (no Trivy/cosign installed), skip attestation gracefully. **NOT a hard fail at build time.** Operator metric `loopzero_attestation_skipped_total` increments.

**EC-2:** Forward-only DB migration adding `image_attestation_rekor_url` column → rollback impossible.
- **Mitigation:** Per CLAUDE.md pre-release governance, forward-only migrations are policy. Column is nullable → existing releases without attestation continue to work.

**EC-3:** Counter `loopzero_harbor_mirror_failure_total` cardinality explosion at 20k tenants × N apps each.
- **Mitigation:** Bounded labels `app` + `tenant` already constrained (tenants finite per cell, apps per tenant ≤plan-cap). At max 20k × 5 apps = 100k series — within Prometheus default limits but watch on cell-2 ramp.

### SHOULD TEST

**EC-4:** TUF ceremony date drift if Pulumi component outputs.tufCeremonyDate not aligned with actual ceremony date.
- **Test:** Add hermetic test in Pulumi vitest asserting `tufPreMintedKeys.ceremonyDate` is parseable ISO-8601 (already required).

**EC-5:** Cosign attest signing key rotation mid-build → some images have old signature, new attestation.
- **Test:** Document operator runbook: signing key rotation MUST drain build queue before swap.

### DOCUMENT

**EC-6:** Sampling 10% may miss specific customer issue → escape hatch via env var.
- **Risk accepted:** Documented in T4.1 runbook.

**EC-7:** Harbor mirror exit handler runs even on Workflow CANCEL — operator may see spurious "failure" log.
- **Risk accepted:** Loki log labels include `workflow_phase=Cancelled` for filtering.

---

## References

- M07 audit § 7 Status Update 2026-05-27: `.claude/knowledge-base/audits/2026-05-26/07-build-pipeline.md`
- AS-IS snapshot: `docs/architecture/build-pipeline/tenant-build-pipeline-as-is.md`
- ADR-2026-05-27-T2.2-OFFLINE-TUF: `.claude/knowledge-base/decisions/2026-05-27-T2.2-offline-tuf-ceremony.md`
- MEET batch sponsor approvals: `.claude/knowledge-base/meetings/MEET-2026-05-27-batch-sponsor-approvals.md`
- Sigstore Pulumi component: `infra/pulumi-platform/src/services/sigstore.ts`
- Roadmap: `docs/strategy/roadmap-macro-2026-05-27.md` R1
- SLSA spec: https://slsa.dev/spec/v1.0/
- in-toto spec: https://in-toto.io/
- Sigstore multi-party root signing: https://github.com/sigstore/root-signing

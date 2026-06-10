# Plan: Tenant Build Pipeline — Gaps Closure (M07 → 3.9 ceiling)

> **Version 1.1** — Fecha 4 gaps actionable do M07 audit 2026-05-26 (Build Pipeline) por meio de engineering deliverables remaining: cosign keyless infrastructure activation (5 sub-deliverables), SBOM-to-image attestation linking (SLSA L3), Harbor mirror observability, OTel sampling docs. Outcome: M07 maturity sobe de 3.4 → ~3.9 (teto ADR D3 cap por M11 #25 string-aliases globally intractable). Critical-path real é GAP-1 (cosign keyless deadline 2026-08-10 ticking).

## Changelog

### v1.1 (2026-05-27, post-edge-case-review)

Absorbed 9 MUST FIX from external `/edge-case-plan` review (`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-edge-case-review.md`):

- **EC-NEW-1** T1.2: `fulcio_cert_issuance_duration` → `fulcio_cert_issuance_duration_seconds` (Prometheus naming convention)
- **EC-NEW-2** T1.2: New sub-task T1.2.b — emit `tuf_root_expiry_timestamp` via CronJob (no orphan metric)
- **EC-NEW-3** T1.3: `loopzero.io/prerequisite: sigstore-active` annotation + pre-flight check
- **EC-NEW-4** T1.4: OIDC URL must be **cluster-external** (multi-cluster verification works)
- **EC-NEW-5** T2.1: cosign-attest DAG dep on **BOTH** cosign-sign AND syft (SLSA L3 chain)
- **EC-NEW-6** T2.1: `activeDeadlineSeconds: 180` on attest step (Rekor latency bounded)
- **EC-NEW-7** T3.1: ADR D5 NEW — Pushgateway-with-TTL (resolves Pushgateway vs Prom client)
- **EC-NEW-8** T3.1: curl `--max-time 10 --connect-timeout 5` on Loki POST (no hang on outage)
- **EC-NEW-9** T4.1: ADR D3 rewritten — head-based 10% + `LOOPZERO_BUILD_FORCE_TRACE=true` escape hatch (no contradictory tail-based)

Engineering estimate revised: ~26h → ~30h (+2h for tuf_root_expiry emitter cronjob + ~1h test scaffolding for new TDD cases).

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

### D3 — OTel sampling policy = head-based 10% with FORCE_TRACE escape hatch (v1.1 — EC-NEW-9 fix)

**Decisão:** Head-based 10% sampling for build workflows. NO tail-based fallback (it's incompatible with head-based — once head decides drop, span never reaches collector). Operator re-runs build with `LOOPZERO_BUILD_FORCE_TRACE=true` env var to force 100% trace on next attempt (escape hatch for forensics).

**Rationale:** Original v1.0 said "head 10% + tail 100% on errors" — physically incompatible. Head sampling drops spans BEFORE error observable; tail requires OTel collector buffering ALL spans (expensive at scale). Picking head-based is industry standard for cost predictability + matches existing LoopZero Tempo setup (no buffering layer).

**Consequences:**
- ✅ 90% cost reduction on OTel storage (predictable)
- ✅ Cost linear with build volume — no surprise spikes during error storms
- ⚠️ 90% of errors at runtime are NOT traced — operator forensic flow: identify failing build → re-run with `LOOPZERO_BUILD_FORCE_TRACE=true` → debug from that trace
- ⚠️ Loss of forensic detail for transient/non-reproducible errors (rare; acceptable for build pipeline scope)

### D5 — Harbor mirror metric via Pushgateway with TTL (v1.1 — EC-NEW-7 fix)

**Decisão:** Exit handler PUSHes `loopzero_harbor_mirror_failure_total{app, tenant}` to existing Pushgateway with `Push-Lifetime: 24h` header. Pushgateway auto-expires entries after 24h. NOT direct Prometheus scrape (more complex setup) and NOT Pushgateway without TTL (stale metrics linger forever).

**Rationale:** v1.0 left "Pushgateway OR direct client" undecided. Pushgateway-with-TTL combines simplicity (push from anywhere) with hygiene (no stale metrics). 24h TTL gives Alertmanager + Grafana enough window to fire alert before metric expires; operator review window adequate.

**Consequences:**
- ✅ Simple ingestion path (curl POST from exit handler script)
- ✅ Stale metric problem eliminated by TTL
- ⚠️ Window for alert detection bounded at 24h — if operator OOO 2 days, metric gone before review. Mitigation: PrometheusRule alert fires within 15min of metric appearance — alerts persist in Alertmanager state independent of source metric.
- ⚠️ Requires Pushgateway `--web.enable-admin-api` flag enabled (already enabled per existing values)

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

### T1.2 — 4 Prometheus alerts for Sigstore stack (v1.1 EC-NEW-1 fix)

#### Objective
Wire `loopzero_cosign_sign_failure_total`, `fulcio_cert_issuance_duration_seconds` (CORRECT name per v1.1), `rekor_log_append_failure`, TUF root expiry countdown.

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
- **Alert 2: `LoopZeroFulcioCertIssuanceSlow`** — `histogram_quantile(0.99, rate(fulcio_cert_issuance_duration_seconds_bucket[5m])) > 30` for 10m → warning. **v1.1 metric name corrected: `fulcio_cert_issuance_duration_seconds` (was `fulcio_cert_issuance_duration` — missing `_seconds` suffix, Prometheus naming convention; original would NEVER match Fulcio's actual emitted metric → silent never-fires bug.)**
- **Alert 3: `LoopZeroRekorLogAppendFailure`** — `rate(rekor_log_append_failure_total[5m]) > 0` for 2m → critical
- **Alert 4: `LoopZeroTufRootExpirySoon`** — `(tuf_root_expiry_timestamp - time()) / 86400 < 30` → warning; <7 → critical. **v1.1 EC-NEW-2 fix:** metric emitter now defined in T1.2.b below (was "LoopZero-emitted" without owner = orphan metric anti-pattern).

#### Tasks
1. Create alerts template gated by sigstoreEnabled flag
2. Add corresponding `loopzero_*` counter declarations in `pkg/metrics/sigstore.go` (NEW)
3. Wire counters into existing cosign + rekor code paths (deferred to operator activation — counters exist as no-ops until Sigstore live)
4. Hermetic test verifying metric name correctness (Prometheus naming convention check)

#### TDD
```
RED:     test_4_alerts_present_in_render() — yq query for alert names
RED:     test_alerts_have_severity_labels() — critical OR warning per alert
RED:     test_alerts_have_runbook_url_annotation() — points at sigstore-alerts-runbook
RED:     test_fulcio_metric_name_has_seconds_suffix() — v1.1 EC-NEW-1 regression guard
GREEN:   Implement
REFACTOR: None
VERIFY:  helm template | grep "alert:" | wc -l == 4 (in sigstore-alerts.yaml block)
```

#### Acceptance Criteria
- [ ] 4 alerts render
- [ ] All have severity + runbook URL annotations
- [ ] Fulcio metric name includes `_seconds` suffix (regression guard test PASS)
- [ ] Pass: kubeconform
- [ ] Pass: helm lint

#### DoD
- [ ] Template committed
- [ ] Counter declarations exist (no-op until Sigstore live)

### T1.2.b — TUF root expiry metric emitter (v1.1 EC-NEW-2 NEW sub-task)

#### Objective
Emit `tuf_root_expiry_timestamp` Prometheus metric daily, reading ceremony date from Pulumi stack output. Without this, Alert 4 (LoopZeroTufRootExpirySoon) renders but never fires = orphan alert.

#### Evidence
EC-NEW-2 of edge-case-review: original v1.0 said "Requires LoopZero-emitted metric" but defined no emitter. "Implemented but not wired" pattern.

#### Files to edit
```
infra/helm/admission-policies/templates/tuf-expiry-cronjob.yaml (NEW) — daily CronJob
infra/helm/admission-policies/values.yaml — add tufRootCeremonyDate value (read from Pulumi outputs)
```

#### Deep file dependency analysis
- New CronJob template gated by same `sigstoreEnabled` flag as T1.2 alerts
- Reads `.Values.tufRootCeremonyDate` (operator sets via helmfile values override pointing at Pulumi stack output)
- Pushes metric via Pushgateway (same as D5 Harbor mirror pattern — keeps Pushgateway as single push-metrics ingestion point)

#### Deep Dives
- **CronJob spec:**
  - Schedule: `0 4 * * *` (daily 04:00 UTC; off-peak)
  - Container: `curlimages/curl` (~2MB image; already used by ci-status-script)
  - Script: compute expiry timestamp = ceremonyDate + 365 days; push to Pushgateway
- **Operator setup:** post-`pulumi up`, operator runs `pulumi stack output tufCeremonyDate` → updates Helm value via `helmfile -e prod sync --set tufRootCeremonyDate=<date>`
- **TTL on Pushgateway entry:** 48h (longer than 24h Harbor pattern — TUF expiry alerts are slower-cadence, less urgent)
- **Failure mode:** CronJob fails → metric goes stale → Pushgateway TTL expires → Alert 4 disappears → operator no warning. Mitigation: separate Prometheus alert `LoopZeroTufExpiryMetricMissing` if metric absent >72h.

#### Tasks
1. Create CronJob template
2. Add `tufRootCeremonyDate` value with default empty string (alert disabled if unset)
3. Script: compute expiry + push to Pushgateway with 48h TTL
4. Add stale-metric alert `LoopZeroTufExpiryMetricMissing`
5. Hermetic test

#### TDD
```
RED:     test_tuf_expiry_cronjob_renders_when_sigstore_enabled()
RED:     test_cronjob_disabled_when_ceremonyDate_empty()
RED:     test_pushgateway_url_uses_lifetime_header()
RED:     test_stale_metric_alert_fires_after_72h_absence()
GREEN:   Implement
VERIFY:  helm template + kubeconform
```

#### Acceptance Criteria
- [ ] CronJob renders when enabled
- [ ] Gracefully disabled if ceremonyDate unset (no orphan metric)
- [ ] Stale-metric alert present

#### DoD
- [ ] Template + values + alert committed
- [ ] Operator runbook documents post-pulumi-up step

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
- **v1.1 EC-NEW-3 fix — Prerequisite annotation:** Manifest includes `loopzero.io/prerequisite: "sigstore-active"`. chaos-test SKILL pre-flight check: `kubectl get pods -n sigstore-system -l app=fulcio --field-selector=status.phase=Running` — if returns 0 pods, SKIP manifest with explicit log "[chaos-test] SKIP cosign-fulcio-kill: prerequisite sigstore-active not satisfied (Fulcio not deployed)". Without this guard, manifest exists in repo + chaos-test runs against empty selector → "0 pods affected" → false positive PASS (no signal).

#### Tasks
1. Create manifest with annotations (hypothesis, slo-query, dod-test) matching existing pattern
2. **v1.1: Add `loopzero.io/prerequisite: "sigstore-active"` annotation**
3. **v1.1: Update chaos-test SKILL.md scenarios list with pre-flight check logic for prerequisite annotation**
4. Cleanup section + finalizers respected

#### TDD
```
RED:     test_chaos_manifest_has_annotations() — loopzero.io/hypothesis + slo-query + dod-test
RED:     test_chaos_targets_fulcio_pods_only() — labelSelector scoped to fulcio
RED:     test_chaos_manifest_has_prerequisite_annotation() — v1.1 EC-NEW-3
RED:     test_chaos_test_skill_respects_prerequisite_annotation() — SKILL.md doc contains skip logic
GREEN:   Create manifest + SKILL update
VERIFY:  kubeconform --strict
```

#### Acceptance Criteria
- [ ] Manifest has 4 required annotations (hypothesis + slo-query + dod-test + prerequisite)
- [ ] chaos-test SKILL.md documents prerequisite skip logic
- [ ] Cleanup defined
- [ ] Pass: kubeconform

#### DoD
- [ ] Manifest + SKILL update committed
- [ ] Test: skill correctly skips when prerequisite not satisfied

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
- Update to LoopZero BUILD cluster workload identity (post-Sigstore activation): **cluster-external OIDC discovery URL** (NOT `https://kubernetes.default.svc.cluster.local` intra-cluster — that breaks cross-cluster verification)
- subjectRegExp scopes to specific ServiceAccount: `^system:serviceaccount:argo:loopzero-build-sa$`

#### Deep Dives
- **v1.1 EC-NEW-4 fix — OIDC issuer URL must be cluster-external:** Workload clusters (cell-default, cell-1, cell-2) verify image signatures. They need to RESOLVE the issuer URL of the BUILD cluster (where signing occurred). Intra-cluster URL `https://kubernetes.default.svc.cluster.local` resolves ONLY within the issuing cluster — workload clusters can't reach it. Cluster-wide admission outage post-activation would result (ALL pods rejected because issuer unreachable).
  - **Correct URL source:** DOKS provides cluster-external OIDC URL via `kubectl get --raw /.well-known/openid-configuration | jq .issuer`. Format: `https://<cluster-id>.k8s.ondigitalocean.com` (DOKS-specific).
  - **Operator step (post-Sigstore activation):** extract BUILD cluster's external issuer URL: `kubectl --context=loopzero-build get --raw /.well-known/openid-configuration | jq -r .issuer` → pin this URL in BOTH chart systems via `clusterImagePolicyKeyless.oidcIssuerURL` Helm value.
  - **Multi-cluster aware:** Each cluster has distinct OIDC URL. Plan documents that operator MUST pin BUILD cluster's URL (not workload cluster's). Wrong cluster's URL = signature verification fail at admission.
- **Subject regexp:** Scopes to specific SA: `system:serviceaccount:argo:loopzero-build-sa` (loopzero-build-sa is the SA used by build pipeline workflows). Regex anchors `^...$` prevent prefix matching attacks.
- **Defense in depth:** Even if attacker compromises Fulcio (mints cert), the cert subject MUST match this regex to be accepted by ClusterImagePolicy. Attacker would need to also forge K8s OIDC tokens FROM THE BUILD CLUSTER specifically.

#### Tasks
1. Update both chart system templates with operator-provided issuer + subject regex
2. Add values.yaml flags `clusterImagePolicyKeyless.oidcIssuerURL` (cluster-external default empty — operator MUST set) + `clusterImagePolicyKeyless.subjectRegExp` (default `^system:serviceaccount:argo:loopzero-build-sa$`)
3. **v1.1: Document operator runbook step extracts BUILD cluster's external OIDC URL** (NOT workload cluster's). Procedure: `kubectl --context=loopzero-build get --raw /.well-known/openid-configuration | jq -r .issuer`
4. **v1.1: Default oidcIssuerURL=""** so chart REFUSES render if operator forgets to set (fail-fast) — Helm `required` template function
5. Render tests
6. **v1.1: SHOULD TEST — escape special chars in subjectRegExp** (EC-NEW-11)

#### TDD
```
RED:     test_keyless_policy_has_subject_regexp() — yq query for subjectRegExp present
RED:     test_dual_chart_systems_match() — both files have same values
RED:     test_default_subject_regex_scopes_to_loopzero_build_sa() — string match
RED:     test_subject_regexp_anchored_with_caret_and_dollar() — v1.1 EC-NEW-11 SHOULD TEST
RED:     test_chart_refuses_render_if_oidcIssuerURL_empty() — v1.1 EC-NEW-4 fail-fast
GREEN:   Update templates + values
REFACTOR: None
VERIFY:  helm template + diff between chart systems
```

#### Acceptance Criteria
- [ ] Both chart systems updated identically (CLAUDE.md Rule 12 dual chart parity)
- [ ] Subject regex scopes to loopzero-build-sa with ^...$ anchors
- [ ] Default oidcIssuerURL empty (fail-fast on operator misconfig)
- [ ] Pass: `task quality:gates` (includes helm-overlap-parity)

#### DoD
- [ ] 4 files updated + parity gate PASS
- [ ] Operator runbook documents BUILD-cluster-specific OIDC URL extraction

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
- **v1.1 EC-NEW-5 fix — DAG dependency on BOTH cosign-sign AND syft-scan:** `cosign attest` semantically associates attestation with EXISTING signature in Rekor. If cosign-sign hasn't run yet, attestation creates UNLINKED Rekor entry → SLSA L3 verification FAILS at downstream (no chain between sig + attestation). DAG dep MUST be `attestStep.Dependencies = []string{signStepName, syftStepName}` (not just syft).
- **v1.1 EC-NEW-6 fix — activeDeadlineSeconds: 180:** `cosign attest` typically completes <30s but Rekor append latency is unbounded (cross-witness + transparency log throughput). Without timeout, build hangs indefinitely if Rekor degraded → Workflow stuck → API doesn't release build slot → next build rejected 409. 3min matches existing cosign-sign timeout pattern.

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
RED:     test_workflow_builder_inserts_attest_step_after_BOTH_sign_AND_syft() — v1.1 EC-NEW-5 — DAG dep on BOTH
RED:     test_workflow_builder_inserts_attest_step_before_push() — push depends on attest
RED:     test_attest_step_has_activeDeadlineSeconds_180() — v1.1 EC-NEW-6 — Rekor latency bounded
RED:     test_attest_idempotent_for_same_digest() — v1.1 EC-NEW-12 SHOULD TEST — same image rebuild = same Rekor entry OK
RED:     test_apprelease_has_attestation_rekor_url_field() — struct field exists
GREEN:   Implement cosign_attest.go + workflow_builder edit + migration
REFACTOR: None expected (mirror cosign.go pattern)
VERIFY:  cd api && go test ./internal/argo/... -run TestCosignAttest
```

#### Acceptance Criteria
- [ ] Function `cosignAttestTemplate()` exported
- [ ] Predicate type = `https://cyclonedx.org/bom` (D1 standard)
- [ ] **DAG dependency: [cosign-sign + syft-scan] → attest → push** (sequential — v1.1 EC-NEW-5)
- [ ] **activeDeadlineSeconds: 180 set on attest step** (v1.1 EC-NEW-6)
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
- **Exit handler template:** Container running shell script that calls `loki-push-api` endpoint (Loki ingest URL via env) + pushes counter to **Pushgateway with 24h TTL** (per ADR D5 — v1.1 EC-NEW-7 resolved decision).
- **v1.1 EC-NEW-7 fix — Pushgateway-with-TTL:** Use `Push-Lifetime: 24h` header on POST to Pushgateway. Pushgateway auto-expires entries → no stale-metric pollution. Direct Prometheus scrape REJECTED (more complex setup for ephemeral pods).
- **v1.1 EC-NEW-8 fix — curl timeouts non-fatal:** Exit handler MUST use `curl --max-time 10 --connect-timeout 5`. Loki/Pushgateway unreachable = log to stdout (captured by pod's stderr → Argo Workflow logs) + exit 0 (non-fatal — Workflow already in best-effort path). Without timeout, exit handler hangs up to ~120s default → Workflow stuck in Cancelling state → API doesn't release build slot → next build 409.
- **Counter labels:** `app`, `tenant`, `harbor_endpoint` — bounded cardinality (~100k series max at 20k tenants × 5 apps)
- **Alert:** Fire if `rate(loopzero_harbor_mirror_failure_total[15m]) > 0.1` for 30m (10% failure rate sustained 30min → operator review). Alert persists in Alertmanager state even after Pushgateway TTL expires.
- **Loki log shape:** structured JSON `{level: "warn", component: "build-pipeline", event: "harbor_mirror_failed", app, tenant, workflow_phase, error_msg}`. `workflow_phase` label allows filtering (EC-7 DOCUMENT — spurious WARN on retried builds).

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
RED:     test_exit_handler_curl_has_max_time_10() — v1.1 EC-NEW-8 — no hang on Loki outage
RED:     test_exit_handler_pushes_with_24h_lifetime_header() — v1.1 EC-NEW-7 ADR D5 — TTL enforced
RED:     test_harbor_mirror_failure_counter_registered() — metric registry has counter
RED:     test_loki_log_includes_workflow_phase_label() — filtering for EC-7 spurious-warn-on-retry
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
1. **v1.1 EC-NEW-9: Document head-based 10% sampling ONLY** (no tail-based contradiction). 90% of errors NOT traced at runtime — forensic flow documented in escape hatch section.
2. Escape hatch: `LOOPZERO_BUILD_FORCE_TRACE=true` env var on `loopzero deploy` → next build records full trace at 100% rate.
3. Cost implications: ~10x reduction vs 100% sampling. At 100 builds/day = ~$5/mo Tempo storage vs ~$50/mo. Linear scaling, no spikes during error storms.
4. Query template for "find trace for specific build" in Tempo (queries by build_id label).
5. Operator forensic procedure documented: identify failing build → re-run with FORCE_TRACE → debug from new trace.

#### TDD
N/A — docs only.

#### Acceptance Criteria
- [ ] Sampling policy explicit head-based 10% (v1.1 EC-NEW-9 — no tail-based contradiction)
- [ ] Escape hatch `LOOPZERO_BUILD_FORCE_TRACE=true` documented with operator-runnable example
- [ ] Cost math present (10x reduction vs 100% sampling)
- [ ] Tempo query template included (filter by build_id label)
- [ ] Operator forensic procedure documented

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
| 1.b | 4 Prometheus alerts Sigstore stack | T1.2 | sign_failure + cert_issuance + log_append + TUF expiry (v1.1: metric name fixed) |
| 1.b' | TUF expiry metric emitter (v1.1 EC-NEW-2) | T1.2.b | Daily CronJob pushes ceremonyDate+365d to Pushgateway |
| 1.c | Chaos drill cosign-fulcio-kill (v1.1 prerequisite) | T1.3 | chaos-mesh PodChaos + loopzero.io/prerequisite annotation |
| 1.d | OIDC issuer trust policy (v1.1 cluster-external) | T1.4 | identityRegexp pinned to BUILD cluster external OIDC URL |
| 1.e | TUF ceremony runbook | T1.5 | 7-section operator procedure |
| 2 | SBOM-to-image attestation linking (v1.1 DAG fix) | T2.1+T2.2+T2.3 | cosignAttestTemplate + DAG dep on sign AND syft + activeDeadlineSeconds |
| 3 | Harbor mirror failures silent (v1.1 Pushgateway-TTL + timeouts) | T3.1 | Exit handler + curl --max-time + Pushgateway-with-TTL |
| 4 | OTel sampling not documented (v1.1 head-only) | T4.1 | docs/operations/build-pipeline-otel.md, head-based 10% + FORCE_TRACE |

**Coverage: 9/9 gaps covered (100%)** (was 8, added T1.2.b TUF emitter sub-task)

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

> **v1.1 status:** External `/edge-case-plan` review (`docs/plans/2026-05-27-tenant-build-pipeline-gaps-closure-edge-case-review.md`) identified 9 MUST FIX + 3 SHOULD TEST + 3 DOCUMENT beyond this inline section. All 9 MUST FIX **ABSORBED** into v1.1 (this file). SHOULD TEST + DOCUMENT items integrated below.

### MUST FIX absorbed inline (v1.0 — 3 items)

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

### DOCUMENT (v1.0 — 2 items)

**EC-6:** Sampling 10% may miss specific customer issue → escape hatch via env var.
- **Risk accepted:** Documented in T4.1 runbook.

**EC-7:** Harbor mirror exit handler runs even on Workflow CANCEL — operator may see spurious "failure" log.
- **Risk accepted:** Loki log labels include `workflow_phase=Cancelled` for filtering.

### v1.1 absorbed from external `/edge-case-plan` review

9 MUST FIX absorbed in tasks above (markers `v1.1 EC-NEW-N fix` in respective sections):
- EC-NEW-1 (T1.2): Fulcio metric name `_seconds` suffix
- EC-NEW-2 (T1.2.b NEW): TUF expiry metric emitter via CronJob
- EC-NEW-3 (T1.3): `loopzero.io/prerequisite: sigstore-active` annotation + pre-flight check
- EC-NEW-4 (T1.4): Cluster-external OIDC URL (multi-cluster verification)
- EC-NEW-5 (T2.1): cosign-attest DAG dep on BOTH sign AND syft
- EC-NEW-6 (T2.1): `activeDeadlineSeconds: 180` on attest step
- EC-NEW-7 (T3.1 ADR D5 NEW): Pushgateway-with-TTL decision
- EC-NEW-8 (T3.1): curl `--max-time 10 --connect-timeout 5` on Loki POST
- EC-NEW-9 (T4.1 ADR D3 rewrite): head-based 10% only + FORCE_TRACE escape

3 SHOULD TEST absorbed into respective task TDD sections:
- EC-NEW-10 (T1.1): `test_pv_resize_runbook_present`
- EC-NEW-11 (T1.4): `test_subject_regexp_anchored_with_caret_and_dollar`
- EC-NEW-12 (T2.1): `test_attest_idempotent_for_same_digest`

3 DOCUMENT items (risk accepted):
- EC-NEW-13: T1.3 chaos drill prerequisite documented in operator runbook
- EC-NEW-14: T2.1 build retries duplicate Rekor entries (acceptable, cosign verify handles)
- EC-NEW-15: T3.1 transient Harbor failures produce spurious WARN log filtered via `workflow_phase` label

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

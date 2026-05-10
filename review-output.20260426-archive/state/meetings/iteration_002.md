# Meeting Minutes — Phase 2, Iteration 1
**Date:** 2026-04-26
**Attendees:** chief-reviewer, completeness-auditor

---

## Status

- **Phase:** 2/7 (Completeness — Discovery and Mapping)
- **Iteration:** 2 (global iteration 2)
- **Findings:** 8 registered (2 high, 5 medium, 1 low)
- **Evidence:** 9 items attached
- **Previous iteration:** Phase 1 completed — 13 components mapped, 5 flows mapped, 11 risk hypotheses documented.

---

## Agent Reports

### Completeness Auditor

Completed full completeness audit of `operators/`. All 6 Phase 1 risk hypotheses targeted for Phase 2 were verified by direct code reading. 2 additional findings emerged during investigation.

**Hypothesis verification results:**

- **RH-001 (High):** CONFIRMED. `SetupPreviewController` is absent from `cmd/main.go:setupControllers()`. The function exists at `preview_controller.go:213` but is never called. No PreviewEnvironment CR will ever be reconciled.
- **RH-002 (Medium):** CONFIRMED. `var _ = statemachine.NewPreviewMachine` at line 29 is a blank identifier doing nothing at runtime. 4 direct phase assignments at lines 79, 133, 155, 169 bypass the state machine entirely.
- **RH-003 (Medium):** CONFIRMED. No `appsecret_controller.go` exists. AppSecret unregistered in scheme (`groupversion_info.go:35-36`). Additionally discovered: the `LoopZeroSecretSyncFailed` Prometheus alert fires on metric `loopzero_secret_sync_hash_mismatch_total` which is never emitted by any code — alert is permanently silent.
- **RH-004 (Low):** CONFIRMED. `ReconciliationReport` also unregistered in scheme (`groupversion_info.go:39`), no controller. CRD still deployed via Helm (`values.yaml: reconciliationreports.enabled=true`).
- **RH-006 (Medium):** CONFIRMED. Build namespace NetworkPolicy egress rules for ports 9000 (MinIO) and 6443 (K8s API) have no `To:` destination selector, allowing connections to any external IP.
- **DOKS exec-auth (Medium):** CONFIRMED. `clustercredential_controller.go:256-257` rejects exec-based auth. Comment at line 242-243 misleadingly says it handles exec-auth.

**Additional findings discovered:**

- **COMP-007 (Medium):** `httpclient/client.go:skipTLSVerify()` defaults to `true` (InsecureSkipVerify) unless explicitly overridden. Secure-by-default pattern is inverted.
- **COMP-008 (High):** GitHub PR webhook handler in `api/cmd/setup_github.go:173-182` is a no-op — logs event, returns nil, TODO comment. This is the API-side of the preview environment dead end (COMP-001 is the operator-side).

**Negative findings (good news):**
- Zero TODO/FIXME/HACK markers in non-test Go files.
- No swallowed errors in controller business logic.
- 7 active controllers all properly wired and have test files.
- Circuit breaker pattern consistently applied across all provisioners.

---

## Strategic Assessment

**What worked well:**
- Direct file reading confirmed all 6 Phase 1 hypotheses rapidly. The hypothesis-first approach saved time — we knew exactly what to look for.
- The COMP-003 finding (AppSecret + silent alert) emerged from following the AppSecret trail into the monitoring manifests. Cross-cutting investigation added real value beyond pure operator/ scope.
- The COMP-008 finding emerged from tracing the full preview flow end-to-end across operator/ AND api/ — the double dead end (both sides broken) is a more complete picture than either alone.

**What didn't work:**
- The first finding (COMP-001) was inserted with empty id because the JSON schema uses `id` not `finding_id`. Fixed via direct Python script.
- Initial grep for TODO/FIXME returned no results because the codebase has clean discipline on markers. This is actually a positive finding.

**Key insight:**
Preview environments have a double dead end: the operator controller (COMP-001) is written but unregistered, AND the API-side trigger (COMP-008) is a no-op TODO. Neither half works. CLAUDE.md accurately says "no production logic" but understates the situation — it should say "unregistered and untriggered." The Prometheus alert (COMP-003) monitoring AppSecretOperator is permanently silent because the AppSecretOperator does not exist. This creates false confidence in secret sync monitoring.

**Cross-cutting concerns:**
- COMP-005 (NetworkPolicy egress) and COMP-007 (TLS default insecure) are both cases of "insecure default with explicit opt-in for security." The codebase has a pattern of making the insecure mode the default for historical/compatibility reasons. This pattern should be reviewed holistically in Phase 6 (Security).

---

## Decisions

1. **All 8 Phase 2 completeness findings are validated.** Evidence attached for each. No false positives identified.

2. **RH-001 severity is confirmed as High.** "Controller skeleton exists" in CLAUDE.md understates the defect. The controller is complete (not a skeleton) but completely unregistered. User impact: preview feature is entirely non-functional end-to-end.

3. **COMP-003 (AppSecret + silent alert) is Medium, not Low.** The silent Prometheus alert creates false confidence in operational monitoring. Operators may believe secret sync failures are being detected when they are not.

4. **COMP-005 and COMP-007 are both correctly Medium.** COMP-005 has a clear exfiltration vector (port 9000/6443 egress to any destination). COMP-007 is mitigated in production by Pulumi config but the default is inverted.

5. **Advance to Phase 3 (Architecture Review).** Phase 2 completeness audit is complete. All hypotheses verified. No additional areas require re-examination. Coverage is comprehensive across the operators/ scope.

---

## Loop-Back Assessment

- **Should we loop back?** NO
- **Rationale:** All 6 Phase 2 hypothesis targets verified. 2 additional findings discovered. 8 findings total, all with evidence. Zero TODO/FIXME markers found (negative finding registered). The completeness picture is clear. Diminishing returns if we investigate further in Phase 2.
- **Confidence:** High. Direct code reading confirmed all findings. No speculation or inference.

---

## Task Assignments for Next Iteration (Phase 3 — Architecture Review)

- **Architecture Analyst:** Deep architecture review of operators/. Focus: (a) sequential step design in TenantController — cascade failure blast radius analysis; (b) multi-cluster client setup — are there scenarios where buildClient and runtimeClient point to wrong clusters?; (c) controller-runtime manager single-process design — leader election implications for preview rollout; (d) probe design in ExposureController — coupling operator health to application reachability; (e) state machine usage consistency across all 8 controllers (7 wired + 1 not).

- **Code Reviewer:** Begin systematic review of TenantController reconcile loop. Focus: (a) re-fetch before Status().Update — are all controllers doing this correctly?; (b) error handling patterns — are all provisioner errors properly wrapped with context?; (c) concurrent reconcile implications — MaxConcurrentReconciles=5 for TenantController and the deletion annotation race (RH-003).

---

## Next Meeting

- **Expected at:** Phase 3, Iteration 1 (global iteration 3)
- **Focus:** Architecture quality findings — coupling, cohesion, sequential cascade design, multi-cluster error handling.

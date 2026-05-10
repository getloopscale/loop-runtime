# Meeting Minutes — Phase 1, Iteration 1
**Date:** 2026-04-26
**Attendees:** chief-reviewer, architecture-analyst, code-reviewer (not yet briefed), completeness-auditor (not yet briefed), security-auditor (not yet briefed), infrastructure-reviewer (not yet briefed), data-reviewer (not yet briefed), flow-tracer, dependency-analyzer

---

## Status

- **Phase:** 1/7 (Baseline — Discovery and Mapping)
- **Iteration:** 1 (global iteration 1)
- **Findings:** 0 registered (Phase 1 is mapping only — findings start Phase 2)
- **Components registered:** 13
- **Flows registered:** 5
- **Risk hypotheses:** 11
- **Previous iteration:** None (first iteration)

---

## Agent Reports

### Architecture Analyst
Completed full controller and CRD inventory. Confirmed 8 controllers exist (7 wired, 1 not wired: PreviewController). 10 CRDs total: 7 with active controllers, 1 with unwired controller (PreviewEnvironment), 2 with no controller at all (AppSecret, ReconciliationReport). All 4 external provisioners have circuit breaker wrappers with consistent configuration (OpenThreshold=5, RetryInterval=120s). Multi-cluster topology clearly defined: mgmt/build/runtime clients separated at struct field level. Compiled binary `tenant-controller` (ELF 64-bit) committed to repository root — anomaly requiring investigation.

### Dependency Analyzer
Confirmed module uses Go 1.25.0 with toolchain go1.25.9. Clean DIP pattern: all 4 provisioners inject through interfaces. Shared pkg dependencies confirmed: statemachine (4 machines, 1 blank-imported), naming, circuitbreaker, logging. Notable absence: pkg/errors (LoopZeroError) is not used by any operator controller — all errors use fmt.Errorf wrapping. No pkg/health or pkg/jwt usage in operators.

### Flow Tracer
Traced 4 critical flows: (1) Tenant creation — 7 sequential top-level steps with 10 build-cluster sub-steps. (2) Application/Exposure chain — certificate readiness gates probe; ConsecutiveFailures tracking enables recovery. (3) Preview lifecycle — DEAD CODE confirmed: var _ = statemachine.NewPreviewMachine (blank identifier, SM never used), SetupPreviewController never called from main. (4) ClusterCredential rotation — Secret watch triggers immediate requeue; exec-based auth explicitly rejected (DOKS incompatibility risk).

### Code Reviewer
Not yet assigned (Phase 2+).

### Completeness Auditor
Not yet assigned (Phase 2).

### Security Auditor
Not yet assigned (Phase 6).

### Infrastructure Reviewer
Not yet assigned (Phase 5).

### Data Reviewer
Not yet assigned (Phase 5).

---

## Strategic Assessment

**What worked well:**
- Direct code reading of all 8 controller files provided unambiguous ground truth on wiring status.
- The multi-cluster architecture is clearly documented within the code itself (TenantReconciler struct comment at lines 50-57 in tenant_controller.go).
- Circuit breaker pattern is consistently applied across all provisioners.

**What didn't work:**
- Initial attempt to grep for pkg/* imports in controllers returned empty (import paths use module-qualified names, not relative paths).
- The dependency analyzer found no hits for pkg imports via grep — resolved by reading go.mod replace directive and checking source manually.

**Key insight:**
The most significant structural finding is that `SetupPreviewController` exists in the code but is never called from `main.go`. This means the entire preview environment feature — which the CLAUDE.md describes as "CRD defined, controller skeleton exists, no production logic" — is actually more accurately described as "controller exists but is completely unregistered with the manager and will never execute." The state machine blank-import (`var _ = statemachine.NewPreviewMachine`) is a code smell indicating the implementer knew the SM should be used but left it as a placeholder.

**Cross-cutting concerns:**
- Sequential reconcile step design in TenantController means any single external system failure cascades to skip all subsequent steps. Gitea downtime blocks ArgoCD provisioning. Under the circuit breaker (120s retry interval), a 2-minute Gitea outage would cause all in-progress Tenant provisions to fail their full reconcile cycle.
- The ExposureController performs live HTTP probes from the operator pod, which is an unusual design choice that couples operator health to application reachability.

---

## Decisions

1. **Advance to Phase 2 (Completeness).** Phase 1 mapping is complete. We have identified all 13 components, 5 flows, and 11 risk hypotheses. Sufficient baseline exists to drive targeted Phase 2 investigation.

2. **RH-001 (PreviewController not wired) is the highest-priority hypothesis** for Phase 2 completeness audit. Confirm no integration test validates the preview flow. Confirm the CLI `loopzero preview` command behavior when no controller reconciles the CR.

3. **RH-006 (Build namespace NetworkPolicy gaps) is the highest-priority security hypothesis** for Phase 6. The egress rule allowing unrestricted port 9000 and 6443 egress is a potential exfiltration vector.

4. **The compiled binary `tenant-controller` in the repo root is a Phase 5 infrastructure finding** to be investigated and raised as a finding (potential stale artifact, supply chain concern).

5. **Severity discipline:** RH-001 (PreviewController unwired) is High. RH-002, RH-004, RH-005, RH-006 are Medium. RH-003, RH-007, RH-008, RH-009, RH-010, RH-011 are Low. Will recalibrate as evidence accumulates.

---

## Loop-Back Assessment

- **Should we loop back?** NO
- **Rationale:** This is the first iteration of Phase 1. We have sufficient baseline to advance. No prior phases have findings to re-examine.

---

## Task Assignments for Next Iteration (Phase 2 — Completeness)

- **Completeness Auditor:** Map every public-facing feature claim in CLAUDE.md/README against actual code. Priority: (a) preview environments — is the CLI command wired to a non-functional API call?; (b) AppSecret lifecycle — what happens when an AppSecret CR is deleted?; (c) scale command stub (CLAUDE.md mentions "honest-stub pattern"); (d) dead code in controllers beyond PreviewController.

- **Code Reviewer:** Begin structural review of TenantController reconcile flow. Focus: (a) sequential steps — what is the failure blast radius?; (b) re-fetch patterns before Status().Update — are all controllers doing this correctly?; (c) state machine usage consistency across controllers.

- **Flow Tracer (Phase 2):** Trace CLI `loopzero preview` command through API to what happens when the PreviewEnvironment CR is created but no controller reconciles it. Document the user-visible failure mode.

---

## Next Meeting

- **Expected at:** Phase 2, Iteration 1 (global iteration 2)
- **Focus:** Completeness audit results — which features are truly inoperable vs. partially functional vs. undocumented stubs. Validate RH-001 and RH-002 with concrete code references.

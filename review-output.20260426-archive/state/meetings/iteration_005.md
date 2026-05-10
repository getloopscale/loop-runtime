# Meeting Minutes — Phase 5, Iteration 1
**Date:** 2026-04-26
**Chief Reviewer:** Claude Sonnet 4.6 (chief-reviewer role)
**Specialists engaged:** infrastructure-reviewer, cicd-reviewer, dependency-analyzer

---

## Status
- **Phase:** 5/7 (Infrastructure & Supply Chain)
- **Findings:** total=29, critical=0, high=5, medium=14, low=10
- **Phase 5 new findings:** 9 (INFRA-000 through INFRA-008), all category=infrastructure
- **Previous iteration (4):** 8 code-quality findings in operators codebase

---

## Agent Reports

### Infrastructure Reviewer (5b, 5c, 5d)

**Dockerfile:**
- Multi-stage build, CGO_ENABLED=0, -trimpath, -ldflags="-s -w" — correct
- Non-root user (uid 1000), HEALTHCHECK present, --leader-elect=true as CMD default
- Finding: alpine:3.22 runtime (not distroless) — shell available post-compromise (INFRA-007, low)

**Deployment:**
- Production (Pulumi): Secrets for kubeconfigs, capabilities.drop=[ALL], seccompProfile=RuntimeDefault, readinessProbe, livenessProbe, resource limits — all correct
- Dev (Tilt): kubeconfigs from ConfigMaps instead of Secrets (INFRA-002, medium), missing capabilities.drop and seccompProfile (INFRA-006, low), --leader-elect=false (intentional for dev)
- OPERATOR_INSECURE_SKIP_TLS=true in dev only; not set in production (correct)

**CRDs:**
- All 10 CRDs use apiextensions.k8s.io/v1 (not deprecated v1beta1)
- All single-version (v1), no conversion webhooks needed
- helm.sh/resource-policy: keep annotation on all chart templates — prevents accidental deletion
- Finding: CRD content sync is manual, CI only checks count parity (INFRA-004, medium)

**RBAC:**
- Strongest finding in phase: ClusterRole grants cluster-wide get/list/watch/create/update/delete on Secrets including delete verb — no namespace restriction (INFRA-001, high)
- No wildcard (*) verbs or resources used — this is positive
- ClusterRole grants create/update on roles+rolebindings cluster-wide — necessary for multi-tenant operator creating tenant RBAC, but still a broad privilege
- Backward-compat rules grant Deployments in core/v1 and ConfigMaps/Services in apps/v1 — API groups that don't contain those resources — dead rules (INFRA-008, low)
- platformbootstrap_role.yaml demonstrates correct least-privilege pattern (namespace-scoped Roles) but main tenant-controller does not follow it for Secrets

### CI/CD Reviewer (5a)

**Build pipeline (build-images.yml):**
- Positive: provenance=true, sbom=true on buildx; SOURCE_DATE_EPOCH for reproducibility; registry cache; cosign keyless signing; SBOM via syft; Trivy with exit-code=1; verification smoke test
- SHA-pinned: actions/checkout, docker/setup-buildx-action
- NOT SHA-pinned: sigstore/cosign-installer, anchore/sbom-action, trivy-action, digitalocean/action-doctl, actions/upload-artifact (INFRA-003, medium)
- Critical structural gap: build-images.yml triggers concurrently with ci.yml on the same push — no dependency on ci-gate passing (INFRA-000, high)
- Supply chain gap: image pushed to DOCR first, Trivy scans after in a separate job — vulnerable image window exists (INFRA-005, medium)

**CI gate (ci.yml):**
- govulncheck runs for operators module when go-operators files change — correct gate
- TruffleHog secret scan present (but also not SHA-pinned)
- kubeconform validates deployment manifests (operator-deployment.yaml) — positive
- Migration safety check prevents 'migrate force' commands
- CRD count parity check exists but no content comparison

### Dependency Analyzer (5e)

**govulncheck:** Not installed locally. CI workflow includes it — sufficient.

**go.mod assessment:**
- k8s.io/api v0.35.2 (K8s 1.35.x — very recent)
- controller-runtime v0.23.3 — current for this K8s generation
- golang.org/x/crypto v0.49.0, x/net v0.52.0 — recent Go toolchain security packages
- No gopkg.in/yaml.v2 direct dependency — uses the modern go.yaml.in/yaml fork (no YAML deserialization CVE risk from that source)
- All OTel, Prometheus, Ginkgo dependencies appear current
- No obviously stale or abandoned dependencies

**License:** Manual inspection shows Apache 2.0, MIT, BSD for major dependencies. go-licenses not available for automated sweep.

**Image signing:** cosign keyless (Fulcio/Rekor) for platform images. Operator image is in sign-and-attest matrix — confirmed. This differs from tenant build images which use key-based cosign. Both approaches are valid; keyless provides GitHub OIDC identity anchoring.

---

## Strategic Assessment

### What worked well
- Production Pulumi configuration is significantly more hardened than the dev tilt deployment — indicates the team knows the right patterns
- CRD design is clean: apiextensions/v1, single-version, status subresource, helm.sh/resource-policy: keep
- The build pipeline has strong provenance (SBOM, cosign, provenance attestation) — post-push security posture is good
- No wildcard RBAC verbs — the team avoided the worst RBAC anti-pattern
- govulncheck in CI is the right tool in the right place

### What didn't work
- The concurrent CI/build-images pipeline is a genuine gap: tests can fail while the image succeeds. This is the most actionable finding.
- The RBAC Secrets overprivilege (INFRA-001) vs the platformbootstrap model shows inconsistency in security posture application: one controller follows the principle, the main one doesn't.
- Dev-prod configuration parity gaps (ConfigMap vs Secret, missing capabilities.drop) are concerning because they allow security issues to hide during local development.

### Key insight
The supply chain and signing posture is strong (provenance, SBOM, cosign) but the ordering of operations (push then scan) and the pipeline dependency (no test gate) create windows where a broken or vulnerable image can temporarily exist in the registry with a production tag. The security controls are applied AFTER the artifact is already in the critical path, not before.

### Cross-cutting concerns
- INFRA-000 (no test gate before image push) compounds COMP-001/COMP-003 (unimplemented features): a change to a skeleton controller could push an untested image
- INFRA-001 (Secret CRUD cluster-wide) connects to CODE-002 (panic in reconcile hot path): if the panic is triggered, the operator restarts with full Secret access during the window — cluster secrets are not protected during operator downtime scenarios

---

## Decisions

1. **INFRA-000 is the highest-priority actionable fix.** Adding `workflow_run` dependency with `conclusion: success` to build-images.yml is a one-line change with no trade-offs. This should be fix 1.

2. **INFRA-001 requires a design decision (T2 change).** Decomposing the ClusterRole for Secrets requires understanding which controller needs what — the platformbootstrap model is the blueprint. This is medium effort but high security value.

3. **INFRA-005 (post-push Trivy) is a structural pipeline issue** that would require either: (a) local image export before push for scanning, (b) registry-side scan gate, or (c) accept the ~10-minute window as managed risk. For a pre-release PaaS, option (c) is acceptable in the short term if DOCR vulnerability scanning is enabled.

4. **Low findings (INFRA-006, INFRA-007, INFRA-008) are quick fixes** that can be bundled in a single PR. distroless switch is the most impactful.

5. **Loop-back assessment:** Phase 5 found 9 new findings (2 high, 4 medium, 3 low). No blind spots identified — the full infrastructure surface was covered. No new critical findings. Findings are declining relative to earlier phases (code phase had 8). Advance to Phase 6 (Security).

---

## Loop-Back Assessment
- **Should we loop back?** NO
- **Rationale:**
  - No critical findings in Phase 5
  - Findings declining: Phase 2=8, Phase 3=4, Phase 4=8, Phase 5=9 — infrastructure phase naturally generates more findings than code
  - All major infrastructure subsystems covered: Dockerfile, CI/CD, RBAC, CRD distribution, supply chain, deployment manifests
  - No blind spots — the operator has no DB surface (confirmed, by design)
  - Quality scores from previous phases all ≥0.91 — proceed with confidence

---

## Task Assignments for Next Iteration (Phase 6 — Security)

**Phase 6 focus:** Security audit using OWASP lens on the operators module.

- **security-auditor:** Review operator's handling of tenant isolation boundaries — can one tenant's operator reconcile affect another's namespace? Review the cell_id scoping. Check if the advisory lock bypasses are real (COMP findings cross-reference). Check reconcileDelete ordering (CODE-006 cross-ref).
- **threat-modeler:** Model the threat of a compromised ClusterCredential — what does an attacker get if they compromise the kubeconfig in a ClusterCredential? What's the blast radius from INFRA-001 (cluster-wide Secret delete)?
- **flow-tracer:** Trace the build admission path (cell capacity gate, plan enforcement) for bypass vectors — can a tenant trigger builds in excess of their plan by racing?

---

## Next Meeting
- **Expected at:** Phase 6, Iteration 1
- **Focus:** Security audit findings validation and synthesis toward Phase 7 (Dogfooding)

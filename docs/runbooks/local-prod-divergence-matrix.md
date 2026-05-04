# Local↔Prod Divergence Matrix

> **Plan T5.1 reference (per `pre-launch-gaps-plan.md`).** Canonical enumeration
> of every accepted divergence between local k3d and production DOKS environments.
> Rule 10 ("local equals production") aspires to zero divergence; this matrix
> documents the exceptions with closure plans.
>
> CI gate `scripts/check-divergence-matrix.sh` (T5.2) enforces that every
> code marker (e.g., `// LOCAL ONLY`, `# DEV path`) has a corresponding entry here.

## Status legend

- **OPEN** — accepted divergence with documented closure plan and horizon
- **CLOSED** — divergence resolved (kept here for audit history)
- **WONTFIX** — divergence permanent by design (with rationale)

## Matrix

| ID | Aspecto | Local | Produção | Reason | Closure plan | Owner | Horizon | Status |
|---|---|---|---|---|---|---|---|---|
| LP-001 | TLS cert issuance | cert-manager letsencrypt-prod (não emite `*.loopzero.local`) | CF-managed TLS via Cloudflare Tunnel | Sem DNS público para ACME challenge (k3d roda localhost) | `mkcert` para gerar wildcard local trust + cert-manager injeta como ClusterIssuer dev-only | infra-engineer | 2026-Q3 | OPEN |
| LP-002 | Cell-2 probe URL routing | Cell-2 escuta em portas 8091/8444 (k3d port mapping); DNS local resolve `*.loopzero.local` para 8090/8443 (workload cluster) | Cell-2 tem hostname próprio resolvido por CF Worker; routing transparente | k3d port allocation evita conflict com workload cluster | (a) `/etc/hosts` override per cell, OR (b) probe loop que escolhe porta via `loopzero admin tenants get --json` cellID lookup | infra-engineer | 2026-Q3 | OPEN |
| LP-003 | OIDC issuer (cosign keyless Track-2) | k3d service account tokens NÃO aceitos por public Fulcio (`fulcio.sigstore.dev`) | GitHub Actions OIDC tokens aceitos pelo Fulcio público (Track-1 GHA) | Public Fulcio policy whitelist | (a) Private Fulcio deployment, OR (b) Spire SVID bridge, OR (c) defer in-cluster Argo keyless | platform-security-engineer | Sprint 9 | OPEN |
| LP-004 | Build registry credentials | `buildkit-registry-config` Secret apontando para `k3d-loopzero-registry:5000` (HTTP) | DOCR (`registry.digitalocean.com`) com TLS + DO_TOKEN auth | Local k3d registry é HTTP-only por design | Production overrides via Pulumi (já implementado); no closure needed | infra-engineer | n/a | WONTFIX |
| LP-005 | PG max_connections | 50 (k3d default) | 200+ (DO managed PG) | k3d resource constraints | Pulumi DO PG config has 200; local intentional baseline | infra-engineer | n/a | WONTFIX |
| LP-006 | ArgoCD repo creds | http://gitea-http... (mgmt-internal HTTP URL) | Gitea via CF Tunnel (HTTPS public-facing) | Local: same-cluster routing | wire-clusters.sh handles both; closure not needed | infra-engineer | n/a | WONTFIX |
| LP-007 | Loki log streaming | Single mgmt cluster Loki collects all 3 clusters via Promtail with cross-cluster forward | Production has cross-cluster Loki streaming (DO Spaces backend) — partial | Cross-cluster Loki streaming infra gap (CLAUDE.md "Known issues") | Infra investment Sprint 10+ | observability-guardian | 2026-Q4 | OPEN |
| LP-008 | KEDA HTTP scale-to-zero | HTTP add-on installed; HTTPScaledObject CRs work | Same — both have KEDA HTTP add-on 0.8.0 | n/a — closed parity | n/a | n/a | n/a | CLOSED |

## Marker convention

When code introduces a local-only path, mark it with one of these comments:

- Go: `// LOCAL ONLY (LP-NNN): <brief>`
- Bash: `# LOCAL ONLY (LP-NNN): <brief>`
- TypeScript: `// LOCAL ONLY (LP-NNN): <brief>`
- YAML: `# LOCAL ONLY (LP-NNN): <brief>`

The `(LP-NNN)` ID MUST match an entry above. CI gate `check-divergence-matrix.sh`
fails if a marker references a non-existent ID OR if a code path is local-only
without any marker.

## Adding a new divergence

1. Identify the divergence (real or proposed).
2. Add row to matrix (next ID = MAX(existing) + 1).
3. Mark code path with `LOCAL ONLY (LP-NNN)` comment.
4. PR review: matrix entry must have **Owner + Horizon** filled (no `TBD`).
5. CI gate validates marker ↔ matrix integrity.

## Closing a divergence

1. Implement closure plan.
2. Remove `LOCAL ONLY (LP-NNN)` markers from code.
3. Update matrix row Status: `OPEN` → `CLOSED`.
4. CHANGELOG.md `[Unreleased]/Changed` entry.

## Why this matrix matters

Without the matrix:
- Each new divergence lives in a comment scattered across the codebase.
- "Why does local behave differently?" needs archaeology.
- Regression hides because dogfood masks "ah, local always does this".

With the matrix:
- Every divergence has Owner + Horizon (forces accountability).
- Closure plans force "intentional divergence" framing.
- CI gate prevents accidental drift (new local-only path without entry).

## References

- Plan `pre-launch-gaps-plan.md` ADR D5
- `scripts/check-divergence-matrix.sh` (T5.2)
- CLAUDE.md Rule 10 ("Local equals production")
- ADR-0021 (multi-cell normalization)
- ADR-0013-local-install-must-be-production-realistic.md

---
slug: m22-hermetic-remainder-batch2
generated_by: roadmap-feature
generated_at: 2026-07-10
status: completed
---

# M22 grill — hermetic-remainder-batch2

## Q1 — What is this feature and why NOW?

**Answer (2026-07-10):**

Continuar o débito hermetic rastreado no **M21 § body institutional backlog**: os 17 gates que são hermeticamente-executáveis (Wave A remainder 7 + Wave B 6 + Wave C 4) usando M20 T2.1 canonical shape (`cmd/loopzero-ops/internal/quality/devpublicbillingportalsyntax/check.go` shipped v0.63.0).

**Why now:**
- (a) M20 T2.1 canonical shape provou-se aplicável 2026-07-10 (6/6 hermetic tests GREEN sob -race + sanity real repo PASS);
- (b) M21 codificou o remainder como institutional backlog em vez de terminal-workaround, com specific shape family mappings;
- (c) Session dedicated com context budget suficiente pode shipar 8+ gates seguindo M16 precedent (mesmo dia shipou 10);
- (d) Fecha o KPI M20 (allowlist ≤ 100) hermeticamente sem sponsor JUNTOS + Cleanroom cluster.

## Q2 — Dependencies

**Answer:** **M20 [x] + M21 [x]** — chain consistency para audit trail explícito. M20 shipou o canonical shape que M22 reusa; M21 rastreou o institutional backlog. Ambos [x] desde 2026-07-10 v0.63.0. M22 elegível imediatamente.

## Q3 — DoD (6-bullet canonical M-Migrate2 batch2)

1. **17 gates Go shipados** em `cmd/loopzero-ops/internal/quality/<name>/` com hermetic tests GREEN sob `-race`.
2. **17 bash scripts DELETED** de `infra/tests/` (16 `check-*_test.sh` restantes + 1 `helm/ory-kratos-public-urls_test.sh`).
3. **Allowlist reduction:** `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 100** (net -17 from 117 baseline).
4. **`task quality:gates` + `go test ./...` GREEN** após cada lote; grep sweep 0 hits para cada script deletado.
5. **CHANGELOG discipline per lot:** `§ Added` Go gates + `§ Removed` bash + `§ Changed` taskfile wiring.
6. **`TestOracle_MatchesFrozenBaseline` re-frozen at M22 close** — closes the M20 blueprint ADR-D3 deferred baseline oracle re-freeze.

**Split-decision protocol (mirror M20 D2):** se velocity < 6 gates/sessão, promover residual a M23 via `/roadmap-feature` mid-milestone com ADR. Application canonical do velocity-split precedent estabelecido same-session 2026-07-10.

## Q4 — Top 2 NEW risks (não duplicar M20)

**R1 (MEDIUM): Wave C complexity + kubeconfig auth-mounts semantic drift.** Os 4 gates da Wave C (`scoped-kubeconfig-cold-bootstrap` + `scoped-kubeconfig-vault` + `seed-loopzero-secrets` + `helm/ory-kratos-public-urls`) têm complexidade domain-specific alta (K8s auth mounts + YAML round-trip + helm-golden test). M20 T2.1 shape (billing-portal-syntax) NÃO é direct match para essas complexidades — requer referenciar M16 T3.7 openbaoinitauth + T3.2 lokiretentionschema + T3.5 esochartsanity. **Mitigação:** per-gate shape family mapping explicit no plan (M22 blueprint), byte-identical parity capture bash-then-Go antes de `git rm` (mesmo pattern M-Migrate2 T2.0 baseline).

**R2 (LOW): TestOracle_MatchesFrozenBaseline re-freeze timing coordenação.** O re-freeze é stateful (produces committed artifact) e precisa acontecer exactamente at M22 close (final terminal step). Se re-freeze happens mid-M22, gates shipados subsequentemente vão falhar oracle test até próximo re-freeze. **Mitigação:** T5.X terminal step apenas, executed depois de todos 17 gates shipped + code-quality PASS.

## Grill status
completed

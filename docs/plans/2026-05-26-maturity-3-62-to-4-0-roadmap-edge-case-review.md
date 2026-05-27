# Edge Case Review — Maturity 3.62→4.0 Roadmap

**Data:** 2026-05-26
**Plano:** docs/plans/2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md (v1.0, 1942 linhas)
**Tasks analisadas:** 27 (T0.1 a T5.4 em 6 phases)
**Edge cases encontrados:** 19 (MUST FIX: 8, SHOULD TEST: 6, DOCUMENT: 5)

> **Veredicto:** **PLANO PRECISA DE AJUSTE** — 8 MUST FIX devem ser absorvidos como sub-tasks antes da implementação.

## Validações Realizadas (antes de classificar)

- `/architecture-docs` skill: **EXISTE** (`.claude/skills/architecture-docs/`) — EC inicialmente classificado como MUST FIX foi descartado
- `/audit-modular` skill: **EXISTE** (`.claude/skills/audit-modular/`) — idem
- `correlation.WithID`: **EXISTE em `pkg/correlation/context.go`** (NÃO em pkg/logging/context.go como T1.1 referencia)
- 8 reconciler files (`*_controller.go`) confirmados em `operators/internal/`

---

## MUST FIX

### EC-1: T1.1 — Caminho de referência incorreto para correlation package

- **Task afetada:** T1.1
- **Familia:** Format / Reference
- **Cenario:** Plano lista `pkg/logging/context.go — (Reference only) ContextLogger pattern` mas o pattern real é `correlation.WithID()` em `pkg/correlation/context.go`. Implementador pode procurar arquivo errado.
- **Impacto:** Confusão de import path; possibilidade de criar wrapper redundante em pkg/logging.
- **Fix sugerido:** Trocar linha em Files to edit:
  ```
  pkg/correlation/context.go  — (Reference only) correlation.WithID pattern
  ```

### EC-2: T2.2 — Fulcio + Rekor Helm chart source não pinned (múltiplos chart maintainers)

- **Task afetada:** T2.2
- **Familia:** Resource / Format
- **Cenario:** Plano cita "Fulcio Helm chart (community-maintained)" mas existem 3+ fontes: `sigstore/fulcio` oficial · `chainguard-dev/fulcio` · upstream tarball. Sem pinning, build não-reproducible.
- **Impacto:** Implementador pode escolher chart errado; future rebuilds podem fetchar versão diferente.
- **Fix sugerido:** Adicionar 1 linha em Deep Dives:
  ```
  Chart source: oci://ghcr.io/sigstore/charts/fulcio version X.Y.Z (pin no Pulumi)
  Chart source: oci://ghcr.io/sigstore/charts/rekor version X.Y.Z (pin no Pulumi)
  ```

### EC-3: T2.2 — Local k3d Fulcio TLS bootstrap não funciona com letsencrypt-prod

- **Task afetada:** T2.2
- **Familia:** Permission / Environment
- **Cenario:** CLAUDE.md Known Issues: "Local k3d cert-manager letsencrypt-prod cannot issue for `*.loopzero.local` (no public DNS for ACME challenge)". Fulcio + Rekor expostos externos precisam de TLS válido. Em local k3d, deploy nunca converge.
- **Impacto:** Phase 2 LIVE validation impossível localmente; bloqueia engineering loop sem drill stack.
- **Fix sugerido:** Adicionar acceptance criterion: `Local k3d deploys Fulcio/Rekor com self-signed CA (loopzero-local-ca-issuer); drill stack usa cert-manager prod issuer.` + ramo no values.yaml para issuer override por environment.

### EC-4: T2.3 — values.schema.json sem constraint `oneOf` para tag XOR digest

- **Task afetada:** T2.3
- **Familia:** Input / State
- **Cenario:** Plano adiciona campo `digest` opcional + template logic "prefer digest over tag if both present". Mas se BOTH são vazios, template renders `image: ""` → pod CrashLoopBackOff sem erro claro.
- **Impacto:** Operator deploy silencioso quebra; debugging difícil (sem mensagem clara).
- **Fix sugerido:** Em values.schema.json:
  ```json
  "oneOf": [
    {"required": ["tag"]},
    {"required": ["digest"]}
  ]
  ```

### EC-5: T2.3 — ClusterImagePolicy enforce-mode pode rejeitar pre-existing images sem digest

- **Task afetada:** T2.3
- **Familia:** Permission / Boundary
- **Cenario:** "ClusterImagePolicy: any policy-controller image not matching `@sha256:...` → reject" — mas admission-policies/monitoring/operator charts pre-existing podem usar `:latest` ou `:v1.2.3` (mutable tag). Restart de pod existing → rejeitado → CrashLoop.
- **Impacto:** Phase 2 deploy quebra ALL workloads, não só novos.
- **Fix sugerido:** Iniciar policy em `mode: warn` (NÃO enforce); flipar para enforce em separate task pós-soak; OR escopo `namespaceSelector: matchLabels loopzero.io/tenant=true` (só tenants).

### EC-6: T3.3 — ConfigMap path/key para "current cosign mode" não definido

- **Task afetada:** T3.3
- **Familia:** Format / Input
- **Cenario:** Script "lê current cosign mode from cluster ConfigMap" mas plano não nomeia namespace/name/key. Implementador inventaria; cliente do script pode escrever em path diferente.
- **Impacto:** Script lê valor errado; nunca bloqueia OU bloqueia erroneamente; ambos invalidam o gate.
- **Fix sugerido:** Adicionar em Deep Dives:
  ```
  ConfigMap: namespace=loopzero-system, name=loopzero-cosign-config, key=mode
  Valores válidos: "keyed" | "keyless"
  Owner: pulumi-platform (Fulcio component sets/updates)
  ```

### EC-7: T3.3 — Script sem emergency override flag

- **Task afetada:** T3.3
- **Familia:** Permission / Boundary
- **Cenario:** Se Phase 2 atrasa e ADR deadline (T0+90d) passa enquanto keyless infra ainda não está active, script bloqueia TODAS production builds. Sem override, recovery requer ADR amendment (slow).
- **Impacto:** Production builds bloqueados em emergência sem path para unblock.
- **Fix sugerido:** Adicionar flag `--emergency-override=<reason>` que: (a) emite WARN log, (b) escreve audit_log entry com reason+actor, (c) exit 0. Doc menciona "uso requer post-hoc Change Governor review".

### EC-8: T4.6 — Loki templating mechanism não definido

- **Task afetada:** T4.6
- **Familia:** Format / State
- **Cenario:** Plano diz "Templated Promtail push URLs per environment" mas não específica: helmfile gotmpl? values.gotmpl.yaml? Pulumi config? Sem decisão, implementador escolhe arbitrariamente.
- **Impacto:** Inconsistência com pattern existente (Promtail values já é .gotmpl-templated per commit `ef7a7c16` per CLAUDE.md).
- **Fix sugerido:** Adicionar em Deep Dives:
  ```
  Mechanism: extend infra/helm/promtail/values.gotmpl.yaml com env-conditional push URL
  Pattern já existe (cell-default → mgmt Loki internal LB) — extend para drill+prod
  ```

---

## SHOULD TEST

### EC-9: T0.3 — Verify AnalysisTemplate exists for cell-2 before runbook é considerado completo

- **Task afetada:** T0.3
- **Teste sugerido:** Acceptance criterion adicional: `kubectl get analysistemplate -n cell-2 returns at least 1 template AnalysisTemplate matching canary success_rate metric`. Runbook sem template de target = procedure unexecutable.

### EC-10: T1.2 — Charts overlap parity com prerelease semver

- **Task afetada:** T1.2
- **Teste sugerido:** `test_helm_overlap_parity_handles_prerelease_versions()` — assert `0.1.0-rc1 ≤ 0.1.0` semver comparison correct (rc < release per semver spec). golang.org/x/mod/semver handles this; explicit test cobre regression.

### EC-11: T2.4 — Conversão errors.New → LoopZeroError sem duplicate wrap em call chain

- **Task afetada:** T2.4
- **Teste sugerido:** `test_no_double_wrap_in_error_chain()` — converter middleware error sem checar se downstream já wrapped causa `WrapWithStack(WrapWithStack(...))` (stack frames duplicados em logs).

### EC-12: T2.6 — Test fixture count assertions quebram com novo register command

- **Task afetada:** T2.6
- **Teste sugerido:** Grep `cli/tests/` por `commands.length` ou `.toBe(N)` patterns; atualizar para N+1 ANTES de adicionar diagnostics register. Sem isso, suite test falha em runtime, não em compile.

### EC-13: T2.7 — Stripe-go SDK version pin explicit

- **Task afetada:** T2.7
- **Teste sugerido:** Acceptance criterion: `go.mod pin github.com/stripe/stripe-go/v82 (or current latest)`. Stripe v82+ tem breaking changes em BillingPortal API; sem pin, futuras updates de Renovate podem quebrar silently.

### EC-14: T4.5 — Release machine load ARCHIVED rows existentes do DB

- **Task afetada:** T4.5
- **Teste sugerido:** `test_release_machine_accepts_archived_state_from_db()` — migration 000008 já permite ARCHIVED em DB; quando state machine carrega row, deve ParseState("ARCHIVED") sem panic. Sem teste, mudança quebra release_archiver service silently.

---

## DOCUMENT

### EC-15: T0.2 — Sponsor decision timeout/fallback

- **Risco aceito:** Se T3 meeting com Sponsor + reviewers não converge em N semanas, Phase 0 fica indefinidamente bloqueado, cascateia para T2.5 + Phase 5. Documentar fallback: "Se decisão >4 semanas, default = self-hosted GitHub Actions runners (restoration of 2026-05-23 status quo); revisitar próximo trimestre."

### EC-16: T1.3 — withEnvironmentLock AST walker false-negative para helpers

- **Risco aceito:** Static test conta call sites diretos via Go AST. Se lock wrapeado em helper (e.g., `lockedHandler(h)`), AST não detecta — gate passa mas lock real ausente. Aceitável porque: (a) atual codebase não usa helpers, (b) PR review pega indireção. Documentar como known limitation.

### EC-17: T2.1 — Component tests vitest mocks não substituem `pulumi preview` LIVE

- **Risco aceito:** Vitest mocks @pulumi/automation API; podem divergir do runtime real. Coverage ≥ 50% é unit-level only. Manter `pulumi preview --stack drill` como integration gate em PRs que tocam pulumi-platform.

### EC-18: T2.5 — chart-testing matrix time budget

- **Risco aceito:** Full matrix (3 K8s versions × 13 charts × kind cluster install) = ~390 install cycles, estimativa 60min+. Limitar CI run scope: PR-time = lint only; full matrix = nightly OR pre-release weekly. Documentar no runbook.

### EC-19: T4.6 — Loki em mgmt cluster é SPOF para queries

- **Risco aceito:** Federation primary em mgmt = se mgmt down, query loss across clusters. Mas runtime log SHIPPING continua (Promtail buffers locally). HA Loki é separate scope; atual trade-off privilegia simplicidade. Documentar em concept page.

---

## Padroes Sistemicos Detectados

| Padrao | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Risk reduzido | T2.6 (doctor wire) — plano endereça |
| Correct code in wrong place | Não | — |
| Project name vs ID | Não relevante a este plano | — |
| ArgoCD notifiers not services | Não relevante | — |
| Single ArgoCD App per tenant | Não relevante | — |
| Internal terms in user-facing | Não | Plano não introduz CLI output novo (exceto T2.6 que reusa pattern) |
| Skill/dependency assumption sem grep-first | NÃO (validado upfront) | `/architecture-docs` + `/audit-modular` confirmados |
| Reference path errado em plan | Sim | T1.1 (pkg/logging vs pkg/correlation) — EC-1 |
| Pre-release breaking change sem mitigation | Sim | T2.3 (ClusterImagePolicy enforce) — EC-5 |

---

## Resumo

| Task | Edges | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------|----------|-------------|----------|
| T0.1 | 0 | 0 | 0 | 0 |
| T0.2 | 1 | 0 | 0 | 1 |
| T0.3 | 1 | 0 | 1 | 0 |
| T0.4 | 0 | 0 | 0 | 0 |
| T1.1 | 1 | 1 | 0 | 0 |
| T1.2 | 1 | 0 | 1 | 0 |
| T1.3 | 1 | 0 | 0 | 1 |
| T1.4 | 0 | 0 | 0 | 0 |
| T1.5 | 0 | 0 | 0 | 0 |
| T2.1 | 1 | 0 | 0 | 1 |
| T2.2 | 2 | 2 | 0 | 0 |
| T2.3 | 2 | 2 | 0 | 0 |
| T2.4 | 1 | 0 | 1 | 0 |
| T2.5 | 1 | 0 | 0 | 1 |
| T2.6 | 1 | 0 | 1 | 0 |
| T2.7 | 1 | 0 | 1 | 0 |
| T3.1 | 0 | 0 | 0 | 0 |
| T3.2 | 0 | 0 | 0 | 0 |
| T3.3 | 2 | 2 | 0 | 0 |
| T3.4 | 0 | 0 | 0 | 0 |
| T4.1 | 0 | 0 | 0 | 0 |
| T4.2 | 0 | 0 | 0 | 0 |
| T4.3 | 0 | 0 | 0 | 0 |
| T4.4 | 0 | 0 | 0 | 0 |
| T4.5 | 1 | 0 | 1 | 0 |
| T4.6 | 2 | 1 | 0 | 1 |
| T4.7 | 0 | 0 | 0 | 0 |
| T5.1-T5.4 | 0 | 0 | 0 | 0 |
| **TOTAL** | **19** | **8** | **6** | **5** |

**Veredicto:** PLANO PRECISA DE AJUSTE — 8 MUST FIX devem ser absorvidos antes da implementação.

## Próximos Passos para o Plano

1. **Aplicar 8 MUST FIX** como edits ao plano (cada um é 1-3 linhas; nenhum exige nova task — todos são clarificações dentro de tasks existentes).
2. **Aplicar 6 SHOULD TEST** como adições à seção TDD ou Acceptance Criteria das tasks afetadas.
3. **Aplicar 5 DOCUMENT** como notas/caveats em Deep Dives.
4. Bump plano v1.0 → v1.1 com referência a esta review.
5. Prosseguir para `/architecture-docs` Mode 3 (PROPOSED) per to-plan skill step 5.

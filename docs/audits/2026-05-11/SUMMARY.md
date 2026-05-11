# Auditoria Modular — LoopZero Platform — 2026-05-11

**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Scope:** 12 módulos top-level + cross-cutting
**Confidence range:** 88-92% (Claude-only, same-model-family residual; sem multi-modelo adversarial)
**Maturity cap:** 4.0 (per request)
**Methodology:** Cada claim com `file:line` evidência; Evidence Bar §1 (T1 commit+test, T2 LIVE log, T3 CI run); 3 evidências por módulo.

---

## 1. Scorecard Agregado

| # | Módulo | Score | Confidence | Bloqueio principal para 4.0 |
|---|---|---|---|---|
| 04 | **Shared Libraries (`pkg/`)** | **3.8 / 4.0** | 92% | 3 deprecated symbols + cobertura adversarial fraca em 3 pacotes |
| 11 | **State Machines** | **3.8 / 4.0** | 92% | String aliases sem compile-time safety; helpers inconsistentes |
| 09 | **Multi-Cell Routing** | **3.7 / 4.0** | 92% | Operacional: prod canary stages 4-5 + 4h soak + chaos pendentes |
| 01 | CLI | 3.5 / 4.0 | 92% | 7 E2E tests skipped (sem API real em CI) + platform installer mockado |
| 02 | API | 3.5 / 4.0 | 90% | 72× `errors.New()` anti-pattern + STUB em `scale.go:90` |
| 06 | Helm Charts | 3.5 / 4.0 | 90% | 4 gates advisory (drift, conftest, chartsnap, helm-diff) |
| 08 | Observability | 3.5 / 4.0 | 90% | Operator ↔ API correlation_id gap + Loki cross-cluster siloed |
| 10 | Auth & Billing | 3.5 / 4.0 | 88% | Session token rotation undocumented + auth audit_log gap |
| 12 | CI/CD & Testing | 3.5 / 4.0 | 90% | **`cell2-soaks` first GREEN + 1 prod customer day-trial pendentes** |
| 03 | Operators | 3.3 / 4.0 | 88% | **AppSecret CRD órfã + ReconciliationReport dead code** |
| 07 | Build Pipeline | 3.3 / 4.0 | 88% | Cosign keyless deferred + signing key rotation undocumented |
| 05 | Infrastructure as Code | 3.0 / 4.0 | 88% | **`pulumi-control-plane` STUB (16 TODOs) + 2 stacks sem testes** |

**Média ponderada (igual peso):** **3.49 / 4.0**

**Comparação:**
- `status-validation` plan v1.2 (2026-05-11, 3-persona adversarial — plan file removed in governance consolidation d18e3b89) LIVE-triangulou: **2.84** (under-claim de -0.65 nesta auditoria)
- Esta auditoria (modular, code-validated): **3.49**

A discrepância vs 2.84 do `status-validation` reflete que aquele plano usou 3 personas adversariais com weighting diferente (Core 3.0/Infra 2.8/Scale 2.9 + 4 dimensões pending). Esta auditoria é mais granular (12 módulos vs 7 dimensões) e tem cap em 4.0 (não 5.0).

---

## 2. Distribuição de Maturidade

```
4.0  ┃                                                             0 módulos
3.8  ┃ ████ pkg/, State Machines                                   2 módulos
3.7  ┃ ██ Multi-Cell                                               1 módulo
3.5  ┃ ████████████ CLI, API, Helm, Observ, Auth/Billing, CI/CD    6 módulos
3.3  ┃ ████ Operators, Build Pipeline                              2 módulos
3.0  ┃ ██ IaC                                                      1 módulo
<3.0 ┃                                                             0 módulos
```

**Padrão:** distribuição centrada em 3.5 (mediana = 3.5). Cluster superior (3.7-3.8) compartilha disciplina arquitetural exemplar (testes property-based, zero TODOs, panic invariants). Cluster inferior (3.0-3.3) compartilha gaps operacionais explícitos (STUB declarados, CRDs órfãs, supply chain incompleta).

---

## 3. Top Gaps Cross-Module (priorizados)

### HIGH (bloqueiam 4.0 ou expõem risco material)

1. **`pulumi-control-plane` STUB com 16 funções TODO declaradas** (M05) — loopzero-api, Tenant Controller, Ory Kratos/Hydra intencionalmente não-deployados via Pulumi. Evidência: `pulumi-control-plane/src/index.ts:16`.

2. **AppSecret CRD órfã** (M03) — CRD registrada em scheme sem reconcile loop. ESO funciona diretamente, mas lifecycle automation (rotation, drift detection) está ausente. Cross-cut com M09 (tenant pin pode orphan AppSecrets).

3. **ReconciliationReport dead code declarado** (M03) — `api/v1/reconciliationreport_types.go:33` comment: "no controller implements reconciliation for this type."

4. **Cosign keyless deferred sem Fulcio/Rekor LIVE** (M07) — code-ready mas inativo. Supply chain depende de long-lived signing keys.

5. **Signing key rotation undocumented** (M07) — secret estático mounted `/cosign/cosign.key` sem audit log, sem schedule, sem runbook.

6. **`cell2-soaks.yml` FIRST GREEN cron pendente** (M12) — **gate explícito para 4.0** per CLAUDE.md. Workflow deployed, aguardando primeira Sunday execution com artifacts.

7. **1 prod customer day-trial 24h pendente** (M12) — **gate explícito para 4.0** per CLAUDE.md. Manual process, requer operator action.

8. **Service-auth path sem binding user↔tenant criptográfico** (M10) — `api/internal/middleware/auth.go:224-254` relies on internal network trust. Compromised cloud → tenant escalation surface.

9. **2 projetos Pulumi sem testes** (M05) — `pulumi-platform/` (4,434 LOC, 0 .test.ts), `pulumi-control-plane/` (1,735 LOC, 0 .test.ts).

### MEDIUM (cumulativos)

10. **Operator ↔ API correlation_id propagation gap** (M08) — audit trail quebra quando operator atua em Tenant CR.

11. **Session token rotation procedure undocumented** (M10) — code support em `pkg/jwt/validator.go:206-243`, sem runbook operacional.

12. **Auth failures não persistem em audit_log table** (M10) — só logged em slog; forensic trail ausente.

13. **72× `errors.New()` fora de `pkg/errors`** (M02) — anti-pattern de consistência. Baixo impacto operacional mas drift de contrato.

14. **STUB confirmado em `routes/scale.go:90`** (M02) — config validada, **NÃO persistida**, retorna 202.

15. **Application Reaper bypass do helper conditions** (M03) — `application_reaper.go:89` append direto em vez de `conditions.SetCondition()`.

16. **Preview TTL com `time.Now()` duplo** (M03) — `preview_controller.go:164` flakiness latente.

17. **Worker.mjs hardcoded values** (M05) — `worker.mjs:1-2` S3 endpoint + base domain hardcoded em vez de Pulumi-injected.

18. **Loki cross-cluster siloed** (M08) — build cluster logs não chegam ao runtime Loki.

19. **Cardinality HIGH em `loopzero_domain_tls_expiry_days{domain,tenant}`** (M08) — N×M series.

20. **Mutable `:prod` tag risk** (M12) — ADR-0049 convention sem CI gate enforcement.

### LOW (debt cumulativo, não-crítico)

21. **4 gates advisory em helm-charts.yml** (M06) — drift, conftest, chartsnap, helm-diff aguardam graduation.

22. **7 E2E tests skipped no CLI** (M01) — `cli-e2e.test.ts` requer API real.

23. **3 deprecated symbols em pkg/logging + naming** (M04).

24. **Stripe price ID default-to-pro vulnerability** (M10) — `webhook.go:149-150`.

25. **State/Event como string aliases** (M11) — sem compile-time safety.

26. **ExposedRoute sem `Terminal: []State{}` explícito** (M11).

---

## 4. Padrões Cross-Module Observados

### Strengths consistentes (sustentam o 3.49 médio)

- **Test discipline alta:** CI 9 blocking gates (M06+M12), pkg/ 388 test funcs (M04), api/ 2,283 test funcs (M02), operators/ 228 Ginkgo blocks (M03), cli/ 1,226 test cases (M01), statemachine 117 funcs com 4 property-based (M11).
- **Lock discipline impecável** (M02): 0 ocorrências de `pg_advisory_lock` session-scoped; 100% `pg_advisory_xact_lock`. TOCTOU rigor em placement/multi-cell (M09).
- **State machine respect** (M02+M03+M11): 0 mutações diretas de status no path principal (verificado cross-module).
- **Strategy patterns elegantes:** CLI commands (M01), API services (M02), Build languages (M07: 9 strategies + UserDockerfileStrategy ortogonal).
- **Self-healing codified:** H1-H18 em scripts (M05) + H12+H13 codified em ClusterCredential controller (M09).
- **LIVE validation persistente:** 3/3 cells GREEN 12/12 ADR gates (M09); journey 76/76 + canary 80/80 + cleanroom 12/12 + ArgoCD resilience 83/83 (M12).
- **Disciplina arquitetural pkg/** (M04): zero TODOs, 1 única dep externa de produção (golang-jwt), property-based tests, ARCH-001 fix já consolidado.

### Weaknesses sistêmicas

- **CLAUDE.md numerical drift** (corrigido pós-auditoria): M02 (35 subpacotes), M06 (16 charts + 18 overlays), M08 (109 metrics + 8 cell metrics), M12 (35 workflows) — todos alinhados no CLAUDE.md 2026-05-11.
- **Supply chain operacional incompleta:** cosign keyless deferred (M07), signing key rotation undocumented (M07+M10), image verification deferred para admission (M07).
- **Correlation/audit gaps:** operator ↔ API correlation_id (M08), async queue context (M08), auth failures em audit_log (M10).
- **Configuração hardcoded em runtime artifacts:** worker.mjs (M05), staging stack stale (M05).
- **CRDs órfãs:** AppSecret + ReconciliationReport (M03).
- **Pulumi `control-plane` STUB:** 16 TODOs declarados (M05).
- **Document-as-code gap:** dashboards hand-edited (M08), runbooks operacionais ausentes (M07+M10).

---

## 5. Caminho Consolidado para 4.0

### Operacional (não requer engineering, requer scheduling/operator action)

1. **[GATE]** Habilitar e aguardar primeiro `cell2-soaks.yml` Sunday cron GREEN com artifacts publicados (M12)
2. **[GATE]** Identificar 1 prod customer + executar 24h day-trial sem incidentes (M12)
3. **[GATE]** Cell-2 production canary stages 4-5 (Sprint 9 readiness review, M09)
4. **[GATE]** 4h soak + Chaos Mesh + SLO P95<705ms (Operators Phase 6 Dogfood Gate, M03)

### Architectural (requer engineering)

5. **[T3 meeting]** Implementar `pulumi-control-plane` completo — 16 funções TODO (M05)
6. **[T2 meeting]** Decidir AppSecret controller — implementar OU remover CRD do scheme (M03)
7. **[T2 meeting]** Decidir ReconciliationReport — implementar (ADR-0031 drift detection) OU deletar (M03)
8. **[T2 meeting]** Cosign keyless cutover — provisionar Fulcio + Rekor + flipar `CosignKeyless=true` (M07)
9. **[T2 meeting]** Service-auth user↔tenant cryptographic binding (M10 G1)
10. **[T1]** Correlation_id propagation operator ↔ API + async queues + webhooks (M08)
11. **[T1]** Remediar 72× `errors.New()` → typed LoopZeroError (M02)
12. **[T1]** Fechar STUB em `scale.go:90` — persist scaling config (M02)
13. **[T1]** Loki cross-cluster federated query (infra gap, M08)

### Operational documentation (runbooks)

14. **[T1]** Session token rotation runbook (M10 G5)
15. **[T1]** Signing key rotation runbook + audit log (M07 + M10)
16. **[T1]** Atualizar CLAUDE.md para refletir realidades numéricas (cross-cut)

### Test debt cumulativo

17. **[T1]** Habilitar 7 E2E tests skipped em `cli-e2e.test.ts` contra staging (M01)
18. **[T1]** Adicionar test suite para `pulumi-platform/` + `pulumi-control-plane/` (M05)
19. **[T1]** Cross-tenant data leakage tests (M10 F2)
20. **[T1]** Auth failures em audit_log table (M10 G6)
21. **[T1]** Graduar 4 helm-charts gates de advisory para blocking (M06)

---

## 6. Recomendações por Prioridade

### Sprint 9 (path to 4.0)

- **Operational:** itens 1, 2 (gates 4.0)
- **Documentation:** itens 14, 15, 16
- **Test debt T1:** itens 19, 20

### Sprint 10 (post-4.0 hardening)

- **Architectural T2:** itens 6, 7, 8, 9
- **Architectural T1:** itens 10, 11, 12
- **Test debt T1:** itens 17, 18, 21

### Sprint 11+ (steady-state)

- **Architectural T3:** item 5 (pulumi-control-plane completo)
- **Architectural T1:** item 13 (Loki cross-cluster)

---

## 7. Anexo — Divergências CLAUDE.md vs Realidade

| Item documentado (à época da auditoria) | Realidade | Status |
|---|---|---|
| "api/internal/ ~25 subpacotes" | 35 subpacotes | ✅ Corrigido no CLAUDE.md |
| "32 bootstrap charts" | 16 charts + 18 value overlays | ✅ Corrigido no CLAUDE.md |
| "29 workflows" | 35 workflows | ✅ Corrigido no CLAUDE.md |
| "43+ loopzero_* metrics" | 105 metrics (including 6 cell metrics) | ✅ Corrigido no CLAUDE.md (105, não 109) |
| "Pulumi L1-only (zero K8s resources)" | 5 projetos Pulumi (1 stub) com 32 Helm releases em platform | ✅ Corrigido no CLAUDE.md |
| "Maturity 3.9" (docs/STATUS.md, removido) | 3.49 (modular) / 2.84 (status-validation 3-persona) | ✅ docs/STATUS.md removido; CLAUDE.md usa 3.49 |

---

## 8. Notas de Confiança

- **Confidence cap 92%** (Claude-only, single-model family residual per CLAUDE.md Rule 11 (evidence governance))
- **Code-validated:** todas as claims com `file:line` reference, grep counts via Bash, e Ginkgo/Go test counts diretos
- **NÃO multi-modelo adversarial** — para isto, consulte `status-validation` plan v1.2 que aplicou 3 personas
- **Esta auditoria é diagnostic** — não T2/T3. Remediações descobertas devem ser canalizadas via Change Governor.

---

## 9. Próximos Passos Sugeridos

1. **Operador humano:** revisar este SUMMARY + 12 docs de módulo (~15 min cada)
2. **Decidir prioridade:** itens 1-2 (gates 4.0 operacionais) ou itens 5-9 (arquitetural T2/T3 via Change Governor)
3. **Atualizar CLAUDE.md** para refletir realidade numérica (item 16)
4. **Atualizar CLAUDE.md** com este diagnostic (canonical source of truth)

---

**Audit completed:** 2026-05-11. 12 docs de módulo em `docs/audits/2026-05-11/01-cli.md` ... `12-cicd-testing.md`.

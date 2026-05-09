# Edge Case Review — deep-review-remediation

**Data:** 2026-05-09
**Plano:** `docs/plans/deep-review-remediation-plan.md`
**Tasks analisadas:** 62 (Phase 0–9)
**Edge cases encontrados:** 17 (MUST FIX: 5, SHOULD TEST: 8, DOCUMENT: 4)

**Veredicto:** **PLANO PRECISA DE AJUSTE** — 5 MUST FIX devem ser dobrados em sub-tasks antes da implementação começar. SHOULD TEST adiciona ~10 testes ao TDD existente sem mudar escopo.

---

## MUST FIX

### EC-1: T0.1 — Comportamento indefinido se DB de produção tiver linhas com phantom states

- **Task afetada:** T0.1 (Migration 000011)
- **Família:** State / Migration safety
- **Cenário:** Plano diz "Pre-migration audit verify zero phantom values; if rows exist, escalate to T0.1a + T0.1b". Mas não especifica o que `escalate` significa nem qual é o trigger automático. Se o validator query for executado manualmente como passo 1 das tasks e operador esquecer, migration roda direto e quebra com FK error genérico — pior que ter um gate explícito.
- **Impacto:** Migration aplica e a CHECK constraint rejeita rows existentes (PG ALTER TABLE valida CHECK contra dados existentes). Pode ficar pendurado em ACCESS EXCLUSIVE lock por minutos antes de erro 23514. Em prod, isso é incidente de DB lock + rollback caótico.
- **Fix sugerido:** Adicionar como passo 0 no T0.1: SQL guard no início do up.sql:
  ```sql
  DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM releases WHERE status IN ('BUILDING', 'FAILED')) THEN
      RAISE EXCEPTION 'Migration 000011 aborted: phantom states present in releases table. Run cleanup migration first.';
    END IF;
  END $$;
  ```
  Migration falha cedo, fast, e claro.

---

### EC-2: T0.3 — Convergence loop com cell-aware factory pode falhar polling sem detectar abort

- **Task afetada:** T0.3 (EmitRolloutAborted wire-up)
- **Família:** Multi-Cell / Resource
- **Cenário:** Convergence loop poll lê `Rollout.status.phase` via cell-aware K8s client. Se factory.Resolve retornar erro (kubeconfig stale, ClusterCredential not yet propagated, network blip), poll ignora silently e continua o loop. Mesma classe de regression do find_arch_007 sendo introduzida no convergence loop.
- **Impacto:** Bad-health canary nunca dispara `rollout.aborted` SSE. CLI fica pendurado indefinidamente até `progressDeadlineSeconds` ou timeout do cliente. Em prod, esse é o PIOR resultado possível — a feature parece funcionar (CLI conecta) mas nunca termina.
- **Fix sugerido:** Em `release_convergence_loop.go` no poll:
  ```go
  rollout, err := factory.GetRollout(ctx, releaseID)
  if err != nil {
      metrics.RolloutPollFactoryErrorTotal.WithLabelValues(reason(err)).Inc()
      // FAIL SAFE: after 2x progressDeadlineSeconds with poll errors, emit aborted with synthetic reason
      if time.Since(release.CreatedAt) > 2*time.Duration(release.ProgressDeadlineSeconds)*time.Second {
          EmitRolloutAborted(releaseID, "PollFactoryUnreachable", "Unknown")
      }
      continue
  }
  ```

---

### EC-3: T2.4 — IP rate limiter trustando X-Forwarded-For sem trust proxy config = bypass trivial

- **Task afetada:** T2.4 (Pre-auth IP rate limit)
- **Família:** Security / Boundary
- **Cenário:** Plano menciona "CDN-aware X-Forwarded-For trust config required" mas não especifica implementação. Se IPRateLimiter ler `r.Header.Get("X-Forwarded-For")` direto, atacante envia `X-Forwarded-For: 1.2.3.4` em cada request com IP rotativo → cada request entra em bucket diferente → rate limit de 120/min vira ilimitado.
- **Impacto:** Defense-in-depth pretendida (defesa pre-auth contra JWT brute-force) não existe. Atacante pode enviar 10k JWT-attempts/seg sem ser limitado, queimando JWKS validation cycles.
- **Fix sugerido:** Em `iprate.go`:
  ```go
  // Trust X-Forwarded-For ONLY when request comes from configured proxy IPs
  realIP := r.RemoteAddr
  if isFromTrustedProxy(r.RemoteAddr, cfg.TrustedProxyCIDRs) {
      if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
          realIP = strings.TrimSpace(strings.Split(xff, ",")[0])
      }
  }
  // key = realIP
  ```
  `cfg.TrustedProxyCIDRs` deve listar apenas Cloudflare egress ranges (publicados em https://www.cloudflare.com/ips/).

---

### EC-4: T1.4 — Removendo `if runtimeKubeconfig != ""` guard quebra deployments existentes mid-rollout

- **Task afetada:** T1.4 (app-operator unconditional cell-aware)
- **Família:** State / Deployment
- **Cenário:** Plano diz "REMOVE the if guard; require env var at startup; fail fast". Se Helm chart atual em produção (cell-default) NÃO monta o `loopzero-cluster-kubeconfigs` Secret, primeiro deploy do novo binary cras com error em loop. Em paralelismo: pods velhos terminam reconcile e morrem; pods novos entram em CrashLoopBackOff → controller fica down → reconciles param → deploys param.
- **Impacto:** Production-down até operador roll back ou aplica Helm chart mount. ~30min outage por cell durante upgrade. Mesma classe de regression do B34 (RUNTIME_KUBECONFIG mount drift).
- **Fix sugerido:** T1.4 deve ter SUB-TASK explícita ordenando:
  ```
  T1.4a — Helm chart update: mount loopzero-cluster-kubeconfigs Secret BEFORE T1.4b
  T1.4b — Code change: remove env guard, fail fast (only after T1.4a in production)
  T1.4c — Smoke test in staging: kill operator pod, verify it boots cleanly with mounted Secret
  ```
  E adicionar nota no DoD: "T1.4b applies AFTER T1.4a is in all production cells via Helm sync."

---

### EC-5: T8.5 — Default-deny-egress sem allow-list completa = self-DoS do LoopZero

- **Task afetada:** T8.5 (NetworkPolicy default-deny-egress)
- **Família:** Network / Boundary
- **Cenário:** Plano apenas diz "default-deny-egress + per-component explicit egress allow". Pods em `loopzero-system` precisam de egress para: kube-dns (UDP/53), kube-apiserver (TCP/443 ou ClusterIP), DOCR/Harbor pulls (TCP/443), Loki ingest (TCP/3100), Tempo OTel (TCP/4317), Hydra (TCP/4444), MinIO (TCP/9000), Postgres (TCP/5432), DO Spaces (TCP/443 external), GitHub API (TCP/443 external), CF API (TCP/443 external). Se policy aplicada antes da allow-list completa, ALL pods perdem DNS → todo o mgmt cluster cai.
- **Impacto:** Mgmt cluster outage até policy ser deletada. Pode levar minutos para diagnosticar (logs param, slog drop, sintomas confusos).
- **Fix sugerido:** T8.5 deve incluir:
  1. Inventory phase: `kubectl get pods -n loopzero-system -o json | jq '.items[].metadata.labels'` + map cada role → necessary egress endpoints.
  2. Apply policy with `policyTypes: [Egress]` ONLY após inventory + per-component allow rules em separate templates.
  3. Pre-apply dry-run: deploy em staging cell-1 primeiro, observe 1h, então cell-default. Não aplicar em todos cells de uma vez.
  4. Rollback procedure documentada: `kubectl delete networkpolicy loopzero-system-default-deny-egress -n loopzero-system`.

---

## SHOULD TEST

### EC-6: T0.5 — Invariant test deve rodar contra DB existente, não fresh PG

- **Task afetada:** T0.5 (CI invariant test)
- **Teste sugerido:** `TestStateMachineCheckInvariant_OnExistingDatabaseAfterAllMigrations` — aplica migrations sequencialmente (000001 → 000010 → 000011) em PG limpo, então roda invariant. Per memória `feedback_live_test_existing_db`: cells migration silently pulou em PG já em version=5; mesma classe de bug aplica aqui.

---

### EC-7: T1.1 — PreviewEnvironment CRs legados sem CellID

- **Task afetada:** T1.1 (PreviewController cell-aware)
- **Teste sugerido:** `TestPreviewReconciler_OnLegacyCRWithoutCellID_ReadsCellFromTenant` — CR com `spec.cellID=""` mas tenant em cell-2. Reconciler deve fazer fallback para `tenant.spec.cellID`, não para cell-default.

---

### EC-8: T1.6 — Billing route requer Stripe customer ID; tenant pode não ter

- **Task afetada:** T1.6 (Billing route wiring)
- **Teste sugerido:** `TestBillingPortal_TenantWithoutStripeCustomer_Returns400` — tenant on Starter (free, sem subscription Stripe) chama `POST /api/v1/billing/portal` → 400 com `code=NO_STRIPE_CUSTOMER` + product-language message. Não 500.

---

### EC-9: T2.3 — Dockerfile lint precisa cobrir `FROM scratch`, `FROM x AS builder`, `FROM $VAR`

- **Task afetada:** T2.3 (Dockerfile FROM allow-list)
- **Teste sugerido:** `TestDockerfileLint_ScratchAndStageNames` — fixtures: `FROM scratch` (allow), `FROM allowed/x AS builder` então `FROM builder` (allow second use of stage name), `FROM $BASE` com `BASE=evil.com/x` (reject), `FROM ${VAR:-docker.io/library/alpine}` (allow with default).

---

### EC-10: T0.3 — Múltiplas detecções de Degraded → múltiplos rollout.aborted SSE

- **Task afetada:** T0.3 (EmitRolloutAborted wire)
- **Teste sugerido:** `TestConvergence_OnRepeatedDegradedPolls_EmitsAbortedOnce` — convergence loop poll 5x com `phase=Degraded` → exatamente 1 chamada de `EmitRolloutAborted`. Idempotency via release state machine: depois do EventAbort, transition é no-op se already Aborted.

---

### EC-11: T7.1 — scale lock contention deve produzir exit 3 (DEPLOY_IN_PROGRESS), não exit 1

- **Task afetada:** T7.1 (scale handler withEnvironmentLock)
- **Teste sugerido:** `TestScale_ConcurrentRejection_ExitCode3` — 3 goroutines `loopzero scale` mesma env → 1 sucesso (exit 0), 2 rejeitados (exit 3 + product-language message "deploy already in progress"). Plan diz 409 mas não especifica CLI exit code mapping.

---

### EC-12: T8.2 — PDB com replicas=1 é meaningless e bloqueia node drain

- **Task afetada:** T8.2 (Bootstrap PDB)
- **Teste sugerido:** `TestPDB_WithReplicasOne_DoesNotBlockNodeDrain` — kubeconform check + assertion que se `replicaCount=1`, o PDB tem `minAvailable=0` (permite drain) OU template explicitly omits PDB. Atual plan adiciona PDB sem aumentar replicas → node drain durante k8s upgrade fica bloqueado para sempre.

---

### EC-13: T2.4 — IPv6 /64 prefix + per-IP key pode ser too aggressive ou too loose

- **Task afetada:** T2.4 (Pre-auth IP rate limit)
- **Teste sugerido:** `TestIPRateLimit_IPv6PrefixGrouping` — atacante com /64 IPv6 cria 2^64 buckets bypassando limit; ou usuários legítimos atrás de NAT IPv6 compartilham bucket. Decisão: agrupar IPv6 por /64 prefix (`2001:db8:1234:5678::/64`) como uma "IP" única.

---

## DOCUMENT

### EC-14: T1.3 — Migração routes/ → domain/ vs feature work concurrent

- **Risco aceito:** Refactor de 9 packages. Se outras PRs estão tocando esses packages durante a migração, merge conflicts paralisam ambos os streams. Plan já menciona "single-PR atomic refactor" — adicionar nota: "freeze feature merges to api/internal/{services,db,k8s,loki,github}/ during T1.3 review window (estimated 2 days)."

---

### EC-15: T7.3 — audit_log 1-year retention vs compliance para eventos financeiros

- **Risco aceito:** Reguladores (PCI, SOC2, financeiros) podem requerer 7 anos para eventos de billing/payment. Plan deve adicionar ADR mencionando: audit_log holds security/operational events (1y); billing events delegated to Stripe (Stripe retains per their SLA, typically 7y+). Justificativa em ADR explícita.

---

### EC-16: T8.3 — Pinar :latest no Tilt pode quebrar hot reload de dev

- **Risco aceito:** Tilt usa `:latest` para `live_update` durante dev. Pinar tags estáticas para clone-runner/syft/cosign pode parecer impedir hot reload mas esses 3 são pipeline-batch images que raramente mudam. Document: "T8.3 só pina pipeline-step images (clone-runner, syft-scanner, cosign-signer); api-deployment + operator deployments mantêm `:latest` com Tilt live_update."

---

### EC-17: T0.5 — Invariant test cresce linearmente com state-bearing tables

- **Risco aceito:** 9 tabelas hoje × 5ms = 45ms por PR. Adicionar 5 novas tabelas (cells_audit, billing_events, etc.) → ~70ms. Aceitável até ~30 tabelas. Document: "se test exceder 200ms, paralellizar via `t.Parallel()` em cada subtest table-driven."

---

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Sim | T0.3 explicitly addresses (find_flow_001); T1.6 (billing); risk in T6.2 (RenderInput.Plan) |
| Correct code in wrong place | Sim | T1.3 (routes/ → domain/) addresses architecturally |
| Project name vs project ID | Sim — risk | T1.6 (billing) precisa usar tenant ID, não project name |
| ArgoCD `notifiers` vs `services` | Não | Out of plan scope |
| Single ArgoCD App per tenant | Não | Existing reality, not plan scope |
| CF scan imports conflicting apex | Não | Out of plan scope |
| Live test against existing DB | Sim | EC-6 above (already SHOULD TEST) |
| RUNTIME_KUBECONFIG mount drift (B34 class) | Sim | EC-4 above (MUST FIX) |
| Dual-chart drift (helm bootstrap vs published) | Sim | T2.1 closes one instance; T8.4 (Tilt seccompProfile) closes another |
| H18 heal recipes drift | Sim | Indirect — T1.4 mount fix prevents future H18 needs |

---

## Resumo

| Phase | Tasks | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|------|-------------------|----------|-------------|----------|
| 0 | 5 | 4 | 2 (EC-1, EC-2) | 2 (EC-6, EC-10) | 0 |
| 1 | 7 | 4 | 1 (EC-4) | 2 (EC-7, EC-8) | 1 (EC-14) |
| 2 | 7 | 3 | 1 (EC-3) | 2 (EC-9, EC-13) | 0 |
| 3 | 4 | 0 | 0 | 0 | 0 |
| 4 | 8 | 0 | 0 | 0 | 0 |
| 5 | 10 | 0 | 0 | 0 | 0 |
| 6 | 8 | 0 | 0 | 0 | 0 |
| 7 | 4 | 2 | 0 | 1 (EC-11) | 1 (EC-15) |
| 8 | 6 | 4 | 1 (EC-5) | 1 (EC-12) | 1 (EC-16) |
| 9 | 4 | 0 | 0 | 0 | 0 |
| Cross-cut | — | — | — | — | 1 (EC-17) |
| **Total** | **62** | **17** | **5** | **8** | **4** |

**Veredicto:** **PLANO PRECISA DE AJUSTE** antes de Phase 0 começar.

### Ações requeridas

1. **Atualizar plano v1.1** dobrando os 5 MUST FIX em sub-tasks:
   - T0.1 ganha SQL guard pre-migration (EC-1)
   - T0.3 ganha factory error fail-safe (EC-2)
   - T1.4 ganha sub-tasks T1.4a/T1.4b/T1.4c ordenadas (EC-4)
   - T2.4 ganha trusted proxy CIDR config explícita (EC-3)
   - T8.5 ganha inventory + dry-run + rollback procedure (EC-5)

2. **Adicionar 8 SHOULD TEST** ao TDD section dos tasks correspondentes.

3. **Adicionar 4 notas DOCUMENT** como callouts no plano.

Após v1.1 lander com esses ajustes, plano está pronto para Phase 0 implementation start.

---

## Filosofia da Análise

Foram **descartados** os seguintes "edge cases" como over-engineering, especulação ou paranoia:

- "E se o disco encher durante migration?" — fora do escopo, é monitoring de infra.
- "E se o operador esquecer de invocar Phase 0 antes de outras phases?" — governance issue, não código.
- "E se atacante compromete RDS?" — fora do threat model (DO managed PG).
- "E se Renovate update conflitar com pinact?" — improvável; configs são complementares.
- "E se Stripe customer portal mudar API?" — third-party integration risk, fora do plan.
- "E se Tempo OTel collector cair?" — observability availability, não plan scope.

**Filtro aplicado:** apenas riscos cuja probabilidade × impacto justifica fix em <=3 linhas de código ou <=1 frase de mudança no plano.

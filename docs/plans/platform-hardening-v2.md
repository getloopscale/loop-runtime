# Platform Hardening v2 — From 3.2 to 4.5

> Plano para corrigir todos os gaps encontrados na sessao de validacao de 2026-04-15.
> Foco: validacao real, observabilidade uniforme, idempotencia verdadeira.

**Status:** Complete (all 5 phases implemented)
**Owner:** Platform Team
**Priority:** P0 — bloqueia confianca em producao
**Estimativa:** 5-7 dias
**Origem:** Auditoria end-to-end destroy→up→bootstrap (2026-04-15)

---

## Diagnostico

Na sessao de validacao, 8 bugs foram encontrados que so aparecem contra infra real.
Todos eram triviais de corrigir, mas nenhum foi pego antes de rodar contra DO.
A causa raiz e tripla:

1. **Sem E2E automatizado contra infra real** — tudo e testado em mock/local
2. **Observabilidade desigual** — API excelente, operators/cloud quase cegos
3. **Pulumi nao-idempotente** — diffs residuais em toda execucao

---

## Fase 1: E2E Semanal Automatizado (~1 dia)

**Objetivo:** Um workflow scheduled que prova que destroy→up→bootstrap→health funciona.

### Task 1.1: Criar workflow `infra-e2e.yml`
**Arquivo:** `.github/workflows/infra-e2e.yml`
**Trigger:** `schedule: cron '0 6 * * 1'` (toda segunda 6am UTC) + `workflow_dispatch`
**Steps:**
```
1. Pre-destroy cleanup (stack=dev)
2. State reset
3. pulumi up (single pass)
4. Bootstrap
5. Health check (all 30 deployments ready)
6. External access check (api, cloud, grafana = HTTP 200)
7. Idempotency check (segundo pulumi up = 0 destructive changes)
8. Report to $GITHUB_STEP_SUMMARY
```
**Criterios de aceite:**
- [ ] Workflow completo em <35min
- [ ] `pulumi up` single-pass (0 erros no primeiro run)
- [ ] Health check valida todos os 30 deployments
- [ ] HTTP 200 em api.dev.loopzero.example, cloud.dev.loopzero.example, grafana.dev.loopzero.example
- [ ] Segundo `pulumi up` = 0 creates, 0 deletes
- [ ] Falha do workflow envia notificacao (GitHub notification ou Slack)

**DoD:** Workflow rodou e passou pelo menos 1x.

### Task 1.2: Criar script `infra/scripts/health-check.sh`
**Arquivo:** `infra/scripts/health-check.sh`
**Responsabilidade:** Validar que a plataforma esta saudavel.
**Checks:**
- [ ] Todos deployments ready (kubectl get deploy --no-headers | awk mismatch)
- [ ] Gitea healthz = pass
- [ ] ArgoCD server Running
- [ ] Grafana Running
- [ ] API health = ok
- [ ] CF Tunnel 2/2
- [ ] Runtime cluster nodes Ready
- [ ] Build cluster nodes Ready
- [ ] External endpoints HTTP 200 (api, cloud, grafana)

**DoD:** Script roda localmente e no CI.

---

## Fase 2: Pulumi Idempotencia Verdadeira (~1 dia)

**Objetivo:** `pulumi up` repetido = exatamente 0 changes.

### Task 2.1: Eliminar `resolve-build-node-ip` timestamp trigger
**Arquivo:** `infra/pulumi/src/components/mgmt-bootstrap.ts`
**Problema:** `command:local:Command resolve-build-node-ip` usa timestamp como trigger,
gerando diff em toda execucao.
**Fix:** Trocar trigger de timestamp para content-based (hash do IP atual).
Alternativa: usar `pulumi.Output` do node IP direto via K8s provider em vez de
`command:local:Command` com curl.
**Criterios de aceite:**
- [ ] Segundo `pulumi up` nao mostra diff para `resolve-build-node-ip`
- [ ] IP do node ainda e resolvido corretamente

### Task 2.2: Eliminar diffs do `gitea-bootstrap` Job
**Problema:** Jobs K8s sao imutaveis — qualquer mudanca causa replace.
O Job usa `retain` policy, mas o replace gera noise no preview.
**Fix:** Mover o bootstrap Job para o `bootstrap-platform.sh` (onde ja pertence)
e remover do Pulumi. Ou usar `ignoreChanges` no Job spec.
**Criterios de aceite:**
- [ ] Segundo `pulumi up` nao mostra diff para `gitea-bootstrap`

### Task 2.3: Eliminar diffs do `loopzero-api-config` ConfigMap
**Problema:** ConfigMap muda porque LOKI_URL contem IP do node (dinamico).
**Fix:** Usar DNS interno do Service em vez de node IP para Loki endpoint.
Se Loki e acessivel via Service, usar `loki.platform-system.svc:3100`.
Se precisa de NodePort, resolver IP uma vez e armazenar no state.
**Criterios de aceite:**
- [ ] Segundo `pulumi up` nao mostra diff para `loopzero-api-config`

### Task 2.4: Validar 0 changes
**Criterios de aceite:**
- [ ] `pulumi preview` mostra `0 changes. N unchanged`
- [ ] Documentar quaisquer diffs aceitos como known (com justificativa)

---

## Fase 3: Observabilidade Uniforme (~2-3 dias)

**Objetivo:** Todos os componentes no mesmo nivel de instrumentacao da API (4/5).

### Task 3.1: `pkg/` — Adicionar `context.Context` a funcoes publicas
**Arquivos:** `pkg/logging/logger.go`, `pkg/errors/errors.go`, `pkg/statemachine/*.go`
**Problema:** Nenhuma funcao do pkg/ aceita `context.Context`. Traces sao quebrados
nas fronteiras da shared library.
**Fix:**
- `logging.NewLogger()` → aceitar `context.Context`, extrair trace_id/span_id
- `WithCorrelationID()` → tambem injetar trace_id se disponivel no ctx
- Funcoes de state machine → aceitar ctx para propagacao
**Nota:** pkg/ nao deve depender de OTel (zero deps externas). Usar interface:
```go
// pkg/logging/context.go
type TraceExtractor func(ctx context.Context) (traceID, spanID string)
```
O caller (api/, operators/) registra o extractor; pkg/ usa sem importar OTel.
**Criterios de aceite:**
- [ ] `logging.NewLoggerFromContext(ctx)` disponivel
- [ ] trace_id aparece nos logs quando ctx tem trace
- [ ] Zero dependencias OTel adicionadas ao pkg/go.mod
- [ ] Testes existentes continuam passando

### Task 3.2: `operators/` — Instrumentacao completa
**Arquivos:** `operators/internal/controller/*.go`
**Problema:** Root spans existem mas sem child spans, sem RecordError, sem metricas custom.
**Fix:**
- Adicionar child spans para cada reconcile step (Gitea, ArgoCD, Harbor, namespace)
- `span.RecordError(err)` + `span.SetStatus(codes.Error)` em todos os erros
- `span.AddEvent("state.transition", ...)` para mudancas de estado
- Metricas custom: `reconcile_duration_seconds` (histogram), `reconcile_errors_total` (counter),
  `crd_count` (gauge por tipo)
- Injetar trace_id nos logs via `log.FromContext(ctx)`
**Criterios de aceite:**
- [ ] Cada controller tem >=3 child spans por reconcile
- [ ] Erros aparecem no trace como span error
- [ ] 5+ metricas custom registradas
- [ ] Logs contêm trace_id

### Task 3.3: `cloud/` — Adicionar OTel SDK
**Arquivos:** `cloud/cmd/main.go`, `cloud/internal/`
**Problema:** Zero OTel. Chamadas a Hydra, Kratos, Stripe sao caixas pretas.
**Fix:**
- Adicionar TracerProvider + OTLP exporter no main.go
- chi middleware para spans automaticos em handlers HTTP
- Instrumentar HTTP clients (Hydra, Kratos, Stripe) com otelhttp
- Trocar contadores atomicos manuais por Prometheus client_golang
- Adicionar histogramas de latencia (auth, billing, Stripe calls)
- Injetar trace_id nos logs slog
**Criterios de aceite:**
- [ ] `go.opentelemetry.io/otel` no go.mod
- [ ] TracerProvider inicializado no main.go
- [ ] Spans em todos os handlers HTTP
- [ ] Hydra/Kratos/Stripe calls geram child spans
- [ ] trace_id nos logs
- [ ] Histogramas de latencia para auth e billing

### Task 3.4: Validar traces end-to-end
**Criterios de aceite:**
- [ ] Request CLI → cloud → api → operators gera trace unico visivel no Tempo
- [ ] Logs dos 3 componentes mostram o mesmo trace_id

---

## Fase 4: Resiliencia do State Pulumi (~0.5 dia)

**Objetivo:** State nunca corrompe com duplicatas.

### Task 4.1: Adicionar validacao de state no workflow
**Arquivo:** `.github/workflows/infra-deploy.yml`
**Problema:** State acumula URNs duplicadas quando `pulumi up` falha mid-way.
**Fix:** Adicionar step pos-deploy que valida integridade do state:
```bash
pulumi stack export | python3 -c "
import sys, json
from collections import Counter
state = json.load(sys.stdin)
urns = [r['urn'] for r in state['deployment']['resources']]
dupes = {u:c for u,c in Counter(urns).items() if c > 1}
if dupes:
    for u,c in dupes.items():
        print(f'::warning::Duplicate URN ({c}x): {u.split(\"::\")[-1]}')
    sys.exit(1)
print(f'State OK: {len(urns)} unique resources')
"
```
**Criterios de aceite:**
- [ ] Step roda apos `pulumi up` (success ou failure)
- [ ] Detecta duplicatas e reporta como warning
- [ ] Nao bloqueia o pipeline (informativo)

---

## Fase 5: Dogfood Pre-Merge para Infra (~0.5 dia)

**Objetivo:** Mudancas em `infra/` sao validadas contra DO antes de merge.

### Task 5.1: `pulumi preview` obrigatorio em PRs
**Arquivo:** `.github/workflows/infra-preview.yml` (ja existe)
**Verificar:**
- [ ] Workflow dispara em PRs que tocam `infra/**`
- [ ] Preview output aparece como comentario no PR
- [ ] Preview mostra quantos creates/updates/deletes
**Fix (se necessario):**
- Garantir que o workflow esta ativo e funcional
- Adicionar comentario no PR com resumo do preview

### Task 5.2: Documentar checklist pre-merge para infra
**Arquivo:** `docs/checklists/infra-pre-merge.md`
**Conteudo:**
```
[ ] pulumi preview mostra mudancas esperadas
[ ] Zero deletes destrutivos nao-intencionais
[ ] Ports de container match com readiness probes
[ ] SecurityContext present em todos os deployments
[ ] Helm chart dependencies resolvem (helm dependency update)
[ ] Script changes validados com bash -n
```

---

## Ordem de Execucao

```
Fase 1 (E2E semanal) ─── ~1 dia
  ├── 1.1: Workflow infra-e2e.yml
  └── 1.2: Script health-check.sh
            │
Fase 2 (Idempotencia) ─── ~1 dia
  ├── 2.1: Eliminar timestamp trigger
  ├── 2.2: Eliminar gitea-bootstrap Job diff
  ├── 2.3: Eliminar loopzero-api-config diff
  └── 2.4: Validar 0 changes
            │
Fase 3 (Observabilidade) ─── ~2-3 dias
  ├── 3.1: pkg/ context propagation
  ├── 3.2: operators/ instrumentacao
  ├── 3.3: cloud/ OTel SDK
  └── 3.4: Validar traces E2E
            │
Fase 4 (State resiliencia) ─── ~0.5 dia
  └── 4.1: Validacao pos-deploy
            │
Fase 5 (Dogfood pre-merge) ─── ~0.5 dia
  ├── 5.1: Preview em PRs
  └── 5.2: Checklist pre-merge
```

---

## Metricas de Sucesso

| Metrica | Antes (3.2/5) | Depois (target 4.5/5) |
|---------|---------------|----------------------|
| Bugs encontrados em deploy real | 8 nesta sessao | 0 (pegos pelo E2E semanal) |
| `pulumi up` segundo run | ~5 diffs | 0 changes |
| OTel coverage (api) | 4/5 | 4.5/5 |
| OTel coverage (operators) | 1.25/5 | 4/5 |
| OTel coverage (cloud) | 1.25/5 | 4/5 |
| OTel coverage (pkg) | 0.75/5 | 3/5 |
| PodSecurity warnings | 3 warnings | 0 |
| State duplicatas | Recorrente | Detectado automaticamente |
| E2E validation frequency | Manual, ad-hoc | Semanal automatizado |
| Trace end-to-end | Quebrado em 2/3 hops | Continuo cloud→api→operators |

---

## Riscos e Mitigacoes

| Risco | Probabilidade | Impacto | Mitigacao |
|-------|--------------|---------|-----------|
| E2E semanal custa $$ | Baixa | Baixo | ~$2/run (20min de clusters DO). 4x/mes = $8 |
| pkg/ context.Context quebra API existente | Media | Medio | Funcoes novas (FromContext), nao alterar assinaturas existentes |
| cloud/ OTel adiciona latencia | Baixa | Baixo | Sampler rate 0.1 em prod, 1.0 em dev |
| Pulumi 0-changes impossivel | Media | Baixo | Documentar diffs aceitos com justificativa |
| State validation falso positivo | Baixa | Baixo | Step informativo, nao blocking |

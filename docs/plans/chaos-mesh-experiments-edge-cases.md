# Edge Case Review — chaos-mesh-experiments

Data: 2026-05-06
Plano: docs/plans/chaos-mesh-experiments-plan.md
Tasks analisadas: 10 (T0.1, T0.2, T0.3, T1.1, T1.2, T1.3, T1.4, T2.1, T3.1, T3.2)
Edge cases encontrados: 9 (MUST FIX: 5, SHOULD TEST: 2, DOCUMENT: 2)

## MUST FIX

### EC-1: k3d cluster stop sem --no-delete pode destruir o cluster
- **Task afetada:** T1.1
- **Familia:** Resource
- **Cenario:** O plano referencia `k3d cluster stop loopzero-workload` mas no ADR D3 menciona `--no-delete` flag. Se alguem chamar `k3d cluster stop loopzero-workload` sem a flag, o cluster NAO e deletado (stop preserva por padrao). POREM, se o teste falhar ANTES do `defer k3dClusterStart()` ser registrado (ex: panic na linha anterior ao defer), o cluster fica parado e todos os testes subsequentes falham em cascata.
- **Impacto:** Suite inteira trava; k3d cluster fica stopped; `task setup` nao o restaura sem intervenção manual.
- **Fix sugerido:** Registrar o defer ANTES de chamar stop: `defer k3dClusterStart("loopzero-workload")` na primeira linha do It(), DEPOIS chamar `k3dClusterStop("loopzero-workload")`. Adicionar `BeforeAll` check que verifica se o cluster esta running e faz start se nao.

### EC-2: T1.2 assume label `loopzero.io/tenant` existe nos namespaces mas Chaos Mesh usa `namespaceSelectors` que pode nao funcionar como esperado
- **Task afetada:** T1.2
- **Familia:** Input / State
- **Cenario:** O plano corretamente identifica que `namespaces: ["loopzero-tn-*"]` NAO funciona (Chaos Mesh nao suporta wildcards). Propoe `namespaceSelectors` com label `loopzero.io/tenant`. Confirmei no codigo: o label EXISTE (envtest tests validam `loopzero.io/tenant` em namespaces criados pelo TenantReconciler). POREM, `namespaceSelectors` no Chaos Mesh v2.7.0 usa `matchLabels`, nao `matchExpressions`. Se o campo for `namespaceSelectors.loopzero.io/tenant: ""`, Chaos Mesh interpreta como "label key existe com valor vazio" — mas no TenantReconciler o valor e o nome do tenant (ex: `loopzero.io/tenant: tn-e2e`), NAO vazio.
- **Impacto:** Manifest nao seleciona nenhum pod; experiment roda mas nao mata nada; teste passa falsamente (0 pods killed = 0 pods to recover).
- **Fix sugerido:** Usar `namespaceSelectors` com `matchExpressions` do tipo `Exists`: `{key: "loopzero.io/tenant", operator: "Exists"}`. Verificar na doc do Chaos Mesh 2.7 se `matchExpressions` e suportado em `namespaceSelectors`. Se nao, usar label fixo `loopzero.io/managed-by: loopzero` (que todo namespace tenant tem).

### EC-3: T1.4 deploy-under-chaos precisa de auth token valido mas chaos_test.go nao tem seed/auth flow
- **Task afetada:** T1.4
- **Familia:** State / Permission
- **Cenario:** O deploy requer um JWT token valido (RSA-only). Os E2E tests resolvem via `resolveToken()` helper (LOOPZERO_TOKEN env var ou `~/.loopzero/local-dogfood-token.txt`). O chaos test package (`chaos_test`) e separado do E2E package — nao tem acesso a esses helpers. Se T1.4 fizer HTTP POST to `/api/v1/builds` sem token, recebe 401.
- **Impacto:** Teste falha com auth error, nao com chaos recovery. False negative.
- **Fix sugerido:** Adicionar `resolveToken()` helper ao chaos_test.go (copiar minimal version, ~10 linhas) ou ler `LOOPZERO_TOKEN` env var diretamente. O CI workflow (T2.1) deve exportar `LOOPZERO_TOKEN` como secret.

### EC-6: pg-network-latency.yaml usa $PG_PRIVATE_HOST que nao e substituido
- **Task afetada:** T0.3 (novo — fix de manifests existentes), T3.1 (validacao)
- **Familia:** Input / State
- **Cenario:** `pg-network-latency.yaml` linha 56 tem `externalTargets: ["${PG_PRIVATE_HOST}"]`. Isso requer `envsubst` antes de `kubectl apply`. O `applyChaos()` helper faz `kubectl apply -f <path>` direto — sem envsubst. O manifest e aplicado com string literal `${PG_PRIVATE_HOST}`, Chaos Mesh nao resolve a variavel, e o NetworkChaos nao target nada. **O cenario 4 (PG network latency) passa trivialmente porque o fault nunca e injetado.**
- **Impacto:** Cenario 4 e um false positive — teste "passa" mas nenhuma falha foi injetada. Invalida a evidencia de resiliencia.
- **Fix sugerido:** Trocar `externalTargets: ["${PG_PRIVATE_HOST}"]` por `labelSelectors: {"app": "postgresql"}` no namespace correto. Elimina dependencia de envsubst e funciona em qualquer ambiente (k3d ou DOKS).

### EC-7: Simultaneous API+controller crash — manifest usa label diferente do teste
- **Task afetada:** T0.3 (novo — fix de manifests existentes), T3.1 (validacao)
- **Familia:** State / Consistency
- **Cenario:** `simultaneous-api-controller-crash.yaml` usa selector `labelSelectors: "loopzero.io/component": "control-plane"`. Porem, o cenario 8 no teste Go (`chaos_test.go:343-346`) NAO usa o manifest — faz `kubectl delete pod -l app=loopzero-api` e `-l app.kubernetes.io/component=loopzero-operator` diretamente. Ha uma inconsistencia entre o manifest (labels `loopzero.io/component`) e o teste (labels `app`/`app.kubernetes.io/component`). O manifest pode nunca ser usado por nenhum teste. Adicionalmente, com o operator multi-binary split (ADR-0023), se nenhum pod tiver `loopzero.io/component: control-plane`, o PodChaos nao mata nada.
- **Impacto:** Manifest orphan — existe mas nenhum teste o consome via `applyChaos()`. Se alguem corrigir o teste para usar o manifest, os labels podem nao selecionar pods.
- **Fix sugerido:** (a) Reescrever cenario 8 para usar `applyChaos("simultaneous-api-controller-crash.yaml")` em vez de `kubectl delete` direto, e (b) atualizar o manifest para usar os labels corretos (`app=loopzero-api` + `app.kubernetes.io/component=loopzero-operator`), ou (c) deletar o manifest orphan se o teste direto com `kubectl delete` for a abordagem correta.

## SHOULD TEST

### EC-4: Chaos Mesh daemon nao instalado no build cluster
- **Task afetada:** T1.3
- **Cenario:** T0.1 adiciona helmfile release com condition `chaosMesh.enabled`. Se o build cluster nao tiver o helmfile release (ex: helmfile so roda no mgmt), IOChaos no build cluster falha com `no chaos daemon found`. O plano menciona fallback Skip, mas nao especifica COMO detectar.
- **Teste sugerido:** `test_iochaos_skips_when_daemon_missing` — antes de `applyChaos()`, executar `kubectl -n chaos-mesh get ds chaos-daemon --context $BUILD_CONTEXT` e Skip se NotFound. Adicionar como pre-check no cenario 11.

### EC-5: checkMetricExists pode falhar por timing
- **Task afetada:** T0.3 + varias
- **Cenario:** `checkMetricExists()` faz `wget /metrics` no pod. Se o pod acabou de reiniciar (pos-chaos), o metrics endpoint pode nao ter emitido o metric ainda (contadores comecam do zero). O teste falha com "metric X not found" quando na verdade o sistema esta saudavel.
- **Teste sugerido:** `test_metric_check_with_retry` — `checkMetricExists()` deveria ter retry com 3 tentativas e 5s entre cada, nao single-shot. Verificar se os 9 testes existentes sofrem desse timing issue.

## DOCUMENT

### EC-8: Chaos suite e CI concorrem pelo mesmo cluster
- **Risco aceito:** Se `chaos-suite.yml` (Sunday 03:00) e `cell2-soaks.yml` (Sunday 02:00) rodarem simultaneamente, ambos injetam falhas no mesmo cluster. Resultados ficam indeterminiticos. A janela de overlap e pequena (cell2-soaks leva ~75min, chaos leva ~45min), mas se cell2-soaks atrasar, ambos rodam juntos.
- **Por que e ok nao tratar agora:** Ambos workflows tem `concurrency.group` diferente, entao nao se cancelam. O risco e flaky results, nao data loss. Mitigacao futura: shift chaos para 05:00 UTC (apos cell2-soaks terminar). Nao e MUST FIX porque o cenario so acontece 1x/semana e o impacto e re-run.

### EC-9: IOChaos em k3d pode nao funcionar (fuse-overlayfs nao disponivel)
- **Risco aceito:** IOChaos requer kernel support para fuse injection. k3d roda containers Docker com kernel do host. Se o host nao tiver `fuse` modulo carregado, IOChaos falha silenciosamente. O plano ja cobre isso com "test should Skip with clear message" (T1.3 acceptance criteria). Documentar como limitacao conhecida.
- **Por que e ok nao tratar agora:** A alternativa (rodar IOChaos em DOKS) requer acesso ao cluster de producao que nao e escopo deste plano. O Skip + warning e suficiente. Quando IOChaos for necessario para SLA, mover para DOKS.

## Padroes Sistemicos Detectados

| Padrao | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | **SIM** | EC-6: `${PG_PRIVATE_HOST}` nunca e substituido (false positive no cenario 4); EC-7: manifest orphan — nenhum teste consome via `applyChaos()` + label pode nao selecionar pods pos-split. Exatamente o padrao "manifest exists but never ran" |
| Correct code in wrong place | Nao | N/A |
| Project name vs ID | Nao | Chaos tests nao usam project IDs |
| ArgoCD notifiers not services | Nao | N/A |
| Single ArgoCD App per tenant | Nao | N/A |

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 0 | 0 | 0 | 0 |
| T0.2 | 1 | 0 | 1 (EC-5) | 0 |
| T0.3 | 2 | 2 (EC-6, EC-7) | 0 | 0 |
| T1.1 | 1 | 1 (EC-1) | 0 | 0 |
| T1.2 | 1 | 1 (EC-2) | 0 | 0 |
| T1.3 | 2 | 0 | 1 (EC-4) | 1 (EC-9) |
| T1.4 | 1 | 1 (EC-3) | 0 | 0 |
| T2.1 | 1 | 0 | 0 | 1 (EC-8) |
| T3.1 | 0 | 0 | 0 | 0 |
| T3.2 | 0 | 0 | 0 | 0 |

**Veredicto:** PLANO PRECISA DE AJUSTE — 5 MUST FIX devem ser incorporados como sub-tasks antes de implementar. EC-6 e EC-7 promovidos de SHOULD TEST para MUST FIX (v1.2, revisão 2026-05-11): ambos causam false positives em testes existentes (padrão "implemented but not wired").

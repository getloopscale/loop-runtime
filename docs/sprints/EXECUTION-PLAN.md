# Execution Plan — LoopZero PaaS

> Ultima atualizacao: 2026-03-27
>
> Plano de execucao detalhado com tasks, Definition of Done, e testes de dogfood.
> Cada sprint segue TDD: teste falha → implementa → teste passa → valida em DO.

---

## H6.1 — loopzero status

**Objetivo:** Wiring da rota `GET /api/v1/projects/{name}/status` que ja tem handler, CLI e testes implementados.
**Pre-requisito:** Nenhum.
**Estimativa:** 30min

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | Registrar rota em `server.go` | Adicionar `StatusHandler` ao `Config` struct e registrar `r.Get("/api/v1/projects/{name}/status", ...)` no `NewRouter()` em `api/internal/server/server.go`. Criar adapter em `api/cmd/main.go` para injetar `StatusBuildStore`. | 30min |

### Definition of Done

- [ ] `GET /api/v1/projects/{name}/status?env=production` retorna 200 com JSON contendo builds recentes
- [ ] `loopzero status --json` retorna exit code 0 com schema correto
- [ ] Testes existentes em `cli/tests/unit/commands/` continuam passando
- [ ] Validado no DigitalOcean (nao apenas local)

### Dogfood Test

```bash
# Deploy um servico primeiro (se nao existir)
loopzero deploy

# Verificar status via CLI
loopzero status
loopzero status --json
loopzero status --env production

# Verificar via API direta
curl -H "Authorization: Bearer $GITHUB_TOKEN" \
  https://api.dev.loopzero.example/api/v1/projects/my-api/status?env=production
```

**Exit Gate:** `loopzero status --json` retorna dados reais do ultimo deploy no DO. Exit code 0.

---

## H7 — Workload Security

**Objetivo:** Garantir que pods no workload cluster rodam com privilegios minimos. Um pod comprometido nao pode escalar para o cluster.
**Pre-requisito:** Nenhum (pode rodar em paralelo com H6.1).
**Estimativa:** 6-8h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | PodSecurityAdmission restricted | Configurar PSA `restricted` nos namespaces de workload via label `pod-security.kubernetes.io/enforce: restricted` no EnvironmentController (`operators/internal/controller/environment_controller.go`) | 1h |
| 2 | SecurityContext nos manifests gerados | Atualizar `api/internal/manifests/` para gerar Deployments com `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `allowPrivilegeEscalation: false`, `drop: [ALL]` capabilities | 1.5h |
| 3 | NetworkPolicy por namespace | Adicionar NetworkPolicy default-deny-ingress + allow-from-traefik no EnvironmentController. Pods so recebem trafego do ingress controller. | 1h |
| 4 | Resource limits obrigatorios | Garantir que todo Deployment gerado tem `resources.limits` (CPU + memory) e `resources.requests`. Valores default se nao especificado no `loopzero.yaml`. | 1h |
| 5 | ServiceAccount dedicado | Cada app recebe ServiceAccount proprio com `automountServiceAccountToken: false`. Sem acesso a API do Kubernetes. | 30min |
| 6 | Testes unitarios | Testes para cada SecurityContext, NetworkPolicy, e ResourceLimits gerados em `api/internal/manifests/` e `operators/internal/controller/` | 1.5h |
| 7 | Validacao em DO | Deploy no DigitalOcean, verificar que pods rodam non-root, PSA rejeita pods inseguros, NetworkPolicy bloqueia trafego lateral | 1h |

### Definition of Done

- [ ] `kubectl get ns <workload-ns> -o yaml` mostra label PSA `restricted`
- [ ] `kubectl get pod <app-pod> -o jsonpath='{.spec.securityContext}'` mostra `runAsNonRoot: true`
- [ ] `kubectl get networkpolicy -n <workload-ns>` mostra default-deny + allow-traefik
- [ ] Pod com `privileged: true` e rejeitado pelo PSA
- [ ] Testes unitarios passam para manifests com security context
- [ ] App continua acessivel via URL publica apos NetworkPolicy

### Dogfood Test

```bash
# Deploy normalmente
loopzero deploy

# Verificar que o pod roda non-root
kubectl get pod -n <ns> -o jsonpath='{.items[0].spec.containers[0].securityContext}'

# Tentar criar pod privilegiado (deve falhar)
kubectl run test-priv --image=nginx --privileged -n <workload-ns>
# Expected: Error from server (Forbidden): pods "test-priv" is forbidden: violates PodSecurity "restricted"

# Verificar NetworkPolicy
kubectl get networkpolicy -n <workload-ns>

# App ainda funciona via URL
curl -s https://my-api.dev.loopzero.example/health
```

**Exit Gate:** Pod comprometido nao consegue escalar privilegios nem acessar outros pods. App funciona normalmente.

---

## Sprint 11 — Validacao de Controllers em DO

**Objetivo:** Validar que os 5 controllers ja wired em `operators/cmd/main.go` (Environment, Application, Exposure, GitRepositoryBinding, PlatformBootstrap) funcionam corretamente no DigitalOcean, nao apenas em envtest.
**Pre-requisito:** Nenhum (controllers ja estao wired no codigo).
**Estimativa:** 8-12h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | EnvironmentController validation | Criar um novo Tenant via `loopzero init` + deploy. Verificar que o EnvironmentController cria namespace, RBAC, e ResourceQuota no runtime cluster. Fix de bugs encontrados. (`operators/internal/controller/environment_controller.go`) | 2h |
| 2 | ApplicationController validation | Verificar que LoopZeroApplication CR recebe hostname atribuido e ExposedRoute CR e criado. Validar health tracking do pod. (`operators/internal/controller/application_controller.go`) | 2h |
| 3 | ExposureController validation | Verificar que ExposedRoute CR faz check de TLS certificate e reachability probe. URL retorna 200. (`operators/internal/controller/exposure_controller.go`) | 2h |
| 4 | GitRepositoryBindingController validation | Verificar sync de credenciais Git para ArgoCD Secrets. Simular rotacao de credencial e validar que o hash SHA256 detecta a mudanca. (`operators/internal/controller/gitrepositorybinding_controller.go`) | 2h |
| 5 | PlatformBootstrapController validation | Verificar fluxo completo: Gitea org → ArgoCD app → API seed. Pode ser testado destruindo e recriando o ambiente. (`operators/internal/controller/platformbootstrap_controller.go`) | 2h |

### Definition of Done

- [ ] Cada controller reconcilia CRs sem erros no log do operator pod
- [ ] `kubectl get environments,loopzeroapplications,exposedroutes -A` mostra recursos com status correto
- [ ] EnvironmentController: namespace + RBAC + quota existem no runtime cluster
- [ ] ApplicationController: hostname atribuido, ExposedRoute criado
- [ ] ExposureController: TLS cert validado, reachability OK
- [ ] GitRepositoryBindingController: Secret sincronizado para ArgoCD
- [ ] PlatformBootstrapController: bootstrap completo sem intervencao manual

### Dogfood Test

```bash
# Verificar que controllers estao rodando
kubectl logs -n loopzero-system deploy/loopzero-operators --tail=50 | grep "controller"

# Verificar CRs
kubectl get tenants -A
kubectl get environments -A
kubectl get loopzeroapplications -A
kubectl get exposedroutes -A
kubectl get gitrepositorybindings -A

# Verificar recursos criados pelo EnvironmentController
kubectl get ns | grep loopzero-
kubectl get rolebinding -n <workload-ns>
kubectl get resourcequota -n <workload-ns>

# Verificar hostname do ApplicationController
kubectl get loopzeroapplication <app> -o jsonpath='{.status.hostname}'

# Verificar TLS do ExposureController
kubectl get exposedroute <route> -o jsonpath='{.status.tlsReady}'

# Deploy completo para validar fluxo end-to-end
loopzero deploy
loopzero status
curl -s https://my-api.dev.loopzero.example/health
```

**Exit Gate:** Todos os 7 controllers reconciliam sem erros. Novo tenant self-provisiona namespace, RBAC, hostname e TLS automaticamente.

---

## Sprint 5 — Preview Environments

**Objetivo:** PR aberta gera URL efemera automaticamente. Merge/close limpa todos os recursos.
**Pre-requisito:** Sprint 11 (controllers validados — EnvironmentController cria namespaces, ExposureController valida TLS).
**Estimativa:** 20-30h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | PreviewEnvironment CRD + controller | Implementar `PreviewEnvironmentController` em `operators/internal/controller/`. Reconcilia `PreviewEnvironment` CRs: cria namespace efemero, deploya app, atribui hostname `pr-{number}.{project}.dev.loopzero.example`. State machine ja definida em `pkg/statemachine/preview.go`. | 5h |
| 2 | API: preview endpoints | `POST /api/v1/projects/{name}/preview` (cria preview), `GET /api/v1/projects/{name}/previews` (lista), `DELETE /api/v1/projects/{name}/preview/{id}` (cleanup manual). Em `api/internal/routes/`. | 4h |
| 3 | CLI: `loopzero preview` command | Implementar em `cli/src/commands/preview.ts`. Subcomandos: `loopzero preview create --pr 42`, `loopzero preview list`, `loopzero preview delete <id>`. Exit codes corretos, `--json` flag, sem termos internos. | 4h |
| 4 | Preview build pipeline | Adaptar `BuildOrchestrator` para suportar preview builds: imagem taggeada como `pr-{number}`, manifests em branch separada no Gitea, ArgoCD Application apontando para branch de preview. | 4h |
| 5 | TTL + auto-cleanup | Preview environments tem TTL (default 72h). Cleanup automatico quando PR e merged/closed (via webhook ou polling). Reaper dedicado para previews expirados. | 3h |
| 6 | GitHub webhook integration | Endpoint `POST /api/v1/webhooks/github` para receber eventos `pull_request.closed` e `pull_request.merged`. Trigger cleanup automatico. | 3h |
| 7 | Testes | Unitarios para controller, API endpoints, CLI command. Integracao para fluxo completo preview create → deploy → cleanup. | 4h |
| 8 | Validacao em DO | Criar PR real, executar `loopzero preview create`, verificar URL funcional, merge PR, verificar cleanup. | 3h |

### Definition of Done

- [ ] `loopzero preview create --pr 42` cria preview environment e retorna URL
- [ ] URL `pr-42.my-api.dev.loopzero.example` responde com a versao do PR
- [ ] `loopzero preview list --json` lista previews ativos com schema estavel
- [ ] Merge/close do PR limpa namespace, Deployment, ArgoCD Application e DNS
- [ ] TTL de 72h funciona: preview expirado e limpo automaticamente
- [ ] Testes unitarios e integracao passam
- [ ] Nenhum termo interno na saida do CLI

### Dogfood Test

```bash
# Criar preview para PR #42
loopzero preview create --pr 42
# Expected: Preview created! URL: https://pr-42.my-api.dev.loopzero.example

# Verificar que esta funcionando
curl -s https://pr-42.my-api.dev.loopzero.example/health

# Listar previews
loopzero preview list
loopzero preview list --json

# Verificar recursos no cluster
kubectl get previewenvironments -A
kubectl get ns | grep preview

# Simular merge (ou merge real)
# Verificar cleanup automatico apos merge
loopzero preview list
# Expected: nenhum preview ativo

# Cleanup manual (se necessario)
loopzero preview delete pr-42
```

**Exit Gate:** PR → preview URL funcional → merge → cleanup automatico. Zero recursos orfaos.

---

## Sprint 4 — Promote + Cancel

**Objetivo:** Promover releases entre ambientes com safety checks e cancelar builds em andamento.
**Pre-requisito:** Sprint 11 (controllers validados).
**Estimativa:** 15-20h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | API: promote endpoint | `POST /api/v1/projects/{name}/promote` em `api/internal/routes/`. Usa `withEnvironmentLock`. Valida health do source environment antes de promover. State machine em `pkg/statemachine/promotion.go`. | 3h |
| 2 | PromotionOrchestrator | Service em `api/internal/services/` que coordena: validar source release → copiar manifests → push gitops → trigger ArgoCD sync → atualizar environment_target. | 4h |
| 3 | CLI: `loopzero promote` | Implementar em `cli/src/commands/promote.ts`. `loopzero promote --from staging --to production`. Exit codes: 0 (sucesso), 3 (deploy failure), 5 (partial). `--json` flag. | 3h |
| 4 | API: cancel endpoint | `POST /api/v1/builds/{id}/cancel` ja existe em `routes/builds.go`. Verificar que funciona end-to-end: cancela Argo Workflow, transiciona build para CANCELLED, emite SSE event. | 2h |
| 5 | CLI: `loopzero cancel` | Implementar em `cli/src/commands/cancel.ts`. `loopzero cancel` (cancela build mais recente) ou `loopzero cancel <build-id>`. | 2h |
| 6 | Testes | Unitarios: promotion state machine transitions, cancel flow. Integracao: promote com lock contention, cancel durante build ativo. | 3h |
| 7 | Validacao em DO | Deploy em staging, promover para production, verificar que a mesma imagem roda em ambos. Iniciar build, cancelar, verificar cleanup. | 2h |

### Definition of Done

- [ ] `loopzero promote --from staging --to production` promove release com sucesso
- [ ] Promotion valida health do source antes de promover (rejeita se unhealthy)
- [ ] `loopzero cancel` cancela build em andamento, Argo Workflow terminado
- [ ] State machines respeitadas: promotion (Validating → Committing → Syncing → Completed), build (RUNNING → CANCELLING → CANCELLED)
- [ ] `withEnvironmentLock` usado em promote e cancel
- [ ] `--json` em ambos os comandos com schema estavel
- [ ] Testes unitarios e integracao passam

### Dogfood Test

```bash
# Deploy em staging
loopzero deploy --env staging

# Verificar que esta rodando
loopzero status --env staging

# Promover para production
loopzero promote --from staging --to production
loopzero promote --from staging --to production --json

# Verificar production
loopzero status --env production
curl -s https://my-api.dev.loopzero.example/health

# Testar cancel
loopzero deploy --env staging  # Iniciar deploy
loopzero cancel                # Cancelar imediatamente
# Expected: Build cancelled. Exit code 0.

loopzero cancel --json
```

**Exit Gate:** Promote staging → production funciona em DO. Cancel interrompe build ativo em < 5s.

---

## Sprint 6 — Security + Supply Chain

**Objetivo:** Imagens assinadas com cosign, SBOM via syft, policy enforcement para rejeitar imagens nao assinadas.
**Pre-requisito:** Sprint 5 (preview environments precisam de policy para cleanup de imagens).
**Estimativa:** 15-20h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | cosign key setup | Gerar e armazenar cosign key pair. Key publica distribuida via ConfigMap. Key privada em Secret no build cluster. | 2h |
| 2 | Signing step no workflow | Adicionar step `cosign sign` no Argo Workflow apos push da imagem. Em `api/internal/argo/workflow_builder.go`. | 3h |
| 3 | SBOM generation | Adicionar step `syft` no workflow para gerar SBOM (SPDX format) e anexar a imagem via cosign. | 2h |
| 4 | Signature verification | Admission webhook ou policy controller (Kyverno/Connaisseur) no workload cluster que rejeita imagens sem assinatura valida. | 3h |
| 5 | CLI: `loopzero inspect` enhancement | Adicionar info de assinatura e SBOM no `loopzero inspect` (ou `loopzero status --detail`). Mostrar "Image signed: yes/no", "SBOM: available/missing". | 2h |
| 6 | Testes | Unitarios: workflow com signing step. Integracao: deploy com imagem assinada passa, imagem nao assinada e rejeitada. | 3h |
| 7 | Validacao em DO | Deploy completo, verificar assinatura com `cosign verify`, verificar SBOM com `cosign download sbom`. | 2h |

### Definition of Done

- [ ] Toda imagem buildada pelo LoopZero e assinada automaticamente com cosign
- [ ] SBOM (SPDX) e gerado e anexado a cada imagem
- [ ] `cosign verify --key <pub-key> <image>` retorna sucesso
- [ ] Imagem sem assinatura e rejeitada pelo workload cluster
- [ ] CLI mostra status da assinatura
- [ ] Key rotation documentada e testada

### Dogfood Test

```bash
# Deploy normal (agora com signing)
loopzero deploy

# Verificar assinatura
cosign verify --key k8s://loopzero-system/cosign-pub \
  registry.digitalocean.com/loopzero-infra-registry/my-api:latest

# Verificar SBOM
cosign download sbom registry.digitalocean.com/loopzero-infra-registry/my-api:latest

# Tentar deploy de imagem nao assinada (deve falhar no workload cluster)
# Verificar via logs do admission webhook

# Status mostra info de signing
loopzero status --json | jq '.signing'
```

**Exit Gate:** 100% das imagens assinadas. Imagem nao assinada rejeitada. SBOM disponivel para auditoria.

---

## Sprint 8 — Chaos Testing

**Objetivo:** Provar que o sistema sobrevive a falhas reais em producao: concorrencia extrema, pod kill, network partition, DB failover.
**Pre-requisito:** Sprint 6 (supply chain — chaos testa o pipeline completo incluindo signing).
**Estimativa:** 10-15h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | 50 deploys concorrentes | Ginkgo test em `infra/tests/chaos/`. 50 goroutines fazem `POST /api/v1/builds` simultaneamente. Exatamente 1 aceito (202), 49 rejeitados (409). Zero data corruption. | 3h |
| 2 | Pod kill durante deploy | Kill do API pod durante build ativo. BuildReconcileLoop deve detectar e retomar. Build termina com sucesso ou falha limpa (nunca stuck). | 2h |
| 3 | DB failover durante deploy | Simular failover de PostgreSQL (kill primary, Patroni promove replica). Builds em andamento devem falhar com erro claro ou retomar apos reconexao. | 2h |
| 4 | Network partition build↔mgmt | Desconectar build cluster do mgmt. ReconcileLoop deve detectar timeout e marcar builds como FAILED. Reconexao retoma operacao normal. | 2h |
| 5 | ArgoCD indisponivel | Parar ArgoCD. Circuit breaker deve abrir. Builds completam ate gitops-push mas release fica PENDING. Quando ArgoCD volta, circuit breaker fecha e releases completam. | 2h |
| 6 | Cleanup validation | Apos todos os cenarios de chaos, verificar: zero recursos orfaos, zero builds stuck, zero releases stuck, database consistente. | 1h |
| 7 | Metricas de chaos | Registrar tempos de recuperacao, taxa de sucesso, e latencias durante chaos. Comparar com baseline. | 1h |

### Definition of Done

- [ ] 50 deploys concorrentes: exatamente 1 aceito, 49 rejeitados, zero data corruption
- [ ] Pod kill: build retomado ou falhado limpo em < 60s
- [ ] DB failover: sistema recupera em < 30s apos novo primary
- [ ] Network partition: builds falhados com erro claro, recuperacao automatica
- [ ] Circuit breaker: abre quando ArgoCD cai, fecha quando volta
- [ ] Zero recursos orfaos apos todos os cenarios
- [ ] Todos os testes em `infra/tests/chaos/` passam

### Dogfood Test

```bash
# Rodar suite de chaos completa
cd infra/tests && go test ./chaos/... -v -timeout 30m

# Verificar estado pos-chaos
kubectl get workflows -n loopzero-builds --field-selector status.phase=Running
# Expected: nenhum stuck

loopzero status
# Expected: ultimo deploy com status claro (succeeded ou failed, nunca running indefinidamente)

# Verificar metricas
curl -s http://localhost:9090/api/v1/query?query=loopzero_build_duration_seconds_bucket | jq .
```

**Exit Gate:** Todos os cenarios de chaos passam. Tempo de recuperacao < 60s para cada cenario. Zero data loss.

---

## Sprint 9 — Polish + Launch

**Objetivo:** Um desenvolvedor externo ao time faz onboarding completo em menos de 10 minutos, sem ajuda.
**Pre-requisito:** Todos os sprints anteriores.
**Estimativa:** 10-15h

### Tasks

| # | Task | Descricao | Estimativa |
|---|------|-----------|------------|
| 1 | `loopzero doctor` | Comando de diagnostico que verifica: CLI atualizado, autenticado, projeto configurado, API acessivel, ultimo deploy saudavel. Sugere acoes para cada problema. (`cli/src/commands/doctor.ts`) | 3h |
| 2 | Error messages com next-action | Revisar todas as mensagens de erro do CLI. Cada erro deve sugerir o proximo passo: "Run `loopzero login` to authenticate", "Run `loopzero deploy --force` to retry". | 2h |
| 3 | Onboarding docs | Documentacao "Getting Started" que cobre: install → login → init → deploy → status → logs → rollback. Maximo 1 pagina. | 2h |
| 4 | CLI help text polish | Revisar `--help` de todos os comandos. Descricoes claras, exemplos uteis, flags documentadas. | 1h |
| 5 | `loopzero init` wizard melhorado | Deteccao automatica de frameworks com confirmacao interativa. Monorepo detection (Turbo/Nx/pnpm) com sugestao de services. | 2h |
| 6 | Validacao com usuario externo | Convidar 1-2 devs externos para fazer onboarding. Observar, anotar friction points, corrigir. | 3h |
| 7 | CHANGELOG completo | Garantir que `CHANGELOG.md` tem todas as entries desde v0.1.0 ate o estado atual. | 1h |

### Definition of Done

- [ ] `loopzero doctor` identifica todos os problemas comuns e sugere solucao
- [ ] Nenhuma mensagem de erro generica ("something went wrong") em todo o CLI
- [ ] Dev externo completa `install → login → init → deploy → URL live` em < 10min
- [ ] `loopzero --help` e cada subcomando tem descricao clara com exemplos
- [ ] CHANGELOG.md atualizado e completo
- [ ] Zero termos internos em qualquer saida do CLI

### Dogfood Test

```bash
# Simular onboarding do zero (novo terminal, sem config)
rm -rf ~/.loopzero

# Instalar
npm install -g @loopzero/cli

# Diagnosticar
loopzero doctor
# Expected: sugere "loopzero login" como primeiro passo

# Login
loopzero login

# Diagnosticar novamente
loopzero doctor
# Expected: sugere "loopzero init" em um projeto

# Init em projeto novo
cd ~/my-new-project
loopzero init

# Deploy
loopzero deploy

# Verificar
loopzero status
loopzero logs --tail 10
curl -s $(loopzero status --json | jq -r '.url')

# Tempo total: < 10 minutos
```

**Exit Gate:** Dev externo faz onboarding em < 10min sem ajuda. NPS >= 8.

---

## Resumo de Estimativas

| Sprint | Estimativa | Acumulado |
|--------|-----------|-----------|
| H6.1 — loopzero status | 30min | 30min |
| H7 — Workload Security | 6-8h | ~8h |
| Sprint 11 — Controllers em DO | 8-12h | ~20h |
| Sprint 5 — Preview Environments | 20-30h | ~50h |
| Sprint 4 — Promote + Cancel | 15-20h | ~68h |
| Sprint 6 — Security + Supply Chain | 15-20h | ~86h |
| Sprint 8 — Chaos Testing | 10-15h | ~99h |
| Sprint 9 — Polish + Launch | 10-15h | ~112h |

**Total estimado: 85-120h de trabalho** (excluindo tempo de espera e debugging em DO).

---

## Notas

1. **Todas as estimativas incluem TDD**: teste falha → implementa → teste passa.
2. **"Validacao em DO" nao e opcional**: entrega so e DONE se funcionar no DigitalOcean, nao apenas testes locais.
3. **Paralelismo possivel**: H6.1, H7 e Sprint 11 podem rodar em paralelo (sem dependencias entre si).
4. **Sprint 5 e Sprint 4 podem rodar em paralelo** apos Sprint 11, se houver mais de uma pessoa.
5. **Cada sprint atualiza o CHANGELOG.md** com as mudancas na secao `[Unreleased]`.

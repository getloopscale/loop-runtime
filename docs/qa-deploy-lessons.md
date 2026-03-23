# Q&A — Lições de Deploy em Produção

> Documento gerado a partir da sessão de deploy do LoopZero na DigitalOcean.
> Cada pergunta representa um problema real encontrado e resolvido.

---

## Infraestrutura (Pulumi + DigitalOcean)

### Q: Por que o `pulumi up` com `|| true` é perigoso?

**A:** O `|| true` mascara falhas parciais. O Pulumi cria alguns recursos (VPC, bucket) mas falha em outros (clusters). O state fica incompleto — registra os recursos criados mas não os que falharam. Na próxima execução, tenta criar os que faltam mas conflita com os que já existem (ex: `BucketAlreadyExists`).

**Fix:** Remover `|| true`. Usar `pulumi.com/skipAwait: "true"` nos Deployments/Services que podem falhar em ImagePullBackOff (imagens ainda não existem). Isso permite que o `pulumi up` complete sem bloquear.

---

### Q: Por que separar o deploy em 3 Steps no GitHub Actions?

**A:** Num deploy from-scratch, existe um chicken-and-egg: os Deployments precisam de imagens no registry, mas o registry é criado pelo Pulumi junto com os Deployments. A solução:

| Step | O que faz | Por que |
|------|-----------|---------|
| **Step 1** | `pulumi up` — cria toda infra + Helm charts | Deployments ficam em ImagePullBackOff (esperado) |
| **Step 2** | Build + push imagens ao registry | Registry já existe (criado no Step 1) |
| **Step 3** | `pulumi up` novamente | Agora os Deployments encontram as imagens |

---

### Q: Por que adicionar `pulumi refresh` antes do Step 3?

**A:** Entre Step 1 e Step 3 (~5 min), o Kubernetes garbage-collect Jobs completados (ex: `gitea-bootstrap`). O Pulumi state ainda referencia o Job, mas ele não existe mais no cluster. O `refresh` sincroniza o state com a realidade — remove o Job do state, permitindo que o `up` o recrie se necessário.

---

### Q: O que fazer quando uma região da DigitalOcean retorna "insufficient capacity"?

**A:** Trocar de região. Todas as regiões DO suportam Kubernetes e Spaces. Ao mudar:
1. Atualizar `region` no `Pulumi.dev.yaml`
2. Verificar que endpoints de Spaces usam `config.region` (não hardcoded `nyc3`)
3. O destroy cleanup deve escanear **todas** as regiões para deletar buckets órfãos
4. Limpar o Pulumi state (recursos da região antiga não existem na nova)

---

### Q: Por que a VPC não pode ser deletada após destroy?

**A:** Quando todos os clusters DOKS de uma VPC são deletados, a DigitalOcean promove a VPC para "default" da região. VPCs default não podem ser deletadas via API (retorna 403).

**Fix:** Importar a VPC existente no Pulumi state antes do recreate:
```bash
pulumi import digitalocean:index/vpc:Vpc loopzero-infra-dev-sfo3-net <vpc-id>
```

**Fix definitivo futuro:** Adicionar `retainOnDelete: true` no recurso VPC do Pulumi, ou não deletar a VPC no pre-destroy cleanup.

---

### Q: Por que o destroy precisa deletar load balancers antes das VPCs?

**A:** O Traefik cria Load Balancers via Service `type: LoadBalancer`. Quando o cluster é deletado com `dangerous=true`, o DOKS não limpa os LBs. Esses LBs ficam como "membros" da VPC, impedindo a deleção. O pre-destroy deve:
1. Deletar todos os Load Balancers
2. Deletar clusters (`dangerous=true`)
3. Esperar 2 min
4. Deletar VPC (com retry se ainda tiver membros)

---

## Operador (TenantController)

### Q: Por que os namespaces usam o prefixo `loopzero-`?

**A:** Convenção: `loopzero-{tenantID}-{environment}` (ex: `loopzero-tn-loopzero-production`). O prefixo evita colisão com namespaces do sistema e torna claro que o namespace é gerenciado pelo LoopZero.

**Bug encontrado:** O `reconcileNamespaces` usava `loopzero-%s-%s` mas `reconcileRBAC` e `reconcileNetworkPolicies` usavam `%s-%s` (sem prefixo). Resultado: namespace criado como `loopzero-tn-loopzero-production`, mas RBAC procurava `tn-loopzero-production` → `not found`.

**Regra:** TODA referência a namespace de tenant deve usar `fmt.Sprintf("loopzero-%s-%s", tenant.Name, env.Name)`.

---

### Q: Por que a NetworkPolicy bloqueava o Traefik?

**A:** A policy `allow-ingress-traefik` permitia ingress do namespace `traefik`. Mas o Traefik roda no namespace `platform-system`. O match de namespace era `kubernetes.io/metadata.name: traefik` — deveria ser `platform-system`.

**Sintoma:** Traefik log: `subset not found for loopzero-tn-loopzero-production/go-api`. O pod estava Running e Ready, mas o Traefik não conseguia alcançá-lo.

---

### Q: O que é condition-gated reconciliation e por que causa problemas?

**A:** O TenantController pula steps cujas conditions já estão `True` no status (ex: `NetworkPolicyReady=True` → pula `reconcileNetworkPolicies`). Isso é correto para performance, mas significa que **corrigir o código não corrige recursos existentes** — a condition já está True, o step não re-executa.

**Fix:** Para forçar re-reconciliação após um fix de código:
```bash
kubectl -n platform-system patch tenant tn-loopzero --type=merge --subresource=status \
  -p '{"status":{"conditions":[{"type":"NetworkPolicyReady","status":"False","reason":"Reset","message":"Force re-reconciliation","lastTransitionTime":"2026-01-01T00:00:00Z","observedGeneration":0}]}}'
```

---

## API (Build + Deploy Flow)

### Q: Qual é o fluxo completo de um `loopzero deploy`?

```
CLI (loopzero deploy)
  → POST /api/v1/source/upload (multipart: git bundle + loopzero.yaml)
  → POST /api/v1/builds (submete build, advisory lock, idempotency-key)
  → GET /api/v1/builds/{id}/stream (SSE — acompanha progresso)

API (BuildOrchestrator)
  → Cria Argo Workflow no build cluster (namespace build-{tenantID})
  → BuildReconcileLoop polls Workflow CRDs a cada 5s

Argo Workflow (build cluster)
  → clone-repo → validate → security-scan → install-deps → build-apps → generate-dockerfile → build-image (Kaniko)

API (ReleaseActivator — 5 fases)
  → Phase 2a: Cria Release (READY)
  → Phase 2b: Gera K8s manifests + push para Gitea gitops repo
  → Phase 2c: Registra resultado do push
  → Phase 2d: Trigger ArgoCD sync + espera health
  → Phase 2e: Release → ACTIVE

ArgoCD
  → Sincroniza manifests do Gitea → runtime cluster
  → Cria Deployment, Service, Certificate, IngressRoute
```

---

### Q: Por que o GitOps pusher comitava no repo errado?

**A:** Dois padrões de nome de repo coexistiam:

| Componente | Padrão | Exemplo |
|-----------|--------|---------|
| API (GitOps pusher) | `loopzero-tenant-{tenantID}` | `loopzero-tenant-tn-loopzero` |
| TenantController (ArgoCD app) | `{tenantID}-gitops` | `tn-loopzero-gitops` |

A API comitava manifests no repo `loopzero-tenant-tn-loopzero`, mas o ArgoCD lia de `tn-loopzero-gitops`. Repos diferentes → ArgoCD nunca via os manifests.

**Fix:** Alinhar API para usar `{tenantID}-gitops`. Mudar em:
- `api/internal/gitops/gitea_pusher.go`
- `api/internal/k8s/tenant_cr_manager.go`

---

### Q: Por que o DAG vazio causava erro no Argo Workflows?

**A:** O `buildOnExitDAG` (finalize) gera tasks de `save-cache` por linguagem. A strategy `custom` não tem cache → 0 tasks. O Argo Workflows rejeita DAGs com 0 tasks: `spec.templates[1].dag.tasks: should have at least 1 items`.

**Fix:** Quando não há tasks de cache, retornar `nil` e não registrar o `onExit` handler no Workflow.

---

### Q: Por que o hostname do IngressRoute tinha muitos subdomínios?

**A:** O formato original era `{app}-{env}.{project}.{tenant}.{domain}` = `go-api-production.go-api.tn-loopzero.example.loopzero.example` (5 níveis). O wildcard DNS `*.dev.loopzero.example` só cobre **1 nível** de subdomínio.

**Fix:** Simplificar para `{app}-{env}--{tenant}.{domain}` = `go-api-production--tn-loopzero.example.loopzero.example` (1 nível). O double-dash `--` separa tenant de app-env sem ambiguidade.

---

## ArgoCD

### Q: Por que o ArgoCD retornava "authentication required" mesmo com credentials?

**A:** Três causas combinadas:

1. **Token inválido:** Num deploy from-scratch, o Gitea é recriado. O token antigo (salvo no K8s Secret) não existe no novo Gitea.

2. **`repo-creds` não recarrega:** ArgoCD tem um [bug conhecido (#7673)](https://github.com/argoproj/argo-cd/issues/7673) — não recarrega Secrets tipo `repo-creds` quando mudam. Precisa de restart.

3. **Tipo errado de Secret:** `repo-creds` é um template (match por URL prefix). `repository` é registro explícito por repo. ArgoCD monitora Secrets `repository` de forma mais confiável.

**Fix definitivo:**
- Validar token Gitea antes de reusar (curl para `/api/v1/user`)
- Criar Secret tipo `repository` (explícito) além do `repo-creds` (template)
- Reiniciar `argocd-repo-server` + `argocd-application-controller` após atualizar Secrets

---

### Q: Qual a diferença entre `repo-creds` e `repository` no ArgoCD?

| Aspecto | `repo-creds` | `repository` |
|---------|-------------|-------------|
| **Label** | `argocd.argoproj.io/secret-type: repo-creds` | `argocd.argoproj.io/secret-type: repository` |
| **Match** | URL prefix (template para vários repos) | URL exato (1 repo) |
| **Reload** | Não confiável (bug #7673) | Confiável |
| **Uso** | Fallback genérico (ex: todos os repos de uma org) | Registro explícito por repo |

**Recomendação:** Usar ambos. `repo-creds` como fallback, `repository` para cada repo que o ArgoCD precisa acessar ativamente.

---

### Q: Por que o ArgoCD precisa de JWT e não a password diretamente?

**A:** O ArgoCD server aceita authentication via JWT Bearer token na API REST. A password do admin é usada para gerar o JWT via `POST /api/v1/session`. O JWT tem expiração e é o formato correto para chamadas API programáticas.

```bash
# Gerar JWT
JWT=$(curl -sf "http://argocd-server:80/api/v1/session" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"<password>"}' | jq -r '.token')
```

---

## TLS / Certificados

### Q: Por que o cert era self-signed do Traefik?

**A:** O cert-manager + ClusterIssuer existiam apenas no mgmt cluster. O runtime cluster (onde rodam as apps de tenant) só tinha Traefik instalado — sem cert-manager. O IngressRoute com `tls: {}` usava o cert default do Traefik (self-signed).

**Fix:**
1. Instalar cert-manager no runtime cluster via Pulumi
2. Criar ClusterIssuer `letsencrypt-prod` no runtime cluster
3. Manifest renderer gera `Certificate` CR por app (cert-manager emite cert Let's Encrypt)
4. IngressRoute referencia o Secret do cert: `tls.secretName: tls-{app}`

---

### Q: O que é necessário para Let's Encrypt funcionar?

1. **cert-manager** instalado no cluster
2. **ClusterIssuer** com ACME HTTP-01 solver (`class: traefik`)
3. **Certificate CR** com `dnsNames` e `issuerRef` apontando para o ClusterIssuer
4. **DNS** configurado — o domínio deve resolver para o IP do Traefik
5. **Porta 80 aberta** — HTTP-01 challenge precisa de acesso HTTP
6. **IngressRoute** com `tls.secretName` apontando para o Secret do Certificate

---

## CI/CD Pipeline

### Q: O que o bootstrap step do pipeline deve fazer?

O step "Bootstrap platform" roda após os 3 steps de deploy e configura tudo que o Pulumi não gerencia:

1. **Gitea:** Gerar admin token (validar existente antes), criar orgs
2. **ArgoCD:** Gerar JWT via session API (não usar password)
3. **K8s Secrets:** `loopzero-platform-tokens` (API), `gitea-repo-creds` (template), `argocd-repo-*` (por repo)
4. **ArgoCD restart:** Reiniciar repo-server + application-controller para detectar novos Secrets
5. **Database:** Seed user + tenant + membership (idempotente via `ON CONFLICT`)
6. **Tenant CR:** Aplicar via `kubectl apply` (idempotente)
7. **Reconciliação:** Aguardar TenantController atingir phase=Active
8. **Restart serviços:** API + Controller para pegar novos tokens

---

### Q: Por que `github.actor` retorna o nome da org?

**A:** Quando `workflow_dispatch` é triggerado via `gh workflow run` num repo de organização, `github.actor` pode retornar o nome da org em vez do usuário.

**Fix:** Usar `github.triggering_actor` que sempre retorna o humano que triggerou o workflow.

---

### Q: Como validar que o Gitea token ainda é válido?

```bash
# Port-forward e validar
kubectl -n platform-system port-forward svc/gitea-http 13099:3000 &
sleep 3
RESP=$(curl -sf "http://localhost:13099/api/v1/user" -H "Authorization: token $TOKEN")
if echo "$RESP" | grep -q "login"; then
  echo "Token válido"
else
  echo "Token inválido — regenerar"
fi
```

Isso é necessário em deploys from-scratch: o Secret pode ter um token do deploy anterior que não existe no novo Gitea.

---

## Naming Conventions

| Recurso | Padrão | Exemplo |
|---------|--------|---------|
| Tenant ID | `tn-{org}` | `tn-loopzero` |
| User ID (API) | `github-{login}` | `github-paulohenriquevn` |
| Runtime namespace | `loopzero-{tenantID}-{env}` | `loopzero-tn-loopzero-production` |
| Build namespace | `build-{tenantID}` | `build-tn-loopzero` |
| GitOps repo | `{tenantID}-gitops` | `tn-loopzero-gitops` |
| Source repo | `loopzero-source-{tenantID}-{project}` | `loopzero-source-tn-loopzero-go-api` |
| ArgoCD Application | `{tenantID}-{env}` | `tn-loopzero-production` |
| IngressRoute hostname | `{app}-{env}--{tenant}.{domain}` | `go-api-production--tn-loopzero.example.loopzero.example` |
| Helm release (mgmt) | `mgmt-{chart}` | `mgmt-traefik` |
| Helm release (runtime) | `mgmt-{chart}` (compat) | `mgmt-traefik` |
| Pulumi resource | `{role}-{resource}` | `runtime-cert-manager` |
| Certificate Secret | `tls-{app}` | `tls-go-api` |

---

## Checklist de Deploy From-Scratch

Antes de considerar o sistema pronto:

- [ ] Pipeline `up` completa sem erros (3 steps + bootstrap)
- [ ] 5 endpoints HTTPS retornam 200:
  - `https://api.{domain}/api/v1/health`
  - `https://argocd.{domain}`
  - `https://git.{domain}`
  - `https://grafana.{domain}`
  - `https://{app}-{env}--{tenant}.{domain}/` (após deploy de app)
- [ ] `loopzero login --token <GH_TOKEN>` autentica com sucesso
- [ ] `loopzero deploy` completa: upload → build → deploy → app live
- [ ] Cert Let's Encrypt válido (não self-signed Traefik)
- [ ] Tenant CR em phase=Active com todas as conditions True
- [ ] NetworkPolicy permite ingress de `platform-system`
- [ ] ArgoCD sincroniza manifests do Gitea sem erros

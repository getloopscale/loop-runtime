# Pre-Production Gaps — 2026-03-23

> Problemas identificados durante o deploy do dev que devem ser corrigidos
> antes de subir para staging/produção.

## Críticos (Segurança + Funcionamento)

### 1. Credenciais hardcoded no bootstrap.ts

**Arquivos:** `infra/pulumi/src/components/bootstrap.ts`

Credenciais em plain text commitadas no repositório:
- `GITEA_TOKEN: "REDACTED_SECRET_026"`
- `ARGOCD_TOKEN: "REDACTED_SECRET_059"` (e o JWT derivado)
- `MINIO_ACCESS_KEY/SECRET_KEY` (Spaces keys)
- `DIGITALOCEAN_TOKEN` (API token completo)

**Fix:** Usar `pulumi.secret()` ou Kubernetes Secrets gerenciados externamente.
Nunca commitar credenciais em plain text.

### 2. ArgoCD JWT token expira em 24h

A API usa um JWT token do ArgoCD com TTL de 24h. Após expirar, o sync
trigger falha com 401 e o circuit breaker abre.

**Fix:** Criar um ArgoCD API key (não-expirante) ou ArgoCD service account
token via `argocd account generate-token`.

### 3. Gitea bootstrap não automatizado

As orgs "loopzero" e "loopzero-platform" e o admin token foram criados manualmente.
Num redeploy, o Gitea inicia sem orgs e o TenantController falha.

**Fix:** Adicionar um Pulumi Job ou init container que:
1. Cria o admin token via `gitea admin user generate-access-token`
2. Cria as orgs necessárias via API
3. Armazena o token num Secret

### 4. ArgoCD repo credentials não no IaC

O Secret `gitea-repo-creds` (labeled `argocd.argoproj.io/secret-type: repo-creds`)
foi criado manualmente. Sem ele, ArgoCD não autentica no Gitea.

**Fix:** Adicionar no `bootstrap.ts` como `k8s.core.v1.Secret` com as labels corretas.

## Importantes (Operabilidade)

### 5. User onboarding manual

User + tenant + membership foram inseridos via SQL manual. O `loopzero login`
deveria criar automaticamente via GitHub OAuth flow.

**Fix:** Implementar o login flow completo no Sprint 1 (já existe código,
precisa de wiring no deploy cloud).

### 6. CWTs não atualizados automaticamente

Os ClusterWorkflowTemplates são aplicados via `helm template | kubectl apply`
manual. O Pulumi Helm release pode não detectar mudanças.

**Fix:** Garantir que o Pulumi Helm release para CWTs detecta mudanças
no chart local. Pode precisar de hash no values para forçar update.

### 7. Circuit breaker não reseta automaticamente

Após failures, o circuit breaker abre e precisa de UPDATE manual no DB.
O close threshold existe no código mas pode não ser atingido se o
problema original persistir.

**Fix:** Verificar que o circuit breaker close logic funciona end-to-end
e que failures transitórias não travam o sistema permanentemente.

## Menor (DX)

### 8. Namespace naming inconsistente

O manifest generator usa `loopzero-{tenant}-{env}` mas o TenantController
cria `{tenant}-{env}`. O ArgoCD Application destination precisa bater.

**Fix:** Alinhar o naming convention entre `manifests.Render()` e o
TenantController.

### 9. MinIO in-cluster não é usado

O MinIO foi instalado no mgmt cluster mas todo o storage usa Spaces.
O MinIO consome recursos sem servir propósito.

**Fix:** Remover o MinIO do bootstrap ou usá-lo como cache local para
reduzir egress para Spaces.

# Recreate Gaps — O que falta sistematizar para destroy+recreate funcionar

> **Data:** 2026-03-24
> **Status:** 14 gaps identificados, 14 resolvidos (commit a47872e)
> **Criticidade:** RESOLVIDO — `pulumi destroy && pulumi up` deve recriar o sistema completo.

---

## Resumo

O sistema funciona no cluster atual, mas várias correções foram aplicadas **manualmente via kubectl/helm** e não estão codificadas no Pulumi ou nos Helm charts. Se a infra for destruída e recriada, esses recursos não existirão.

---

## Gap 1 — MinIO não instalado pelo Pulumi

**Severidade:** CRITICAL — sem MinIO, source upload falha
**O que foi feito manualmente:** `helm install minio minio/minio` no cluster mgmt
**O que falta:** `installMinIO()` existe em `bootstrap.ts:565-610` mas **nunca é chamada** em `bootstrapMgmtCluster()`

**Fix:**
```typescript
// infra/pulumi/src/components/bootstrap.ts — dentro de bootstrapMgmtCluster()
// Adicionar após installGitea():
const minio = installMinIO(provider, ns);
```

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts`
**Linhas:** ~100-150 (onde outros install* são chamados)

---

## Gap 2 — MinIO credentials Secret não criado pelo Pulumi

**Severidade:** CRITICAL — CWTs no build cluster precisam desse Secret para download de bundles
**O que foi feito manualmente:** `kubectl create secret` em platform-system e argo namespace
**O que falta:** Pulumi deve criar `loopzero-minio-credentials` com:
- `AWS_ACCESS_KEY_ID`: loopzero-admin
- `AWS_SECRET_ACCESS_KEY`: loopzero-minio-dev-2026
- `MINIO_ENDPOINT`: http://minio.platform-system.svc.cluster.local:9000

**Criar em 2 locais:**
1. `platform-system` namespace no mgmt cluster (para API)
2. `argo` namespace no build cluster (para CWTs clone/cache)

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — adicionar após MinIO install

---

## Gap 3 — Gitea token é placeholder no Pulumi

**Severidade:** CRITICAL — sem token real, TenantController não cria repos
**O que foi feito manualmente:** `kubectl exec gitea -- gitea admin user generate-access-token`
**O que falta:** O PlatformBootstrapController foi criado para resolver isso, mas ainda não está wired no Pulumi. O Pulumi cria `loopzero-platform-tokens` com `GITEA_TOKEN=placeholder-set-after-first-deploy`.

**Fix (2 opções):**
1. **Opção A (curto prazo):** Pulumi bootstrap Job executa o token generation (já existe parcialmente em bootstrap.ts:688-704)
2. **Opção B (longo prazo):** PlatformBootstrap CRD é aplicado pelo Pulumi, controller gera o token automaticamente

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts:688-704` (bootstrap Job)

---

## Gap 4 — Novos CRDs não aplicados pelo Pulumi

**Severidade:** CRITICAL — controllers crasham sem os CRDs registrados
**O que foi feito manualmente:** `kubectl apply -f operators/config/crd/bases/loopzero.io_*.yaml`
**6 CRDs novos que precisam ser aplicados:**
- `loopzero.io_platformbootstraps.yaml`
- `loopzero.io_gitrepositorybindings.yaml`
- `loopzero.io_environments.yaml`
- `loopzero.io_loopzeroapplications.yaml`
- `loopzero.io_exposedroutes.yaml`
- `loopzero.io_loopzerobuilds.yaml`
- `loopzero.io_loopzeroreleases.yaml`

**Fix:** Adicionar ao Pulumi bootstrap, após namespace creation:
```typescript
// Apply all CRDs from operators/config/crd/bases/
for (const crdFile of glob("operators/config/crd/bases/*.yaml")) {
  new k8s.yaml.ConfigFile(`crd-${basename(crdFile)}`, { file: crdFile }, { provider });
}
```

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — seção de CRD installation

---

## Gap 5 — RBAC para novos controllers não no Pulumi

**Severidade:** CRITICAL — controllers não conseguem list/watch os novos CRDs
**O que foi feito manualmente:** `kubectl apply` de ClusterRole + ClusterRoleBinding `loopzero-environment-manager`
**O que falta:** O RBAC manifest existe em `operators/config/rbac/platformbootstrap_role.yaml` mas não é aplicado pelo Pulumi.

**Fix:** Adicionar ao Pulumi ou ao Helm chart do tenant-controller:
```typescript
new k8s.yaml.ConfigFile("rbac-environment-manager", {
  file: "operators/config/rbac/platformbootstrap_role.yaml"
}, { provider });
```

Adicionar também RBAC para: environments, loopzeroapplications, exposedroutes, loopzerobuilds, loopzeroreleases.

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — após CRDs

---

## Gap 6 — API memory limit insuficiente

**Severidade:** HIGH — com 512Mi o git push durante source upload é killed
**O que foi feito manualmente:** `kubectl patch deployment loopzero-api` para 1Gi
**O que falta:** Pulumi define 512Mi em bootstrap.ts

**Fix:**
```typescript
// infra/pulumi/src/components/bootstrap.ts — installLoopZeroAPI()
resources: {
  requests: { cpu: "200m", memory: "512Mi" },
  limits: { cpu: "1", memory: "1Gi" },  // was 512Mi
}
```

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — função installLoopZeroAPI

---

## Gap 7 — CWT templates não auto-atualizados

**Severidade:** HIGH — CWTs deployados pelo Pulumi na criação mas nunca atualizados
**O que foi feito manualmente:** `helm template | kubectl apply` para atualizar CWTs
**O que falta:** Quando o código dos CWTs muda (cwt-clone.yaml, cwt-summary.yaml), o Pulumi não re-aplica porque o Helm release version não muda.

**Fix:** Incluir CWT update no CI/CD pipeline (após build-images):
```yaml
# .github/workflows/build-images.yml — adicionar step:
- name: Update CWTs on build cluster
  run: |
    helm template argo-workflows infra/helm/argo-workflows --set minio.insecure=true | \
      kubectl apply -f -
```

**Arquivo:** `.github/workflows/build-images.yml`

---

## Gap 8 — Manifest renderer não gera Certificate CR

**Severidade:** HIGH — deploys ficam com cert self-signed (browser rejeita)
**O que foi feito manualmente:** `kubectl apply` de Certificate CR + patch IngressRoute
**O que falta:** `api/internal/manifests/renderer.go` foi modificado na Fase 0 para remover Certificate CR (LEVE-2 fix). Isso foi um erro — sem Certificate CR, não há Let's Encrypt.

**Fix:** Restaurar geração de Certificate CR no renderer:
```go
// api/internal/manifests/renderer.go — buildIngressResources()
// Gerar Certificate CR:
// - secretName: tls-{appName}
// - issuerRef: letsencrypt-prod (ClusterIssuer)
// - dnsNames: [hostname]
// IngressRoute tls.secretName: tls-{appName}
```

**Arquivo:** `api/internal/manifests/renderer.go`

---

## Gap 9 — ArgoCD namespace mismatch

**Severidade:** MEDIUM — ArgoCD está em `platform-system`, não em `argocd`
**Estado atual:** `ARGOCD_NAMESPACE=platform-system` no deployment do controller
**Impacto:** ArgoCD apps são criadas em `platform-system`, GitRepositoryBindingController cria Secrets em `argocd` (que está vazio)
**O que falta:** Consistência — decidir se ArgoCD fica em `platform-system` ou `argocd` e alinhar todo o código.

**Fix:** Alinhar `ARGOCD_NAMESPACE` no Pulumi bootstrap com o namespace real onde ArgoCD está instalado.

**Arquivos:** `infra/pulumi/src/components/bootstrap.ts`, controller env vars

---

## Gap 10 — CWT clone usa imagem alpine/git mas Pulumi deploya alpine:3.21

**Severidade:** MEDIUM — o CWT no repo usa `alpine/git:2.45.2` mas o Pulumi/Helm pode deployar versão antiga
**O que foi feito:** Fix no arquivo `cwt-clone.yaml` (commitado), CWT atualizado via kubectl
**O que falta:** Garantir que Pulumi/Helm usa o chart atualizado no próximo `pulumi up`

**Fix:** Mesmo que Gap 7 — CWT auto-update no CI/CD

---

## Gap 11 — ARGOCD_NAMESPACE na configuração do Pulumi para controller

**Severidade:** MEDIUM — controller env var `ARGOCD_NAMESPACE` pode estar errada no recreate
**O que falta:** Verificar que o Pulumi passa o namespace correto onde ArgoCD está instalado

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — env vars do tenant-controller

---

## Gap 12 — Bootstrap endpoint não testado no recreate

**Severidade:** MEDIUM — `POST /internal/bootstrap` precisa ser chamado para seed do DB
**O que foi feito manualmente:** O endpoint funciona mas o Pulumi bootstrap Job pode não chamá-lo
**O que falta:** Adicionar step no Pulumi bootstrap Job que chama o endpoint após API estar healthy

**Arquivo:** `infra/pulumi/src/components/bootstrap.ts` — bootstrap Job

---

## Gap 13 — Upload timeout do CLI muito curto

**Severidade:** LOW — default 30s, source upload pode levar mais em conexões lentas
**O que foi feito:** `LOOPZERO_UPLOAD_TIMEOUT=120000` via env var
**O que falta:** Aumentar o default no código de 30s para 120s

**Fix:**
```typescript
// cli/src/config/constants.ts
export const UPLOAD_TIMEOUT_MS = envInt('LOOPZERO_UPLOAD_TIMEOUT', 120000); // was 30000
```

**Arquivo:** `cli/src/config/constants.ts`

---

## Gap 14 — Condition gates parcialmente removidos

**Severidade:** LOW — o commit removeu gates do reconcile principal mas não do spec change detection
**Estado:** O spec change detection code foi removido (correto — com always-reconcile não é necessário)
**O que falta:** Verificar que nenhum outro controller usa condition-gated skip que possa causar o mesmo bug

**Arquivo:** Todos os controllers em `operators/internal/controller/`

---

## Ordem de Resolução Recomendada

| Prioridade | Gaps | Esforço |
|-----------|------|---------|
| **P0 — Sem isso nada funciona** | #1 (MinIO), #2 (MinIO creds), #3 (Gitea token), #4 (CRDs), #5 (RBAC) | 2-3h no Pulumi |
| **P1 — Deploy funciona mas com problemas** | #6 (API memory), #7 (CWT update), #8 (Certificate CR) | 1-2h código |
| **P2 — Cleanup e consistência** | #9 (ArgoCD namespace), #10 (CWT image), #11 (env vars), #12 (bootstrap endpoint) | 1h |
| **P3 — Polish** | #13 (upload timeout), #14 (condition gates) | 30min |

---

## Teste de Validação

Após resolver todos os gaps, o teste de validação é:

```bash
# 1. Destroy tudo
pulumi destroy --yes

# 2. Recriar
pulumi up --yes

# 3. Esperar bootstrap completar (5 min)
# 4. Verificar
kubectl get tenants -A                    # tn-loopzero Active
kubectl get environments -A               # tn-loopzero-production
kubectl get applications -A               # tn-loopzero-production Synced/Healthy
curl https://api.dev.loopzero.example/api/v1/health  # {"status":"ok"}

# 5. Deploy um app
cd examples/node-npm && loopzero deploy

# 6. Acessar no browser COM TLS válido
# https://node-npm-json-test-production.node-npm-json-test.tn-loopzero.example.loopzero.example
# Cadeado verde, sem aviso de segurança
```

Se o passo 6 funcionar com cadeado verde — o sistema está 100% sistematizado.

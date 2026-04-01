# Session Review — 2026-04-01

**Scope:** Full session — dogfood, PR reviews, CI optimization, linter fixes, DigitalOcean deployment
**Commits:** 55+ across develop/dogfood branches
**Files changed:** 138

---

## 1. Intervenções Manuais (devem ser automatizadas)

Tudo abaixo foi feito manualmente durante a sessão e **não deveria ser necessário**:

| # | Intervenção | Onde | Como automatizar |
|---|------------|------|------------------|
| 1 | `doctl registry create loopzero-infra-registry` | DO | O Pulumi DEVE criar o registry. Removido `import:` que impedia criação. |
| 2 | `doctl databases firewalls append` | DO | A firewall rule do DB DEVE ser gerenciada pelo Pulumi, não por `doctl` manual. |
| 3 | `kubectl exec psql -c "UPDATE schema_migrations SET dirty=false"` | Cluster | O init container de migration deve lidar com dirty state automaticamente (force version). |
| 4 | `kubectl delete pod` (múltiplas vezes) | Cluster | Pods stuck devem ser resolvidos pelo deployment strategy, não por delete manual. |
| 5 | `kubectl scale deployment cloudflared --replicas=0` | Cluster | O rollout strategy deve lidar com pods crashing sem intervenção. |
| 6 | `gh workflow run "Build: Container Images"` | GitHub | O build-images deve disparar automaticamente após CI success via `workflow_run`. |
| 7 | `gh workflow run "Infra: Deploy"` | GitHub | Cada push em develop deve disparar deploy automaticamente (já corrigido). |
| 8 | Restart API local (`kubectl rollout restart`) | k3d | O reconcile loop trava após Tilt rebuild — precisa de fix no startup. |

**Regra:** Se precisou de intervenção manual, é um bug na automação.

---

## 2. Bugs Encontrados e Corrigidos

### 2.1 Código (CLI/API/Operators)

| Bug | Severidade | Root Cause | Fix |
|-----|-----------|-----------|-----|
| `loopzero secret set` panic | S1 | Go nil interface trap: `*SecretK8sManager(nil)` passa `!= nil` check | Usar variável de interface |
| `loopzero secret delete --json` trava | S2 | `--json` mode não exige `--yes`, prompt trava em pipe | Check `opts.json \|\| opts.isCI` |
| `loopzero logs` exige `--build` | S2 | Sem inferência do último build | Adicionado `getProjectStatus` fallback |
| `loopzero cancel` retorna `MISSING_IDEMPOTENCY_KEY` | S2 | Rota de cancel com idempotency middleware | Removido (cancel é idempotente por natureza) |
| Commander errors não JSON com `--json` | S2 | `bin/loopzero` não chamava `exitOverride()` | `configureOutput` no program |
| Init truncation sem warning | S3 | Nome truncado para 63 chars sem mensagem | Adicionado `warning` no JSON |
| Data races nos test fakes (5 structs) | S1 (CI) | `sync.Map` / plain map acessados sem mutex | Mutex + accessors thread-safe |
| CWT tests falham com Helm templates | S1 (CI) | YAML parser não entende `{{ .Values }}` | Skip files com Helm syntax |
| Duplicate migration 000011 | S1 (CI) | Dois PRs criaram mesmo número | Renumerado para 000012 |
| Team add permite `role: "owner"` via API | S1 (Security) | CLI bloqueia mas API aceita | Rejeitar `owner` no handler |
| Unused imports/vars/funcs (15 issues) | S3 | Refactors que deixaram dead code | golangci-lint clean |

### 2.2 Infraestrutura (Pulumi/DO)

| Bug | Severidade | Root Cause | Fix |
|-----|-----------|-----------|-----|
| Registry 409 "name already exists" | S0 | `import:` falha quando registry não existe | Removido import, usado `retainOnDelete` |
| Traefik ServiceAccount ownership | S0 | Helm release renomeado (MED-06) | Mantido `mgmt-traefik` como release name |
| Gitea/MinIO unreachable (i/o timeout) | S0 | `default-deny-ingress` sem `allow-gitea`/`allow-minio` | NetworkPolicies adicionadas |
| CA cert double base64 | S0 | `Buffer.from(base64).toString("base64")` | Usar `data["ca.crt"]` diretamente |
| Cloudflared CrashLoopBackOff | S0 | Metrics em 20241, liveness probe em 2000 | `--metrics 0.0.0.0:2000` |
| PostgreSQL egress bloqueado | S0 | NetworkPolicy porta 5432, DO DB usa 25060 | Corrigido para 25060 |
| Kube context name mismatch | S0 | ConfigMap com DOKS names, kubeconfig com scoped names | Alinhado BUILD/RUNTIME para scoped, MGMT para DOKS |
| Registry endpoint sem nome | S1 | `serverUrl` de import não inclui registry name | Construção determinística |
| DB firewall vazio | S0 | Nenhuma trusted source configurada | Adicionado via `doctl` (PRECISA SER PULUMI) |
| Dirty migration flag | S1 | Migration anterior falhou no meio | Limpo via `psql UPDATE` (PRECISA DE AUTO-FIX) |
| Grafana Helm timeout | S2 | Default 5min insuficiente para PVC no DO | Aumentado para 10min |

---

## 3. Problemas de Idempotência da Pipeline

**Requisito:** `pulumi destroy --stack dev && pulumi up --stack dev` deve funcionar sem intervenção manual.

### Bloqueios atuais:

#### 3.1 DB Firewall NÃO gerenciada pelo Pulumi

```
CRÍTICO: A firewall rule do database foi adicionada manualmente via doctl.
Se a infra for destruída e recriada, o DB firewall fica vazio e NENHUM
pod consegue conectar ao PostgreSQL.
```

**Fix necessário:** Adicionar `digitalocean.DatabaseFirewall` resource no Pulumi:

```typescript
new digitalocean.DatabaseFirewall("db-firewall", {
  clusterId: database.id,
  rules: [
    { type: "k8s", value: mgmtCluster.id },
  ],
});
```

#### 3.2 Migration dirty state não é auto-resolvido

```
Se uma migration falha no meio (timeout, crash), o golang-migrate marca
o DB como "dirty" e RECUSA executar qualquer migration subsequente.
Resultado: TODOS os pods API ficam em Init:CrashLoopBackOff.
```

**Fix necessário:** O init container deve rodar `migrate force <version>` antes de `migrate up`:

```yaml
args: ["-path=/migrations", "-database", "$(DATABASE_URL)", "force", "12"]
# Then run up
```

Ou usar um wrapper script que detecta dirty e resolve.

#### 3.3 Container Registry compartilhado entre stacks

```
DO permite apenas 1 registry por conta. Se o stack dev cria e o stack
prod tenta criar, dá 409. Se o stack dev destrói, o prod perde o registry.
```

**Estado atual:** `retainOnDelete: true` impede deleção. Mas um `pulumi destroy` seguido de `pulumi up` pode ter estado inconsistente.

**Fix necessário:** O registry deve ser um recurso compartilhado gerenciado por um stack dedicado (`shared` ou `global`) que nunca é destruído.

#### 3.4 Helm release names com legado

```
O Traefik usa "mgmt-traefik" como release name por backward compat.
Em um deploy fresh (destroy + recreate), o nome correto seria
"runtime-traefik" mas o código hardcoda "mgmt-traefik".
```

**Fix necessário:** Usar `${role}-traefik` consistentemente. Em deploy fresh não há conflito de annotations.

#### 3.5 Scoped kubeconfigs dependem de ServiceAccount token

```
Os scoped kubeconfigs são criados a partir de ServiceAccount tokens.
Se o cluster é destruído e recriado, os tokens são invalidados.
O Pulumi precisa recriar os kubeconfigs a cada deploy.
```

**Estado atual:** Funciona porque o Pulumi recria os Secrets. Mas se o state está inconsistente, os tokens antigos ficam nos Secrets.

---

## 4. Code Smells Identificados

| # | Smell | Localização | Impacto |
|---|-------|------------|---------|
| 1 | **"github-" prefix hardcoded 3x** | `api/internal/routes/teams.go:196,268` + `db/team_store.go` | DRY violation. Se o formato mudar, 3 locais precisam update |
| 2 | **StubEnvironmentManager em código de produção** | `api/internal/routes/environments.go:218-253` | Dead code no package de routes. Deveria estar em test file |
| 3 | **reservedEnvironmentNames inconsistente** | API tem "argocd"/"monitoring", CLI tem só 3 nomes | Mismatch entre validações client/server |
| 4 | **Helm release name hardcoded** | `bootstrap-shared.ts:212 name: "mgmt-traefik"` | Legado que impede deploy limpo |
| 5 | **DB port hardcoded** | Era 5432 em NetworkPolicy, DO usa 25060 | Deveria vir da config, não hardcoded |
| 6 | **Reconcile loop trava após Tilt rebuild** | API background goroutines | O startup do API não garante que goroutines estão processando |
| 7 | **applicationSetGVR usa v1alpha1** | `operators/internal/argocd/applicationset.go` | Versão pode mudar com upgrade do ArgoCD |
| 8 | **Deep health timeout sem retry** | API `/health/deep` | Falha com 503 se o pool ainda está conectando — deveria ter retry |

---

## 5. Erros Cometidos Durante a Sessão

| # | Erro | Consequência | Lição |
|---|------|-------------|-------|
| 1 | Afirmei que ESO/Secrets "nunca existiu" | Estava implementado desde H6 | **Sempre verificar com evidência antes de afirmar** |
| 2 | Afirmei que `loopzero status` não estava wired | A rota existia, testei com projeto deletado | **Testar com dados válidos** |
| 3 | Usei `import: spec.name` no registry | Falha quando não existe | **Testar destroy + recreate antes de mergear** |
| 4 | Adicionei `forceUpdate` no Traefik | Não resolve replace (delete+create) | **Entender a diferença entre upgrade e replacement no Pulumi** |
| 5 | Adicionei `aliases` para Traefik | Não funciona quando o resource não existe no state | **Aliases resolvem renames, não creates** |
| 6 | Mudei MGMT_KUBE_CONTEXT para "mgmt" | O mgmt kubeconfig não é scoped (usa DOKS admin) | **Verificar TODAS as variações antes de generalizar** |
| 7 | Múltiplos commits para o mesmo fix | Registry teve 4 tentativas | **Diagnosticar completamente antes de commitar** |
| 8 | Não percebi o DB port 25060 | Perdi tempo investigando firewall | **Verificar NetworkPolicy + porta do serviço juntos** |

---

## 6. Melhorias Prioritárias para Pipeline Idempotente

### P0 — Bloqueia destroy+recreate

1. **DB Firewall no Pulumi** — Adicionar `digitalocean.DatabaseFirewall` com mgmt cluster como trusted source
2. **Migration auto-recovery** — Wrapper script no init container que faz `force` antes de `up`
3. **NetworkPolicy completa desde o início** — As policies `allow-gitea`, `allow-minio` devem estar no código original, não adicionadas depois
4. **DB port da config** — Ler porta do DB da connection string ou do Pulumi output, não hardcodar

### P1 — Melhora confiabilidade

5. **Registry em stack compartilhado** — Mover para um stack `global` que nunca é destruído
6. **Helm release names consistentes** — Usar `${role}-traefik` sem legado
7. **Build images auto-trigger** — Verificar que `workflow_run` dispara corretamente
8. **Scoped kubeconfig tests** — Testar que o kubeconfig gerado é válido (parsear, verificar context, verificar CA)

### P2 — Qualidade e DX

9. **Deep health com retry** — O `/health/deep` deve ter retry com backoff
10. **Reconcile loop recovery** — O API deve detectar que goroutines não estão processando e restart
11. **Pipeline dry-run** — Comando `task infra:test` que faz `pulumi preview` localmente
12. **Dogfood remoto** — Executar dogfood skill contra o ambiente DO, não apenas local

---

## 7. Métricas da Sessão

| Métrica | Valor |
|---------|-------|
| Commits | 55+ |
| PRs revisados | 8 (#12-#19) |
| Bugs encontrados | 22 (11 código + 11 infra) |
| Bugs corrigidos | 22 |
| Security issues encontrados | 2 (privilege escalation, nil interface panic) |
| Security issues corrigidos | 2 |
| Data races corrigidos | 5 fakes com mutex |
| Lint issues resolvidos | 16 (API 11 + operators 4 + CLI 1) |
| Intervenções manuais | 8 (devem virar automação) |
| Tentativas de fix para registry | 4 (import → protect → retainOnDelete → final) |
| Deploys falhados no DO antes de green | 7 |
| Tests no final | 747 CLI + all Go passing |
| Dogfood runs | 8 |

---

## 8. Conclusão

O sistema está funcional em DigitalOcean com todas as pipelines green. Mas a pipeline **não é idempotente** — um `pulumi destroy && pulumi up` vai falhar por causa de:
1. DB firewall não gerenciada pelo Pulumi
2. Migration dirty state não auto-resolvido
3. Registry compartilhado sem stack dedicado

**Próxima sessão deve focar em:** Resolver os 4 items P0 para tornar a pipeline idempotente. Depois, executar um destroy+recreate completo para validar.

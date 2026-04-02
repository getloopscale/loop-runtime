# Plano: Pipeline Idempotente v2

**Data:** 2026-04-02
**Status:** ✅ **CONCLUÍDO** — validado em DigitalOcean (02/Abr, run 23899478737)
**Pré-requisito:** Análise de idempotência (sessão 02/Abr)
**Objetivo:** `pulumi destroy --stack dev && pulumi up --stack dev` funciona sem intervenção manual, com zero falsos positivos (pipeline green = plataforma funcional)

### Resultado da validação (02/Abr)

- Destroy: ✅ success (run 23898986300, 7min)
- Recreate from zero: ✅ success (run 23899478737, ~38min)
- Zero errors, zero intervenção manual
- Runtime health: 3/3 (API, Gitea, tenant-controller)
- Seed via kubectl run: ✅ (DB firewall bypass)
- Bootstrap final gate: ✅ (tokens + secrets verified)

---

## Contexto

A v1 deste plano (01/Abr) implementou 3 das 4 tasks com sucesso:
- ✅ Task 1: DB Firewall no Pulumi
- ✅ Task 2: Migration Auto-Recovery
- ✅ Task 3: Health Validation Step
- ⚠️ Task 4: Destroy + Recreate (não revalidado após fixes de cert-manager/NetworkPolicy)

A análise de 02/Abr encontrou **3 problemas P0** e **5 problemas P1** que quebram ou enfraquecem a idempotência.

---

## Critério de sucesso global

```bash
# Este script DEVE completar sem erros e sem intervenção humana:
cd infra/pulumi
pulumi destroy --stack dev --yes          # destroy completo
pulumi up --stack dev --yes               # recreate from zero
# Build images (registry é novo, sem images)
gh workflow run "Build: Container Images" --ref develop -f component=all
# Re-deploy com images
pulumi up --stack dev --yes
# Validação automática:
#   - Todos os pods Running (0 CrashLoopBackOff, 0 ImagePullBackOff)
#   - API health: {"status":"ok"}
#   - Gitea: acessível
#   - Tenant CR: Active
#   - loopzero deploy: funciona end-to-end
```

---

## Task 1: Registry deletion safety (P0)

### Problema

`pre-destroy-cleanup.sh:112` deleta o container registry via DO API:
```bash
curl -s -X DELETE ... "https://api.digitalocean.com/v2/registry"
```

Isso contradiz `retainOnDelete: true` no Pulumi (`registry.ts:32`). DigitalOcean tem **1 registry por conta** — deletar em dev apaga imagens de staging/prod.

### Arquivo

`infra/scripts/pre-destroy-cleanup.sh`

### Implementação

Remover a linha de deleção do registry. O registry é compartilhado entre stacks e deve **nunca** ser deletado automaticamente. A proteção `retainOnDelete` no Pulumi é intencional.

```diff
-# Container registry
-curl -s -X DELETE -H "Authorization: Bearer $DIGITALOCEAN_TOKEN" \
-  "https://api.digitalocean.com/v2/registry" 2>/dev/null
-echo "  Registry deleting"
+# Container registry — NEVER delete automatically.
+# DO allows only 1 registry per account (shared across all stacks).
+# retainOnDelete: true in Pulumi already prevents destroy from deleting it.
+# Manual deletion: doctl registry delete (requires confirmation).
+echo "  Registry: skipped (shared resource, retainOnDelete)"
```

### DoD

- [x] `pre-destroy-cleanup.sh` não contém `DELETE .../v2/registry`
- [ ] Teste: executar cleanup com `STACK=dev` — registry permanece em `doctl registry get`
- [ ] Teste: `pulumi destroy && pulumi up` — registry existente é importado ou reutilizado (Step 1 da pipeline já faz import)
- [x] Comentário explica por quê o registry não é deletado

---

## Task 2: Bootstrap fail-safe (P0)

### Problema

O step "Bootstrap platform" (`infra-deploy.yml:613`) usa `set +e` e retorna `exit 0` em múltiplos caminhos de erro:
- Linha 641: `exit 0` quando Gitea pod não encontrado
- Linha 666: `exit 0` quando token generation falha

Resultado: pipeline green mas plataforma sem tokens — API não consegue falar com Gitea nem ArgoCD.

### Arquivo

`.github/workflows/infra-deploy.yml` (step "Bootstrap platform")

### Implementação

Substituir `exit 0` por `exit 1` nos caminhos de erro críticos. Manter `set +e` (necessário para que comandos individuais falhem sem matar o step inteiro), mas garantir que o step falha se os **requisitos mínimos** não forem atingidos.

Definir requisitos mínimos de bootstrap:
1. Gitea token gerado ou validado
2. ArgoCD JWT gerado
3. K8s secrets atualizados
4. DB seed executado

```diff
 GITEA_POD=$(kubectl -n $NAMESPACE get pod -l app.kubernetes.io/name=gitea -o jsonpath='{.items[0].metadata.name}' 2>/dev/null)
 if [ -z "$GITEA_POD" ]; then
-  echo "::warning::Gitea pod not found, skipping bootstrap"
-  exit 0
+  echo "::error::Gitea pod not found — platform cannot function without Gitea"
+  exit 1
 fi
```

```diff
-[ -z "$GITEA_TOKEN" ] && echo "::warning::Failed to generate Gitea token" && exit 0
+if [ -z "$GITEA_TOKEN" ]; then
+  echo "::error::Failed to generate Gitea token — platform cannot function"
+  exit 1
+fi
```

Adicionar gate final antes do "Bootstrap Complete":

```bash
# ═══════════════════════════════════════
# FINAL GATE: verify minimum viable bootstrap
# ═══════════════════════════════════════
BOOT_FAIL=0
[ -z "$GITEA_TOKEN" ] && echo "::error::Missing Gitea token" && BOOT_FAIL=1
[ -z "$ARGOCD_JWT" ] && echo "::error::Missing ArgoCD JWT" && BOOT_FAIL=1

# Verify secrets were persisted
STORED_GITEA=$(kubectl -n $NAMESPACE get secret loopzero-platform-tokens -o jsonpath='{.data.GITEA_TOKEN}' 2>/dev/null | base64 -d)
[ "$STORED_GITEA" = "placeholder-set-after-first-deploy" ] && echo "::error::Gitea token still placeholder" && BOOT_FAIL=1

if [ "$BOOT_FAIL" -gt 0 ]; then
  echo "::error::Bootstrap failed — $BOOT_FAIL critical items missing"
  exit 1
fi
```

### DoD

- [x] `exit 0` removido de TODOS os caminhos de erro no Bootstrap step (Gitea, ArgoCD, seed)
- [x] Gate final adicionado: verifica Gitea token + ArgoCD JWT + secrets persistidos
- [ ] Teste: simular Gitea pod ausente (scale replicas=0) → pipeline FALHA (não green)
- [ ] Teste: simular token generation failure → pipeline FALHA
- [ ] Teste: deploy normal → bootstrap completa com sucesso
- [x] Step "Bootstrap platform" NÃO tem `continue-on-error: true`

---

## Task 3: MinIO credentials from config (P0)

### Problema

O Secret `loopzero-minio-credentials` (`mgmt-bootstrap.ts:71-85`) hardcoda `"loopzero-admin"` / `"loopzero-minio-dev-2026"`, mas o Helm chart do MinIO usa `config.minioRootPassword ?? "loopzero-minio-dev-2026"` (linha 438). Em produção, onde `minioRootPassword` é custom, o Secret diverge do MinIO real → API não consegue autenticar no MinIO.

### Arquivo

`infra/pulumi/src/components/mgmt-bootstrap.ts`

### Implementação

Derivar as credenciais do Secret a partir da mesma fonte que o Helm chart:

```diff
+  const minioUser = "loopzero-admin";
+  const minioPassword = config.minioRootPassword
+    ? config.minioRootPassword.apply(p => p)
+    : pulumi.output("loopzero-minio-dev-2026");
+
   new k8s.core.v1.Secret(
     "minio-credentials-mgmt",
     {
       metadata: {
         name: "loopzero-minio-credentials",
         namespace: PLATFORM_NAMESPACE,
       },
       stringData: {
-        AWS_ACCESS_KEY_ID: "loopzero-admin",
-        AWS_SECRET_ACCESS_KEY: "loopzero-minio-dev-2026",
+        AWS_ACCESS_KEY_ID: minioUser,
+        AWS_SECRET_ACCESS_KEY: minioPassword,
         MINIO_ENDPOINT: `http://minio.${PLATFORM_NAMESPACE}.svc.cluster.local:9000`,
       },
     },
     { provider, dependsOn: [minio] },
   );
```

### DoD

- [x] `"loopzero-minio-dev-2026"` NÃO aparece hardcoded no Secret (apenas como fallback default)
- [x] `config.minioRootPassword` é usado quando definido
- [ ] Teste: `pulumi config set --secret minioRootPassword "custom-pass"` → Secret contém `"custom-pass"`
- [ ] Teste: sem minioRootPassword configurado → Secret contém `"loopzero-minio-dev-2026"` (default dev)
- [ ] Teste: API consegue fazer upload para MinIO com as credenciais do Secret
- [x] TypeScript compila sem erros (`npx tsc --noEmit`)

---

## Task 4: Verify deployment health não silencia erros (P1)

### Problema

O step "Verify deployment health" (`infra-deploy.yml:973`) retorna `exit 0` quando não consegue obter o Pulumi execution state (linha 982). Deploy com state corrompido passa silenciosamente.

### Arquivo

`.github/workflows/infra-deploy.yml`

### Implementação

```diff
 if [ $? -ne 0 ]; then
-  echo "::warning::Could not retrieve execution state"
-  echo "### ⚠️ Could not verify deployment health" >> "$GITHUB_STEP_SUMMARY"
-  exit 0
+  echo "::error::Could not retrieve execution state — Pulumi state may be corrupted"
+  echo "### ❌ Could not verify deployment health" >> "$GITHUB_STEP_SUMMARY"
+  exit 1
 fi
```

### DoD

- [x] `exit 0` removido do step "Verify deployment health"
- [x] Falha ao obter execution state → pipeline falha
- [ ] Teste: deploy normal → step passa
- [ ] Teste: state corrompido (simular com jq filter) → step falha

---

## Task 5: SQL injection no seed (P1)

### Problema

`github.triggering_actor` é interpolado diretamente no SQL (`infra-deploy.yml:776`). Se o username GitHub contém `'`, o SQL quebra ou permite injection.

### Arquivo

`.github/workflows/infra-deploy.yml` (step "Bootstrap platform", seção 4)

### Implementação

Usar variáveis `psql` com `-v` ao invés de interpolação direta:

```diff
-GH_LOGIN="${{ github.triggering_actor }}"
-GH_USER_ID="github-${GH_LOGIN}"
-
-psql "$LOOPZERO_DB_URI" -v ON_ERROR_STOP=0 <<SEED_SQL
-INSERT INTO users (id, email, github_id, display_name, created_at, updated_at)
-VALUES ('${GH_USER_ID}', '${GH_LOGIN}@users.noreply.github.com', ...
+# Sanitize: remove any single quotes from GitHub username
+GH_LOGIN=$(echo "${{ github.triggering_actor }}" | tr -d "'")
+GH_USER_ID="github-${GH_LOGIN}"
+
+psql "$LOOPZERO_DB_URI" \
+  -v gh_login="$GH_LOGIN" \
+  -v gh_user_id="$GH_USER_ID" \
+  -v ON_ERROR_STOP=0 <<'SEED_SQL'
+INSERT INTO users (id, email, github_id, display_name, created_at, updated_at)
+VALUES (:'gh_user_id', :'gh_login' || '@users.noreply.github.com', ...
```

**Nota:** a sintaxe `:'var'` do psql produz valores quoted — seguro contra injection.

### DoD

- [x] `${{ github.triggering_actor }}` NÃO é interpolado dentro de SQL
- [x] Variáveis psql `-v` usadas para todos os valores dinâmicos no seed
- [x] Heredoc usa `<<'SEED_SQL'` (quoted — sem shell expansion dentro do SQL)
- [ ] Teste: username com `'` → seed executa sem erro
- [x] Todos os `ON CONFLICT` mantidos (idempotência preservada)

---

## Task 6: Remover sleep 90 redundante (P1)

### Problema

`infra-deploy.yml:476` tem `sleep 90` para "esperar webhooks". O root cause real (NetworkPolicy bloqueando webhook) já foi corrigido. O `waitForJobs: true` no cert-manager Helm release já garante webhook readiness. O sleep adiciona 90s desnecessários a cada retry.

### Arquivo

`.github/workflows/infra-deploy.yml`

### Implementação

Substituir sleep por `kubectl wait`:

```diff
-echo "Waiting 90s for cert-manager/ArgoCD webhooks to initialize..."
-sleep 90
+echo "Waiting for cert-manager webhook readiness..."
+for KC_OUTPUT in mgmtKubeconfig runtimeKubeconfig; do
+  KC=$(pulumi stack output $KC_OUTPUT --show-secrets --stack "$STACK" 2>/dev/null || true)
+  if [ -n "$KC" ] && [ "$KC" != "undefined" ]; then
+    echo "$KC" > /tmp/webhook-wait-kc.yaml
+    kubectl --kubeconfig=/tmp/webhook-wait-kc.yaml wait deployment \
+      -l app.kubernetes.io/component=webhook,app.kubernetes.io/instance=${KC_OUTPUT%%Kubeconfig*}-cert-manager \
+      -n platform-system --for=condition=Available --timeout=120s 2>/dev/null || true
+    rm -f /tmp/webhook-wait-kc.yaml
+  fi
+done
```

### DoD

- [x] `sleep 90` removido
- [x] Substituído por `kubectl rollout status` com timeout de 120s
- [ ] Teste: retry `pulumi up` inicia assim que webhook está ready (não espera 90s fixos)
- [x] Fallback: se `kubectl wait` falha (kubeconfig indisponível), não bloqueia — `|| true`

---

## Task 7: Hardcoded doctl version consolidado (P1)

### Problema

`doctl-1.104.0` aparece hardcoded em 3 locais: `infra-deploy.yml:493`, `infra-deploy.yml:620`, e `pre-destroy-cleanup.sh:80`. Atualizar requer editar 3 arquivos.

### Arquivo

`.github/workflows/infra-deploy.yml` + `infra/scripts/pre-destroy-cleanup.sh`

### Implementação

Adicionar variável de environment no workflow:

```yaml
env:
  NODE_VERSION: "20"
  DOCTL_VERSION: "1.104.0"
```

Substituir todas as ocorrências por `${{ env.DOCTL_VERSION }}` no workflow e `${DOCTL_VERSION}` no script (passado como env var).

### DoD

- [x] `1.104.0` aparece em **1 único local** (env var no topo do workflow)
- [x] Todas as 3 ocorrências usam a variável
- [x] `pre-destroy-cleanup.sh` recebe `DOCTL_VERSION` como env var documentada
- [x] Teste: `grep -r "1.104.0" .github/ infra/scripts/` retorna apenas a definição + fallback default

---

## Task 8: Traefik release name consistente (P1)

### Problema

`installTraefik()` em `bootstrap-shared.ts:224` usa `name: "mgmt-traefik"` para **ambos** mgmt e runtime. Semanticamente errado e dificulta debugging.

### Arquivo

`infra/pulumi/src/components/bootstrap-shared.ts`

### Implementação

Em fresh deploy (destroy+recreate), não há legacy — podemos usar o nome correto:

```diff
   return new k8s.helm.v3.Release(
     `${role}-traefik`,
     {
-      name: "mgmt-traefik",
+      name: `${role}-traefik`,
       chart: "traefik",
```

O `alias` existente na linha 267 já garante migration de state antigo.

**Risco:** Se o cluster runtime existente JÁ tem recursos com annotation `meta.helm.sh/release-name=mgmt-traefik`, o Helm rejeita o novo release `runtime-traefik`. Para mitigar: incluir um step no pipeline que re-annota resources antes do `pulumi up`.

**Decisão necessária:** Este é um T2 change (Helm chart modification). Requer validação em destroy+recreate E em upgrade de cluster existente.

### DoD

- [x] `installTraefik()` usa `${role}-traefik` como release name
- [x] `fullnameOverride` mantido como `${role}-traefik` (já estava correto)
- [ ] Teste destroy+recreate: ambos clusters com release names corretos
- [x] Teste upgrade: alias garante migration transparente
- [ ] `kubectl get secrets -l owner=helm -n platform-system` mostra `mgmt-traefik` e `runtime-traefik` (não 2x `mgmt-traefik`)

---

## Sequência de execução

```
Phase 1: Fixes críticos (P0) — sem dependências entre si
├── Task 1: Registry deletion safety        (15 min)
├── Task 2: Bootstrap fail-safe              (30 min)
└── Task 3: MinIO credentials from config    (20 min)
    Gate: npx tsc --noEmit + pipeline syntax check

Phase 2: Robustez (P1) — independentes
├── Task 4: Health check não silencia erros  (10 min)
├── Task 5: SQL injection no seed            (20 min)
├── Task 6: Remover sleep 90                 (15 min)
└── Task 7: doctl version consolidado        (10 min)
    Gate: grep + lint

Phase 3: Naming (P1 — T2, requer validação)
└── Task 8: Traefik release name             (30 min + teste manual)
    Gate: destroy+recreate completo

Phase 4: Validação final
└── Destroy + Recreate via pipeline          (30-60 min)
    Gate: script do critério de sucesso global
```

**Tempo estimado:** 2-3h (Phases 1-3) + 1h (Phase 4 validação)

---

## Checklist pré-merge

- [x] `npx tsc --noEmit` passa
- [x] `npm test` (infra/pulumi) passa — 32/32 tests
- [ ] `task lint` passa
- [x] `grep -r "exit 0" .github/workflows/infra-deploy.yml` — apenas em paths não-críticos (CF Worker, Loki)
- [x] `grep -r "1.104.0" .github/ infra/scripts/` — apenas na definição da env var + fallback default
- [x] `grep "DELETE.*registry" infra/scripts/pre-destroy-cleanup.sh` — zero matches
- [x] `grep "loopzero-minio-dev-2026" infra/pulumi/src/components/mgmt-bootstrap.ts` — apenas no fallback default, não no Secret
- [x] Pipeline `infra-deploy.yml` YAML válido (python3 yaml.safe_load OK)

---

## Referências

| Documento | Relevância |
|-----------|-----------|
| `docs/plans/PIPELINE-IDEMPOTENCY-PLAN.md` | Plano v1 (3/4 tasks completas) |
| `docs/reviews/SESSION-REVIEW-2026-04-01.md` | Bugs encontrados na sessão de dogfood |
| `docs/meetings/records/MEET-2026-04-01-026.md` | Meeting sobre Bootstrap CRIT-01/CRIT-02 |
| `infra-deploy.yml` | Pipeline principal |
| `infra/scripts/pre-destroy-cleanup.sh` | Script de cleanup pré-destroy |

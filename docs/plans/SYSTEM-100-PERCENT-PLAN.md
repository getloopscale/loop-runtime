# Plano: Sistema 100% Funcional

**Data:** 2026-04-01
**Origem:** Dogfood Full (Skill v2)
**Objetivo:** Corrigir todos os achados F-01 a F-05 do último dogfood

---

## F-01: `loopzero secret set` panic — nil pointer on SecretK8sManager (S1 — P0)

### Root Cause

Go nil interface trap. Em `main.go`:

```go
secretK8sMgr, err := k8s.NewSecretK8sManager(...)
if err != nil {
    logger.Warn("secret K8s manager not available — ESO sync disabled", "error", err)
    // secretK8sMgr is *k8s.SecretK8sManager(nil) — TYPED nil
}
// ...
SecretK8sSync: secretK8sMgr, // typed nil passes non-nil interface check!
```

Em `server.go:203`: `if cfg.SecretK8sSync != nil` retorna `true` para typed nil.
Em `secrets.go:189`: `if h.k8sSync != nil` retorna `true` para typed nil.
O `CreateSecret(0x0, ...)` faz panic: nil pointer dereference.

### Fix

**Arquivo:** `api/cmd/main.go`

Usar variável de interface para evitar typed nil:

```go
var secretK8sSync routes.SecretK8sSync // interface type — nil by default
REDACTED_SECRET_058, err := k8s.NewSecretK8sManager(...)
if err != nil {
    logger.Warn(...)
} else {
    secretK8sSync = REDACTED_SECRET_058 // only assign if non-nil
}
// ...
SecretK8sSync: secretK8sSync, // now correctly nil when creation failed
```

### DoD

**TDD (test primeiro, depois implementação):**

1. **Teste RED:** Teste unitário em `api/tests/unit/` que cria um `SecretHandler` com `k8sSync = nil` e chama `Set()` — deve NÃO panic e retornar 200
2. **Teste RED:** Teste unitário que cria `SecretHandler` com `k8sSync = (*k8s.SecretK8sManager)(nil)` (typed nil) — deve NÃO panic
3. **Implementar** fix em `main.go`
4. **Testes GREEN**

**Validação E2E (todos devem passar):**

5. `loopzero secret set DOGFOOD_KEY=dogfood_value --json` → exit 0, JSON contém `{"name":"DOGFOOD_KEY","created":true}`
6. `loopzero secret list --json` → exit 0, JSON contém array com entry onde `name == "DOGFOOD_KEY"`
7. `loopzero secret set DOGFOOD_KEY=updated_value --json` → exit 0, JSON contém `{"name":"DOGFOOD_KEY","created":false}` (upsert, não criação)
8. API logs nos últimos 30s: **zero** linhas contendo `panic`
9. `go test ./api/...` → 0 failures
10. **Valor NUNCA aparece no output:** `loopzero secret list --json | grep -c "dogfood_value"` → 0

**Critério de FAIL:** Se qualquer item 5-10 falhar, o fix NÃO está correto.

---

## F-02: `loopzero secret delete` requer confirmação interativa em pipe (S2 — P1)

### Root Cause

`SecretDeleteCommand` pede confirmação quando `--yes` não é passado.
Em `--json` mode, stdin está em pipe — prompt fica pendurado até timeout.
O `--yes` flag existe mas `--json` não o implica nem o exige.

### Fix

**Arquivo:** `cli/src/commands/secret.ts` — método do delete

Quando `--json` está ativo e `--yes` não foi passado, retornar erro imediato:

```typescript
if (opts.json && !opts.yes) {
    return this.exitWithError(opts,
        'Use --yes to skip confirmation in JSON mode. Example: loopzero secret delete KEY --yes --json',
        1, 'CONFIRMATION_REQUIRED');
}
```

### DoD

**TDD:**

1. **Teste RED:** `SecretDeleteCommand.execute({json: true, yes: false})` → exit 1, code `CONFIRMATION_REQUIRED`
2. **Teste RED:** `SecretDeleteCommand.execute({json: true, yes: true})` → exit 0 (delete procede)
3. **Implementar** fix
4. **Testes GREEN**

**Validação E2E (pré-requisito: F-01 fix aplicado, secret DOGFOOD_KEY existe):**

5. `loopzero secret delete DOGFOOD_KEY --json` (sem --yes) → exit 1, JSON: `{"error":"Use --yes to skip confirmation in JSON mode...","exit_code":1,"code":"CONFIRMATION_REQUIRED"}`
6. `loopzero secret delete DOGFOOD_KEY --yes --json` → exit 0, JSON com confirmação
7. `loopzero secret list --json` → DOGFOOD_KEY ausente do array
8. `loopzero secret delete DOGFOOD_KEY --yes --json` (segundo delete) → exit code definido (1 ou 3), erro claro "not found"
9. **Human mode:** `loopzero secret delete NAO_EXISTE --yes` → mensagem sem termos internos
10. `cd cli && npm test` → 0 failures

**Critério de FAIL:** Se item 5 retornar exit 0 (deleta sem confirmação em JSON mode), o fix está errado.

---

## F-03: `loopzero logs` sem `--build` retorna erro (S2 — P2)

### Root Cause

`LogsCommand` exige `--build <id>` como opção obrigatória.
Usuário natural espera ver logs do último build sem saber o ID.

### Fix

**Arquivo:** `cli/src/commands/logs.ts`

Quando `--build` não é fornecido:
1. Ler `loopzero.yaml` para obter project name
2. Chamar `GET /api/v1/projects/{name}/status` para obter `last_build.id`
3. Usar esse ID automaticamente

### DoD

**TDD:**

1. **Teste RED:** `LogsCommand.execute({})` (sem build, com loopzero.yaml) → exit 0, output contém `build_id`
2. **Teste RED:** `LogsCommand.execute({})` sem loopzero.yaml → exit 1, code `MISSING_CONFIG`
3. **Teste RED:** `LogsCommand.execute({})` com projeto sem builds → exit 1, code `NO_BUILDS`
4. **Implementar** inferência de último build
5. **Testes GREEN**

**Validação E2E (pré-requisito: projeto com deploy completado):**

6. `loopzero logs --json` (sem --build) → exit 0, JSON contém campo `build_id` não-vazio
7. `loopzero logs --build <id-explicito> --json` → exit 0, mesmo resultado (backward compat)
8. `cd /tmp/sem-projeto && loopzero logs --json` → exit 1, `MISSING_CONFIG`
9. `cd cli && npm test` → 0 failures

**Critério de FAIL:** Se item 6 retornar exit 1 com `required option '--build'`, o fix não foi aplicado.

---

## F-04: `loopzero logs` retorna 0 linhas de log (S2 — P2, infra)

### Root Cause

Loki está rodando (`loki-0` Running), mas os logs do build podem não estar sendo coletados.
Possíveis causas:
- Promtail não está no build cluster (Argo Workflow pods não são scraped)
- Labels do Promtail não matcham os labels dos workflow pods
- Query LogQL na API usa labels errados

### Investigação (não é code fix)

**Arquivo:** `api/internal/loki/client.go` — verificar a query LogQL usada

### DoD

**Diagnóstico (todos devem ser respondidos):**

1. Promtail está instalado no build cluster? `kubectl --context k3d-loopzero-build get pods -n monitoring | grep promtail`
2. Promtail está coletando logs? `kubectl --context k3d-loopzero-build logs -n monitoring -l app.kubernetes.io/name=promtail --tail=5`
3. Qual query LogQL a API usa? `grep -n "LogQL\|query\|loki" api/internal/loki/client.go | head -10`
4. A query retorna resultados quando executada diretamente contra Loki? `curl -sf "http://localhost:30093/loki/api/v1/query_range?query={...}" | head -5`
5. Os labels do workflow pod matcham a query? `kubectl --context k3d-loopzero-build get pod <workflow-pod> -o jsonpath='{.metadata.labels}'`

**Resolução:**

6. Se Promtail não está no build cluster → instalar via `infra/helm/` e adicionar ao `task setup`
7. Se a query está errada → corrigir labels no `loki/client.go`
8. Se tudo está certo mas logs locais expiram rápido → documentar como limitação do ambiente local

**Critério de DONE:** `loopzero logs --build <id-de-build-recente> --json` retorna `entries.length >= 1` com texto de log real (não array vazio).

**Critério alternativo (se Loki local não é viável):** Documentar explicitamente em `CLAUDE.md` que `loopzero logs` requer Loki (disponível em DO, não garantido local) e o dogfood skill marca como WARN no pre-flight.

---

## F-05: Deploy "sync failed" com mensagem genérica (S3 — P3)

### Root Cause

O CLI exibe `"Deploy failed: sync failed. Check service logs for details."` sem incluir o motivo específico da falha.
A API já retorna `error_message` no build record — o CLI não o utiliza na mensagem ao usuário.

### Fix

**Arquivo:** `cli/src/commands/deploy.ts` — handler de `deploy.failed` event

Incluir o `error_message` quando disponível e sugerir o comando de logs correto:

```typescript
// Antes: "Deploy failed: sync failed. Check service logs for details."
// Depois: "Deploy failed: sync failed (ArgoCD application not healthy). Run: loopzero logs --build build-abc123"
```

Nota: `error_message` pode conter termos internos ("ArgoCD"). O CLI deve sanitizar antes de exibir (Rule 3).

### DoD

**TDD:**

1. **Teste RED:** Deploy SSE event `deploy.failed` com `error_message` → output contém parte da mensagem (sanitizada)
2. **Teste RED:** Deploy SSE event `deploy.failed` com `error_message` contendo "ArgoCD" → output NÃO contém "ArgoCD" (Rule 3)
3. **Teste RED:** Deploy SSE event `deploy.failed` → output contém `loopzero logs --build <id>` (orientação de próximo passo)
4. **Implementar** fix
5. **Testes GREEN**

**Validação E2E:**

6. Provocar deploy failure → mensagem contém build_id e sugestão de `loopzero logs`
7. Output de falha NÃO contém termos internos: `echo "$OUTPUT" | grep -icE "argocd|gitops|gitea" → 0`
8. `cd cli && npm test` → 0 failures

**Critério de FAIL:** Se a mensagem de deploy failed ainda diz apenas "Check service logs for details" sem build_id, o fix não foi aplicado.

---

## Sequência de Execução

```
F-01 (P0, 30min):  nil interface trap → secret set funciona
    ↓ Gate: items 5-10 do DoD passam
F-02 (P1, 15min):  --json exige --yes em secret delete
    ↓ Gate: items 5-10 do DoD passam
F-03 (P2, 45min):  logs infere último build
    ↓ Gate: items 6-9 do DoD passam
F-04 (P2, investigação): Promtail/Loki diagnóstico
    ↓ Gate: diagnóstico completo OU documentado como limitação
F-05 (P3, 30min):  mensagem sync failed enriquecida
    ↓ Gate: items 6-8 do DoD passam
DOGFOOD: /dogfood full → 0 achados S0/S1/S2
```

**Tempo total estimado: 2-3h**

**Critério de 100% funcional:** Dogfood full com skill v2 retorna 0 achados S0/S1/S2.

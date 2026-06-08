# Dogfood Walkthrough — Dashboard → Go proxy → TheMemory E2E

Manual step-by-step para testar o sistema end-to-end. Companion to `dev-loop.md § TheMemory native-process lifecycle`. Cobre 3 caminhos de uso:

1. **API direta** (curl) — funciona AGORA sem UI
2. **MCP via Claude Code** — TheMemory exposto como 4 ferramentas (`remember`/`recall`/`forget`/`reflect`)
3. **Dashboard UI** — requer Kratos login (instruções no final)

---

## Estado atual (verificado 2026-06-07 23:54 BRT)

| Camada | Endpoint | Status |
|---|---|---|
| TheMemory native | `http://localhost:8080/v1/health` | ✅ 200 (PID em `.themory.pid`) |
| Go proxy loopzero-cloud | `http://localhost:13002/v1/memory/*` | ✅ 200 com Bearer `loopzero_live_*` |
| K8s Secret master key | `loopzero-system/themory-dev-key` | ✅ valueFrom propagada para Pod |
| Dashboard Vite dev | `http://localhost:5174/memory/memories` | ⏸ Requer Kratos session login |
| Bearer scoped (memory:r/w/d) | `~/.loopzero/local-test-memory-bearer.txt` | ✅ chmod 600 |

---

## Caminho 1 — API via curl (funciona AGORA)

### Setup pré-requisitos

```bash
# 1. TheMemory native + Tilt cluster (já feito por start-themory.sh + tilt up)
bash infra/scripts/local/start-themory.sh
bash infra/scripts/local/bootstrap-themory-key.sh

# 2. Port-forward Go proxy para o host
kubectl -n loopzero-system port-forward deployment/loopzero-cloud 13002:13002 &

# 3. Carregar bearer (scoped key memory:read|write|delete)
export BEARER="$(cat ~/.loopzero/local-test-memory-bearer.txt)"
```

### 1.1 — Remember (TheMemory direct, sem proxy)

Para teste de sanidade. NÃO requer cloud Pod:

```bash
curl -X POST http://localhost:8080/v1/remember \
  -H "Content-Type: application/json" \
  -d '{
    "userId": "smoke-user",
    "text": "I prefer dark mode and use Linux on a Framework 16 laptop",
    "infer": false
  }' | jq '.'
```

**Resposta esperada:**

```json
{
  "items": [{
    "id": "<uuid>",
    "text": "I prefer dark mode and use Linux on a Framework 16 laptop",
    "hash": "<md5>",
    "scope": { "kind": "user", "userId": "smoke-user" },
    "createdAt": "2026-06-08T...",
    "tValid": "2026-06-08T...",
    "tInvalid": null,
    "confirmationCount": 1,
    "rejectedAsDuplicate": false
  }]
}
```

### 1.2 — List via Go proxy (cadeia E2E)

```bash
curl -fsS "http://localhost:13002/v1/memory/list" \
  -H "Authorization: Bearer $BEARER" | jq '.'
```

**Resposta esperada:**

```json
{
  "memories": [
    {
      "id": "<uuid>",
      "content": "I prefer dark mode and use Linux on a Framework 16 laptop",
      "created_at": "2026-06-08T..."
    }
  ]
}
```

> **D1 invariant**: o proxy ignora qualquer `?owner_user_id=` na URL — usa o `subject` da API key (no nosso caso `smoke-user`). Para listar as memórias de outro usuário, você usaria uma Bearer key cujo `subject` aponta para esse usuário.

### 1.3 — Recall semântico (TheMemory direct)

```bash
curl -X POST http://localhost:8080/v1/recall \
  -H "Content-Type: application/json" \
  -d '{
    "userId": "smoke-user",
    "query": "what device does the user use?",
    "topK": 5
  }' | jq '.'
```

**Resposta:** items rankeados por similaridade semântica + BM25 + entity-boost (Mem0-shape 9-step pipeline).

### 1.4 — Forget via Go proxy

```bash
MEMORY_ID="<uuid-from-step-1.1>"
curl -fsS -X DELETE "http://localhost:13002/v1/memory/forget/$MEMORY_ID" \
  -H "Authorization: Bearer $BEARER" -i | head -5
```

**Resposta esperada:** `HTTP/1.1 204 No Content` (soft-delete via `t_invalid`).

### 1.5 — One-shot smoke completo

```bash
bash infra/scripts/local/smoke-themory-e2e.sh
# OU via Taskfile:
task test:e2e:themory
```

**Output:**
```
STEP 1/5: POST /v1/remember to TheMemory direct → memoryId=<uuid>
STEP 2/5: GET /v1/memory/list via Go proxy → fact appeared
STEP 4/5: DELETE /v1/memory/forget/{id} via Go proxy → 204
STEP 5/5: GET /v1/memory/list via Go proxy → fact correctly absent
✓ ALL 5 STEPS GREEN — chain wired end-to-end
```

---

## Caminho 2 — MCP (Claude Code consome TheMemory como tool)

TheMemory expõe os 4 verbos (`remember` / `recall` / `forget` / `reflect`) como tools MCP via `themory mcp`. Setup para Claude Code:

### 2.1 — Configurar Claude Code MCP

Edite `~/.claude/mcp.json` OU project-local `.mcp.json`:

```json
{
  "mcpServers": {
    "themory": {
      "command": "node",
      "args": [
        "/home/paulo/Projetos/loopzero/loopzero-memory/dist/agent-tools/cli.js",
        "mcp",
        "--transport=stdio"
      ],
      "env": {
        "LOOPZEROMEM_PG_URI": "postgresql://themem:themem@localhost:5432/themory_dev",
        "OPENAI_API_KEY": "<your-key>",
        "OPENAI_BASE_URL": "https://openrouter.ai/api/v1"
      }
    }
  }
}
```

Path absoluto matters — MCP server precisa ler o dist do TheMemory.

### 2.2 — Verificar tools no Claude Code

Em uma nova sessão Claude Code:

```
/mcp
```

Deve listar 4 tools: `remember`, `recall`, `forget`, `reflect`.

### 2.3 — Invocar a partir do agente

Conversação natural — o Claude detecta o uso. Exemplo:

```
You: lembre que eu uso fonte JetBrains Mono em 14pt
[Claude invoca tool: themory.remember({ userId: "...", text: "..." })]
Claude: Salvo.

You: o que voce sabe sobre minha preferencia de fontes?
[Claude invoca tool: themory.recall({ userId: "...", query: "..." })]
Claude: Voce usa JetBrains Mono em 14pt.
```

### 2.4 — Inspecionar tool-output bruto

Use o flag debug do Claude Code:

```bash
claude --debug 2>&1 | grep -A 5 "tool_use.*themory"
```

---

## Caminho 3 — Dashboard UI (requer Kratos login)

A Dashboard vive em Vite dev server (`http://localhost:5174`). Pages relevantes:

- `/memory/memories` — list + add memory
- `/memory/entities` — explore entities (User/Session/Agent/Skills)
- `/memory/webhooks` — webhook subscriptions
- `/memory/exports` — bulk export jobs

### 3.1 — Caminho rápido (assume cluster up + dashboard up)

```bash
# 1. Dashboard running (já feito):
cd cloud/dashboard && npm run dev  # binds :5173 (or :5174 if 5173 taken)

# 2. Port-forwards no host (já feito):
kubectl -n loopzero-system port-forward deployment/loopzero-cloud 13002:13002 &
kubectl -n loopzero-auth  port-forward svc/ory-kratos-public 14433:80 &
kubectl -n loopzero-auth  port-forward svc/ory-hydra-public  14444:4444 &

# 3. Setup Caddy + DNS (dev-public flow):
task cloud:dashboard-public:up
```

Caddy mapeia `app.loopzero.example` → `localhost:5174` (Vite proxies já roteam `/v1` → `:13002`, `/self-service` → `:14433` per `vite.config.ts`).

### 3.2 — Login

1. Browser → `https://app.loopzero.example`
2. Caddy redireciona para Kratos selfservice (GitHub OAuth ou Google)
3. Após login, retorna para Dashboard

### 3.3 — Páginas Memory

Sidebar → **Memory** → submenu:

| Page | URL | O que mostra |
|---|---|---|
| Memories | `/memory/memories` | Lista paginada + Search + Add modal |
| Entities | `/memory/entities` | 4 abas (User/Session/Agent/Skills) com entity-boost rankings |
| Webhooks | `/memory/webhooks` | Subscriptions + delivery history + replay |
| Exports | `/memory/exports` | Bulk export jobs (NDJSON / CSV) |

Cada page faz call para Go proxy `/v1/memory/*` que propaga para TheMemory.

---

## Troubleshooting

### "Empty reply" ao curl `:13002`

Port-forward caiu. Re-run:

```bash
pkill -f "port-forward.*loopzero-cloud"
kubectl -n loopzero-system port-forward deployment/loopzero-cloud 13002:13002 &
```

### Dashboard branco / Kratos HTTP 500

Sessão Kratos não estabelecida. Limpar cookies de `loopzero.example` + relogar OR usar **Caminho 1 (API curl)** que não depende de UI.

### `bootstrap-themory-key.sh` HTTP 401

Server foi reinicializado e o bootstrap-token foi regenerado. Re-bootstrap:

```bash
bash infra/scripts/local/stop-themory.sh
bash infra/scripts/local/start-themory.sh  # gera token novo
bash infra/scripts/local/bootstrap-themory-key.sh --rotate  # nova master key
```

### Smoke test step 2 timeout

`TEST_USER` (cliente) não match `subject` da API key (cloud). Override:

```bash
TEST_USER=$(meu-subject) bash infra/scripts/local/smoke-themory-e2e.sh
```

Default = `smoke-user` (matches o `subject` da key minted via `create-test-api-key.sh` quando o admin JWT é do session-user `smoke-user`).

### TheMemory dies sem `OPENAI_API_KEY`

Server precisa do provider LLM para a extraction pipeline. Sourcing `.themory.env`:

```bash
cp infra/scripts/local/.themory.env.example infra/scripts/local/.themory.env
# Edit OPENAI_API_KEY + OPENAI_BASE_URL
bash infra/scripts/local/stop-themory.sh
bash infra/scripts/local/start-themory.sh
```

---

## Cleanup ao fim do dogfood

```bash
# Stop dashboard (if started by you)
pkill -f "vite"

# Stop port-forwards
pkill -f "kubectl.*port-forward"

# Stop TheMemory native process
bash infra/scripts/local/stop-themory.sh

# Optional: cleanup test artifacts
rm -f infra/scripts/local/.themory.log
# (PID file already removed by stop-themory.sh; bootstrap-token persists until next start)
```

---

## Estado final do sistema (no encerramento desta sessão de dogfood)

Para que o próximo `tilt up` seja idempotente:

- `infra/tilt/cloud-deployment.yaml` consome Secret via `valueFrom.secretKeyRef`. NÃO editar inline.
- Master key vive em K8s Secret `loopzero-system/themory-dev-key`. Rotate via `bootstrap-themory-key.sh --rotate`.
- Documentação operador completa: `docs/operations/dev-loop.md § TheMemory native-process lifecycle`.

---

## Referências cruzadas

- Operator runbook: [`docs/operations/dev-loop.md`](./dev-loop.md)
- Plan E2E: [`.claude/knowledge-base/plans/loopzero-cloud-themory-e2e-finalize-plan.md`](../../.claude/knowledge-base/plans/loopzero-cloud-themory-e2e-finalize-plan.md) (v1.3, SHIPPABLE_WITH_CAVEATS 70.0)
- Roadmap closed: [`.claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md § Open items #6`](../../.claude/knowledge-base/plans/2026-06-06-dashboard-memory-section-roadmap.md)
- API contract upstream: TheMemory `/v1/openapi.json` (`curl localhost:8080/v1/openapi.json | jq '.paths | keys'`)
- MCP tools spec: [loopzero-memory README § Agent tools (MCP)](../../../loopzero-memory/README.md)

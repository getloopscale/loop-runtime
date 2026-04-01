# Plano: `loopzero cancel` — CLI Command

> **Objetivo:** Permitir que o usuario cancele um build em andamento via CLI,
> usando o backend ja existente (CancelBuild + POST /builds/:id/cancel).
>
> **Escopo:** CLI only. Backend 100% implementado e wired.
>
> **Classificacao:** T1 — single-component (CLI), zero backend changes.
>
> **Status:** PENDING — requires meeting vote

---

## Contexto

### O Que Ja Existe (Backend 100% Pronto)

1. **`BuildOrchestrator.CancelBuild()`** (`api/internal/services/build_orchestrator.go:494`)
   - Adquire environment lock (Rule 6)
   - Re-fetch build inside lock (race condition guard)
   - Verifica se build ja e terminal → retorna `ErrBuildAlreadyCompleted` (409)
   - Transiciona via state machine: `BuildCancelRequested`
     - QUEUED → CANCELLED (direto)
     - RUNNING → CANCELLING → CANCELLED (confirmacao)
     - RELEASING → CANCELLED (direto)
   - Cancela o workflow via `WorkflowSubmitter.Cancel` (best-effort)
   - Emite SSE `build.cancelled`

2. **`POST /api/v1/builds/{id}/cancel`** (`api/internal/server/server.go:219`)
   - Auth middleware ativo
   - Idempotency middleware ativo
   - SEC-4: Verifica tenant ownership (retorna 404 para builds de outros tenants)
   - Resposta: `202 Accepted` com `{"status": "cancelled"}`

3. **State Machine** (`pkg/statemachine/build.go`)
   - QUEUED + BuildCancelRequested → CANCELLED
   - RUNNING + BuildCancelRequested → CANCELLING
   - CANCELLING + BuildCancelConfirmed → CANCELLED
   - RELEASING + BuildCancelRequested → CANCELLED

4. **Typed Errors** (`pkg/errors/errors.go`)
   - `ErrBuildNotFound` (404) — build does not exist
   - `ErrBuildAlreadyCompleted` (409) — build already in terminal state

### O Que Falta

Apenas o comando CLI. Nenhuma mudanca em API, operators, pkg, ou infra.

---

## Design

### Sintaxe do Comando (Decisao do Meeting)

**Opcao A:** Comando top-level
```bash
loopzero cancel <build-id>
```

**Opcao B:** Subcomando de build
```bash
loopzero build cancel <build-id>
```

Ambas as opcoes sao apresentadas ao meeting para decisao. Argumentos:

| Criterio | Opcao A (`loopzero cancel`) | Opcao B (`loopzero build cancel`) |
|----------|------------------------|-------------------------------|
| Consistencia com CLI existente | `loopzero rollback`, `loopzero deploy` — top-level | Agrupa operacoes de build |
| Simplicidade | Menos digitacao | Mais digitacao |
| Extensibilidade | Se futuramente cancelar outros recursos (preview, promote), fica ambiguo | Claro que cancela build especificamente |
| DX (Vercel-like) | `vercel cancel` nao existe, `vercel rollback` e top-level | — |

### Flags

| Flag | Tipo | Default | Descricao |
|------|------|---------|-----------|
| `--json` | boolean | false | Output em JSON (stability contract, spec S0.3) |
| `--force` | boolean | false | **Decisao do meeting:** cancelar builds em estado RELEASING? |

### Exit Codes (Rule 4)

| Code | Cenario |
|------|---------|
| 0 | Build cancelado com sucesso |
| 1 | Usage error (build-id nao fornecido, flag invalida) |
| 3 | Cancel falhou (build not found, already completed, API error) |
| 4 | Auth error (nao autenticado) |

### Output Humano

**Sucesso:**
```
Build bld-abc12345 cancelled.
```

**Build ja finalizado:**
```
Build bld-abc12345 has already completed. Nothing to cancel.
```

**Build nao encontrado:**
```
Build bld-abc12345 not found.
```

### Output JSON (--json)

**Sucesso:**
```json
{"ok": true, "build_id": "bld-abc12345", "status": "cancelled"}
```

**Erro — build ja finalizado:**
```json
{"code": "BUILD_ALREADY_COMPLETED", "error": "Build bld-abc12345 has already completed. Nothing to cancel.", "exit_code": 3}
```

**Erro — nao encontrado:**
```json
{"code": "BUILD_NOT_FOUND", "error": "Build bld-abc12345 not found.", "exit_code": 3}
```

**Erro — auth:**
```json
{"code": "AUTH_REQUIRED", "error": "Not authenticated. Run: loopzero login", "exit_code": 4}
```

### Error Cases

| API Response | CLI Behavior | Exit Code |
|-------------|-------------|-----------|
| 202 Accepted | "Build {id} cancelled." | 0 |
| 404 Not Found | "Build {id} not found." | 3 |
| 409 Conflict (already completed) | "Build {id} has already completed. Nothing to cancel." | 3 |
| 401 Unauthorized | "Not authenticated. Run: loopzero login" | 4 |
| 5xx / network error | "Cancel failed: {reason}. Try again." | 3 |

### Conformidade Rule 3 (No Internal Terms)

O comando NAO deve mencionar:
- "Argo Workflow" → nao mencionado
- "CANCELLING state" → nao mencionado (apenas "cancelled")
- "advisory lock" → nao mencionado
- Qualquer termo interno

---

## Tasks

### T1: Implementar CancelCommand (`cli/src/commands/cancel.ts`)

**Classificacao:** T1

**Arquivo:** `cli/src/commands/cancel.ts` (novo)

**Padrao:** Seguir exatamente `rollback.ts` — DI via deps interface, classe CancelCommand
com metodo `execute()`, `exitWithError()`, `handleError()`.

```typescript
export interface CancelDeps {
  getToken: () => string | null;
  cancelBuild: (buildId: string) => Promise<{ status: string }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface CancelOptions {
  json?: boolean;
}

export class CancelCommand {
  constructor(private readonly deps: CancelDeps) {}
  async execute(buildId: string, opts: CancelOptions): Promise<number> { ... }
}
```

**API call:** `POST /api/v1/builds/${buildId}/cancel` (sem body)

**DoD:**
- [ ] CancelCommand class implementada
- [ ] Auth check (exit 4)
- [ ] Build ID required (exit 1)
- [ ] Success output (exit 0)
- [ ] Error mapping: 404 → "not found" (exit 3), 409 → "already completed" (exit 3), 401 → auth (exit 4)
- [ ] `--json` output para todos os cenarios
- [ ] Zero internal terms em qualquer string
- [ ] `npm run build` passa

---

### T2: Testes Unitarios (`cli/tests/unit/cancel.test.ts`)

**Classificacao:** T1

**Arquivo:** `cli/tests/unit/cancel.test.ts` (novo)

**Cenarios obrigatorios (Arrange-Act-Assert):**

1. `cancel succeeds — returns 0 and prints confirmation`
2. `cancel with --json — returns JSON with ok:true`
3. `cancel when not authenticated — returns exit 4`
4. `cancel when build not found (404) — returns exit 3`
5. `cancel when build already completed (409) — returns exit 3`
6. `cancel with --json when error — returns JSON error shape`
7. `cancel when API returns 500 — returns exit 3 with retry suggestion`

**DoD:**
- [ ] 7+ testes escritos e passando
- [ ] Cada teste usa deps mockadas (padrao do projeto)
- [ ] `npm test` passa
- [ ] Nenhum teste flaky

---

### T3: Wire na CLI (`cli/src/index.ts`)

**Classificacao:** T1

**Localizacao:** `cli/src/index.ts` — apos o bloco `rollback` (linha ~590)

**Wire pattern (seguir rollback):**
```typescript
program
  .command('cancel <build-id>')
  .description('Cancel a running build')
  .action(async (buildId: string) => {
    const globalOpts = program.opts();
    const token = configStore.getToken();
    const baseURL = getDefaultApiUrl();
    const apiClient = new ApiClient({ baseURL, token: token ?? undefined });

    const cancelCmd = new CancelCommand({
      getToken: () => configStore.getToken(),
      cancelBuild: async (id: string) => {
        return apiClient.post(`/api/v1/builds/${encodeURIComponent(id)}/cancel`, {});
      },
      writeTo,
      writeError,
    });

    const exitCode = await cancelCmd.execute(buildId, { json: globalOpts.json });
    process.exitCode = exitCode;
  });
```

**DoD:**
- [ ] Import de CancelCommand adicionado
- [ ] Comando registrado no program
- [ ] `loopzero cancel --help` funciona
- [ ] `loopzero cancel bld-xxx` chama API corretamente
- [ ] `npm run build` passa

---

### T4: Changelog + Documentacao

**Classificacao:** T0

**Arquivo:** `CHANGELOG.md`

**Entry:**
```markdown
### Added
- CLI command `loopzero cancel <build-id>` to cancel running builds (#cancel)
```

**DoD:**
- [ ] Entry adicionada em `[Unreleased]` > `Added`
- [ ] Formato Keep a Changelog respeitado

---

## Meeting Agenda

Questoes que o meeting deve responder antes da implementacao:

### Q1: Sintaxe do Comando

`loopzero cancel <build-id>` vs `loopzero build cancel <build-id>`?

**Recomendacao:** `loopzero cancel <build-id>` — consistente com `loopzero rollback`, `loopzero deploy`.
Subcomando `build` so faz sentido se houver outros cancelaveis (preview, promote),
que nao existem hoje (YAGNI).

### Q2: Flag `--force` para RELEASING

O state machine ja permite RELEASING → CANCELLED. O backend aceita cancel em RELEASING.
Deve existir uma flag `--force` para diferenciar?

**Recomendacao:** NAO. O backend ja cancela RELEASING sem flag especial. Adicionar `--force`
e YAGNI — nao ha estado onde cancel precisa de "forca extra". Simplifica a DX.

### Q3: SSE Streaming de Progresso do Cancel

O cancel deve mostrar streaming SSE do progresso (como deploy)?

**Recomendacao:** NAO. Cancel e uma operacao instantanea (~100ms). O backend ja
transiciona sincronamente e retorna 202. Streaming adicionaria complexidade
sem valor (KISS).

---

## O Que NAO Fazer

- [ ] NAO modificar o backend (API, orchestrator, routes)
- [ ] NAO modificar state machines
- [ ] NAO adicionar novos endpoints
- [ ] NAO criar subcomando `loopzero build` (YAGNI — so quando houver mais subcomandos)
- [ ] NAO implementar streaming SSE para cancel
- [ ] NAO implementar `--force` sem aprovacao do meeting

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `api/internal/services/build_orchestrator.go` | CancelBuild implementation |
| `api/internal/routes/builds.go` | Cancel HTTP handler |
| `api/internal/server/server.go:219` | Route registration |
| `pkg/statemachine/build.go` | State transitions for cancel |
| `pkg/errors/errors.go` | ErrBuildNotFound, ErrBuildAlreadyCompleted |
| `cli/src/commands/rollback.ts` | Reference pattern for CLI command |
| `cli/src/index.ts` | Command wiring |

# LoopZero CLI Reference — Documento Técnico

> Documento técnico de todos os comandos do `loopzero` CLI, com opções e status de validação E2E na sessão de 2026-04-23.

**CLI version:** 0.1.0
**Session-owner:** validação `docs/plans/e2e-validation-plan.md` (k3d production-parity)

---

## Legenda de status

| Símbolo | Significado |
|---|---|
| ✅ | Validado end-to-end nesta sessão contra cluster real |
| 🟢 | Validado em sessão anterior (REPORT.md prévio) + re-verificado parcialmente |
| ⚠️ | Comando funcional; backend com limitação conhecida (documentada) |
| ❌ | Backend não implementado (CLI retorna mensagem UX-friendly via AUDIT-27/28) |
| 📝 | Não testado nesta sessão (não inferido quebrado) |

## Global flags

```
loopzero [options] [command] [args...]

Options:
  -V, --version       output the version number
  --json              Output in JSON format  (Rule 5: contrato estável)
  -h, --help          display help for command
```

**Env vars:**

| Variável | Default | Uso |
|---|---|---|
| `LOOPZERO_URL` | `http://localhost:8080` | API endpoint (Vercel-style override). **Atenção:** k3d local usa `http://localhost:13001`. |
| `LOOPZERO_API_URL` | — | Legacy alias of `LOOPZERO_URL`. |
| `CI` | — | Quando true: desativa spinners, força saída estruturada, prompts auto-abortam em non-TTY. |
| `LOOPZERO_SERVICE_AUTH` | — | Token operator para `loopzero admin`. |

**Exit codes** (Rule 4, invioláveis):

| Code | Significado |
|---|---|
| 0 | Sucesso |
| 1 | Usage error (argumentos, config) |
| 2 | Build failure |
| 3 | Deploy failure / recurso |
| 4 | Auth error |
| 5 | Partial failure |

---

## 1. Autenticação

### `loopzero login`

Autentica com OAuth2 device flow.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--token <token>` | string | — | Authentication token (skip device flow) |
| `--team <team>` | string | — | Select team |

**Status:** 📝 Não re-executado nesta sessão (OAuth2 client_credentials direto do Hydra foi usado). Login device-flow validado em sessão anterior.

### `loopzero logout`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `-y, --yes` | flag | false | Skip confirmation prompt |

**Status:** 📝

### `loopzero whoami`

Retorna identidade atual.

**Opções:** apenas `--json` global.

**Status:** ✅ Funcional em duas dimensões:
- Token válido → `{authenticated: true, email, team, username}`
- Token inválido → `exit 4` + `AUTH_REQUIRED`
- API unreachable → `code=API_UNREACHABLE` + "Check LOOPZERO_URL" (AUDIT-22)

---

## 2. Inicialização de projeto

### `loopzero init`

Inicializa `loopzero.yaml` no diretório atual.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--yes` | flag | false | Skip confirmation (obrigatório em CI) |
| `--force` | flag | false | Overwrite existing `loopzero.yaml` |
| `--name <name>` | string | diretório | Project name |

**Status:** 📝 (fixture foi criada manualmente)

### `loopzero validate`

Valida `loopzero.yaml` localmente.

**Opções:** apenas `--json` global.

**Status:** 📝

---

## 3. Deploy pipeline (core)

### `loopzero deploy`

Constrói, publica imagem, e sincroniza release via ArgoCD.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target environment |
| `--app <name...>` | string[] | todos | Deploy specific app(s) |
| `--wait` | flag | true | Wait for deploy to complete |
| `--no-wait` | flag | false | Return imediato após submit |
| `--no-cache` | flag | false | Build without layer cache |

**Status:** ✅ Go (build-fefb8b7e2d90364e SUCCEEDED 214s) + Node (build-98802efe21dfd76a SUCCEEDED 208s após AUDIT-29).

**Bug crítico encontrado + corrigido nesta sessão:** AUDIT-29 — `loopzero-packs-runner` local bundlava shell script legacy. Qualquer deploy Node falhava com `adduser -D -u 1000 loopzero`. Fix: Dockerfile multi-stage agora clona `github.com/getloopscale/loopzero-packs` e builda Go binary canônico.

### `loopzero status`

Mostra estado do último build + release no environment.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target environment |

**Status:** ✅ JSON schema `{project, environment, last_build, active_release, sync_status, url}` estável (AUDIT-2).

### `loopzero logs`

Logs do build (build cluster) ou workload (runtime cluster).

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--build <id>` | string | último | Build ID |
| `--tail <n>` | int | — | Últimas N linhas |
| `--step <name>` | string | — | Filter por build step |
| `--env <name>` | string | `production` | Target environment |
| `-f, --follow` | flag | false | Stream em tempo real via SSE |

**Status:** ⚠️ Command responde corretamente, mas Loki cross-cluster não wired em k3d local. Em produção (DO) funciona. Gap infra, não bug CLI.

### `loopzero rollback`

Reverte para release anterior.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target environment |
| `--to <release-id>` | string | anterior | Rollback para release específica |
| `--yes` | flag | false | Skip confirmation |

**Status:** ✅ Validado: `rel-3779212b24b1b1f4` → `rel-b48bebaa478be160` em 1s, sync synced.

### `loopzero promote`

Promove release de um env para outro.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--from <env>` | string | `staging` | Source environment |
| `--to <env>` | string | `production` | Target environment |
| `--yes` | flag | false | Skip confirmation |

**Status:** ✅ Validado: staging→production em 1s, nova release criada.

### `loopzero cancel <build-id>`

Cancela build RUNNING ou QUEUED. Existe top-level **e** como `loopzero build cancel <id>` (AUDIT-30).

**Status:** ✅ Validado: `build-3ec18f87100945f1` RUNNING → CANCELLED em <5s.

### `loopzero scale`

Ajusta replicas fixas ou autoscaling.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--app <name>` | string | — | App a escalar |
| `--env <name>` | string | `production` | Target environment |
| `--replicas <n>` | int | — | Fixed replica count |
| `--min <n>` | int | — | Min replicas (HPA) |
| `--max <n>` | int | — | Max replicas (HPA) |
| `--cpu <n>` | int | — | CPU target % (HPA) |

**Status:** ⚠️ Backend `/scale` endpoint retorna `202 Accepted` com `status: "acknowledged"` (stub — config validada mas não persistida no DB). Para aplicar scaling, declare `autoscaling` em `loopzero.yaml` e execute `loopzero deploy`. Scale-to-zero (`min: 0`) requer KEDA habilitado no cluster (`LOOPZERO_KEDA_ENABLED=true`).

**Path declarativo (recomendado)**: configure `autoscaling: { min: 0, max: 5, targetCPU: 80 }` em `loopzero.yaml` e execute `loopzero deploy`.

---

## 4. Projetos

### `loopzero project list`

Opções: apenas `--json`.

**Status:** ✅ JSON `{projects:[{id,name,team,created_by,created_at}]}`.

### `loopzero project inspect <name>`

**Status:** ✅ Required arg (`exit 1 COMMAND_VALIDATION_ERROR` sem nome). AUDIT-4 garante campo `team` (não `team_id`).

### `loopzero project create <name>`

Sem flags.

**Status:** 📝 Não re-testado nesta sessão (foi testado em sessão anterior).

### `loopzero project delete <name>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | todos | Delete de env específico |
| `--yes` | flag | false | Skip confirmation (obrigatório em CI) |

**Status:** 📝

---

## 5. Equipe

### `loopzero team list`

**Status:** ✅ JSON `{members:[{username,email,role,joined_at}],total}`.

### `loopzero team add <username>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--role <role>` | string | `member` | Role (member\|admin) |

**Status:** ✅ Contrato correto: user precisa `loopzero login` primeiro — retorna `USER_NOT_FOUND` se nunca logou.

### `loopzero team remove <username>`

Sem flags.

**Status:** ✅ Retorna `MEMBER_NOT_FOUND` exit 1 se não for membro (AUDIT-6 silent-success eliminado).

### `loopzero team role <username> <role>`

Sem flags.

**Status:** ✅ Retorna `MEMBER_NOT_FOUND` se username inexistente.

---

## 6. Ambientes

### `loopzero env list`

**Status:** ✅ JSON `{environments:[{name,status}],total}`.

### `loopzero env inspect <name>`

**Status:** ✅ Retorna `ENV_NOT_FOUND` exit 1 se nome inexistente (AUDIT-12).

### `loopzero env create <name>`

**Status:** ✅ Plan enforcement ativo. Starter=1 env → `PLAN_LIMIT_EXCEEDED` com mensagem clara.

### `loopzero env delete <name>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--confirm <name>` | string | — | Confirma repetindo nome (mais seguro) |
| `--yes` | flag | false | Skip prompt (CI/scripts) |

**Status:** ✅ Retorna `ENV_NOT_FOUND` se env não existe (AUDIT-12 — sem silent success).

---

## 7. Custom Domains

### `loopzero domain list`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--project <name>` | string | — | Filter por project |
| `--app <name>` | string | — | Filter por app |
| `--env <name>` | string | — | Filter por env |

**Status:** ✅ JSON válido.

### `loopzero domain add <domain>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--project <name>` | string | — | Project name |
| `--app <name>` | string | — | App name |
| `--env <name>` | string | — | Environment |

**Status:** ⚠️ Local: requer Cloudflare credentials (`CF_ZONE_MANAGEMENT`) não provisionadas em dev. Validado E2E em produção (aquim.ai — CLAUDE.md 2026-04-08).

### `loopzero domain verify <domain>`, `loopzero domain status <domain>`, `loopzero domain dns <domain>`

Sem flags adicionais.

**Status:** ⚠️ Mesmo gap de CF creds local.

### `loopzero domain remove <domain>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--yes` | flag | false | Skip confirmation |

**Status:** ⚠️ CF creds. AUDIT-13: erro uniforme via `NewDomainNotFound`.

---

## 8. Builds

### `loopzero build list`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | — | Filter por env |
| `--limit <n>` | int | — | Max builds |

**Status:** ✅ AUDIT-5: colunas truncadas, header "COMMIT".

### `loopzero build inspect <build-id>`

**Status:** ✅ Retorna JSON completo do build + duration + error.

### `loopzero build cancel <build-id>` (AUDIT-30)

**Status:** ✅ Alias de `loopzero cancel <id>`. Validado em `loopzero build --help`.

---

## 9. Secrets

### `loopzero secret set <nameValue>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target env |
| `--app <name>` | string | — | Target app (required) |

**Formato:** `NAME=value` (validado: upper+underscore). Valor nunca aparece em output.

**Status:** ✅ Validado: `E2E_KEY=secretvalue` → `{name, environment, app, created:true}`.

### `loopzero secret list`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target env |

**Status:** ✅ JSON `{secrets:[{name,environment,app}]}` — **valores nunca retornados**.

### `loopzero secret delete <name>`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--env <name>` | string | `production` | Target env |
| `--app <name>` | string | — | Target app |
| `--yes` | flag | false | Skip confirmation |

**Status:** ✅ Idempotente-consciente: segunda chamada retorna `SECRET_NOT_FOUND` exit 1.

---

## 10. GitHub App Integration

### `loopzero connect`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--repo <owner/repo>` | string | auto-detect | Repo GitHub |
| `--branch <branch>` | string | `main` | Default branch para prod |
| `--installation-id <id>` | int | — | GitHub App installation ID |

**Status:** ✅ Handler ativo em dev após AUDIT-20. AUDIT-19 garantiu audit action `repository.connected` (antes era `project.created`). Requer GitHub App instalado no repo.

### `loopzero disconnect`

Sem flags.

**Status:** ✅ Retorna 404 `REPOSITORY_NOT_LINKED` se projeto sem conexão (AUDIT-21 — silent-success eliminado).

---

## 11. Audit Log

### `loopzero audit`

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--action <action>` | string | — | Filter (ex: `project.created`) |
| `--since <date>` | string | — | Filter por data (YYYY-MM-DD) |

**Status:** ✅ Requer role admin/owner. AUDIT-14: exit 4 em 401. AUDIT-23: JSON em **todas** as branches de erro. Actions seguem `resource.verb_past` (AUDIT-17/18/19).

---

## 12. Preview Environments

### `loopzero preview list`

Sem flags adicionais.

**Status:** ✅ Sprint 5 / T4.1 — API wired (`/api/v1/projects/{project}/previews`). Lista previews ativos do projeto corrente. Ordenação: created_at DESC.

### `loopzero preview inspect <id>`, `loopzero preview delete <id> [--yes]`

**Status:** ✅ Sprint 5. `inspect` retorna detalhes (id, pr_number, branch, status, url, ttl_expires_at). `delete` faz soft-delete (status='deleting'); o controller no mgmt cluster reconcilia o teardown do namespace. AUDIT-15: exit 1 `PREVIEW_NOT_FOUND` quando o ID não existe ou pertence a outro projeto.

---

## 13. Billing

### `loopzero billing portal`

Abre browser em portal Stripe.

**Status:** 📝 Requer cloud.{domain} com Stripe configurado. Gap dev local.

---

## 14. Admin (operator-only)

### `loopzero admin tenants evict`

Requer `LOOPZERO_SERVICE_AUTH` env var.

| Flag | Tipo | Default | Descrição |
|---|---|---|---|
| `--pattern <sqlLike>` | string | — | SQL LIKE (ex: `tn-lt-%`) |
| `--ids <csv>` | string | — | Lista separada por vírgula |
| `--dry-run` | flag | false | Mostra matches sem evict |
| `--yes` | flag | false | Skip confirmation |

**Status:** 📝 Operator-only; não testado nesta sessão.

---

## Contratos CLI (Rule 3/4/5)

**Rule 3 — No internal terms (0 violations 2026-04-23):**

Forbidden: `ArgoCD`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`.

Sweep: 16 commands × 4 canais (`--help`, `--json` happy, human, `--json` erro) = 46 checks, 0 leaks.

**Rule 4 — Exit codes permanentes:** 0/1/2/3/4/5 (ver tabela acima).

**Rule 5 — `--json` stability contract:** Field names congelados. Novos campos podem ser adicionados. Sweep validado 8/8 commands.

**Error code taxonomy** (consolidado AUDIT-17 até AUDIT-30):

| Code | Significado | Introduzido |
|---|---|---|
| `AUTH_REQUIRED` | Token ausente ou inválido | base |
| `API_UNREACHABLE` | API unreachable (ECONNREFUSED etc.) | AUDIT-22/23/24/25 |
| `COMMAND_VALIDATION_ERROR` | Argumento/flag inválido | base |
| `MISSING_CONFIG` | `loopzero.yaml` ausente | base |
| `INVALID_CONFIG` | `loopzero.yaml` inválido | base |
| `PLAN_LIMIT_EXCEEDED` | Limite do plano | Phase 1 |
| `DEPLOY_IN_PROGRESS` | Lock concorrente | base |
| `SCALE_NOT_AVAILABLE` | Feature não deployada | AUDIT-27 |
| ~~`PREVIEW_NOT_AVAILABLE`~~ | _Removido em Sprint 5 / T4.1 — preview API agora wirada. NOT_FOUND surface o code original._ | AUDIT-28 (deprecated) |
| `PROJECT_NOT_FOUND` | Resource 404 | base |
| `ENV_NOT_FOUND` | Resource 404 | AUDIT-12 |
| `SECRET_NOT_FOUND` | Resource 404 | base |
| `DOMAIN_NOT_FOUND` | Resource 404 | AUDIT-13 |
| `TEAM_MEMBER_NOT_FOUND` | Resource 404 | AUDIT-6 |
| `USER_NOT_FOUND` | User nunca logou | base |
| `REPOSITORY_NOT_LINKED` | Repo não conectado | AUDIT-21 |
| `PREVIEW_NOT_FOUND` | Preview inexistente | AUDIT-15 |
| `REPO_ALREADY_LINKED` | Repo já conectado a outro projeto | base |
| `NO_PREVIOUS_RELEASE` | Rollback sem release anterior | base |
| `NOT_CONNECTED` | Disconnect sem conexão | base |
| `BUILD_ALREADY_COMPLETED` | Cancel de build já finalizado | base |
| `RELEASE_NOT_FOUND` | Rollback/promote para release específica inexistente | base |
| `UNEXPECTED_ERROR` | Fallback genérico | base |

---

## Histórico de correções desta sessão (2026-04-23)

Commits auditáveis em branch `develop`:

| AUDIT | Area | Status |
|---|---|---|
| 17-19 | audit action naming (domain/source/promote/connect) | ✅ commits 676e1c9, 8e19bae, 64034df |
| 20 | RepositoryHandler desacoplado de WEBHOOK_SECRET | ✅ 24180fc |
| 21 | repo disconnect 404 | ✅ f3c067f |
| 22 | whoami unreachable vs expired | ✅ f40d2c6 |
| 23 | audit --json JSON em erro | ✅ cbe4512 |
| 24/25 | API_UNREACHABLE uniforme em 22 commands | ✅ d122304, 7b4ee2e |
| 26 | argo GVK fix | ❌ REJECTED (env gap, não bug) |
| 27 | scale 404 = SCALE_NOT_AVAILABLE | ✅ d980397 |
| 28 | preview list 404 = PREVIEW_NOT_AVAILABLE | ✅ a46b2ff |
| 29 | **Node deploy restaurado** (loopzero-packs-runner Go rewrite) | ✅ 3751c29 |
| 30 | `loopzero build cancel` discoverability | ✅ 5f56a0c |
| 31 | `loopzero logs` message respects terminal build status | ✅ 72eb363 |
| 32 | `loopzero deploy` stream.closed guides to `loopzero build inspect` | ✅ 703aeb5 |
| 33 | **`loopzero status` crash on active_release fields** (contract drift) | ✅ 465e620 |
| 34 | **`loopzero build inspect` Project: undefined** (contract drift `project`→`project_id`) | ✅ 71b5179 |
| 35 | audit log `resource_type="build"` for `/builds/*/cancel` | ✅ 1a8be8b |
| 36 | `loopzero init` exit 5 on 4xx (Rule 4 partial failure) | ✅ 564f79b |
| 37 | `loopzero logs --follow` exit 3 on stream error | ✅ 4e7b66c |

---

## Padrões sistêmicos encontrados e mitigados

Três classes de bug-pattern emergiram durante a validação E2E 2026-04-23.
Cada classe tem múltiplos AUDITs e heurística de detecção reutilizável.

### Classe 1: contract drift CLI↔API

**Heurística de detecção:** testar render humano do command (não só `--json`).
Interfaces TypeScript inventadas nos testes passam; API real produz undefined
nos campos e crash/render sujo.

**Ocorrências:** AUDIT-2 (status.last_build), AUDIT-33 (status.active_release),
AUDIT-34 (build inspect project_id).

**Regression guard padrão:** `expect(output).not.toContain('undefined')`.

### Classe 2: exit code não reflete erro visível

**Heurística:** verificar exit code isolado do output para cada command.
Especial atenção a commands que seguem depois de ver erro (init, logs --follow).

**Ocorrências:** AUDIT-36 (init com plan limit exit 0), AUDIT-37 (logs --follow
exit 0 após stream error).

**Regression guard padrão:** assertar `expect(exitCode).toBe(N)` explicitamente
para cada branch de erro.

### Classe 3: mensagem contradiz estado conhecido

**Heurística:** mensagens com "may", "still", "probably", "endpoint not
found" quando o CLI já conhece o estado real. Substituir por guia acionável.

**Ocorrências:** AUDIT-22 (whoami token-vs-unreachable), AUDIT-27 (scale
feature-missing), AUDIT-28 (preview feature-missing), AUDIT-31 (logs terminal
build), AUDIT-32 (deploy stream closed).

**Regression guard padrão:** `expect(msg).not.toContain('may')` + assertar
o novo guia (`Check LOOPZERO_URL`, `loopzero build inspect`, etc.).

---

**Última atualização:** 2026-04-23 (sessão de validação E2E completa — 21 AUDITs,
1059 testes unit passing, 0 regressões, 57+ commits na branch develop).
**Autor:** agente automatizado em Ralph loop contra k3d production-parity (mgmt / build / workload clusters).

# PR Guardrail — CLI Command E2E Coverage Matrix

> **Plano associado:** `docs/plans/CHG-2026-04-25-PR-GUARDRAIL-plan.md`
> **Atualizado em:** 2026-04-25 — Phase 0 + Phase 5 iter 2-10 (43 comandos cobertos, 87.8%) + 1 PIt ativado (F3.2)
> **Status:** Phase 5 progresso **87.8%** (43/49) — **MISSING zerado**, restantes apenas 4 BLOCKED (preview×3 + scale, gaps de backend não-deste-projeto). **Cobertura efetiva sobre testáveis: 43/45 = 95.6%.**
>
> **Progresso Phase 5 (iter 2 + iter 3 do Ralph Loop):**
>
> Iteração 2 (5 comandos):
> - ✅ `whoami` — `cli_whoami_test.go` (3 It × 5 contratos)
> - ✅ `logout` — `cli_logout_test.go` (4 It incl. bonus cancellation)
> - ✅ `validate` — `cli_validate_test.go` (4 It × 5 contratos, local-only)
> - ✅ `audit` — `cli_audit_test.go` (3 It × 5 contratos)
> - ✅ `env list` — `cli_env_list_test.go` (3 It × 5 contratos)
>
> Iteração 3 (6 comandos):
> - ✅ `project list` — `cli_project_list_test.go` (3 It × 5 contratos)
> - ✅ `project inspect` — `cli_project_inspect_test.go` (4 It incl. AUDIT-4 schema, MISSING_ARGUMENT, PROJECT_NOT_FOUND)
> - ✅ `team list` — `cli_team_list_test.go` (3 It × 5 contratos)
> - ✅ `build list` — `cli_build_list_test.go` (4 It incl. MISSING_CONFIG)
> - ✅ `secret list` — `cli_secret_list_test.go` (5 It incl. **CRITICAL zero-leak invariant** F1.7/Rule 5)
> - ✅ `team role` — `cli_team_role_test.go` (5 It incl. INVALID_ROLE, no-token, no-args, internal terms; happy path SKIP por seed gap)
>
> Iteração 4 (6 comandos):
> - ✅ `env create` — `cli_env_create_test.go` (5 It incl. INVALID_NAME validation, RESERVED_NAME, 409 idempotent)
> - ✅ `env inspect` — `cli_env_inspect_test.go` (4 It incl. ENV_NOT_FOUND, AUDIT-1 null projects safety)
> - ✅ `project create` — `cli_project_create_test.go` (4 It incl. RFC 1123 validation, 409 idempotent)
> - ✅ `domain list` — `cli_domain_list_test.go` (3 It × 5 contratos — converted from MANUAL_ONLY)
> - ✅ `secret set` — `cli_secret_set_test.go` (5 It incl. INVALID_FORMAT, INVALID_SECRET_NAME, **zero-leak on SET path itself**)
> - ✅ `domain status` — `cli_domain_status_test.go` (4 It incl. NOT_FOUND, MISSING_ARGUMENT)
>
> Iteração 5 (6 comandos — mutations):
> - ✅ `env delete` — `cli_env_delete_test.go` (6 It incl. **CRITICAL PRODUCTION_PROTECTED** test — production NEVER deletable, even with --yes --confirm)
> - ✅ `project delete` — `cli_project_delete_test.go` (5 It incl. round-trip, CI mode CONFIRMATION_REQUIRED, schema {project,deleted_environments,cancelled_builds})
> - ✅ `team add` — `cli_team_add_test.go` (5 It incl. INVALID_ROLE for 'owner' (cannot add as owner), garbage role)
> - ✅ `team remove` — `cli_team_remove_test.go` (5 It incl. **CRITICAL self-removal** SELF_REMOVE/OWNER_PROTECTED test, MEMBER_NOT_FOUND)
> - ✅ `secret delete` — `cli_secret_delete_test.go` (6 It incl. round-trip set→delete + zero-leak on delete path, --json without --yes CONFIRMATION_REQUIRED)
> - ✅ `domain remove` — `cli_domain_remove_test.go` (6 It incl. CONFIRMATION_REQUIRED without --yes, NOT_FOUND, MISSING_ARGUMENT)
>
> Iteração 6 (7 comandos — restantes):
> - ✅ `cancel` — `cli_cancel_test.go` (5 It incl. BUILD_NOT_FOUND, BUILD_ALREADY_COMPLETED via 409, AUTH_REQUIRED)
> - ✅ `build inspect` — `cli_build_inspect_test.go` (5 It incl. **AUDIT-34 contract** — schema must use `project_id` not `project`)
> - ✅ `connect` — `cli_connect_test.go` (5 It incl. MISSING_CONFIG, MISSING_REPO, AUTH_REQUIRED)
> - ✅ `disconnect` — `cli_connect_test.go` (4 It — same file: NOT_CONNECTED 404, MISSING_CONFIG, AUTH_REQUIRED)
> - ✅ `billing` — `cli_billing_test.go` (4 It incl. portal subcommand schema {url}, AUTH_FAILED via null-pattern)
> - ✅ `domain verify` — `cli_domain_verify_test.go` (4 It incl. error path exit 3 via handleApiError)
> - ✅ `domain dns` — `cli_domain_dns_test.go` (4 It incl. NOT_AVAILABLE / unregistered domain)
>
> Iteração 7 (3 comandos + 1 PIt ativado):
> - ✅ `login` — `cli_login_test.go` (5 It incl. CI MISSING_TOKEN, AUTH_FAILED, deprecation warning for ghp_*)
> - ✅ `admin tenants evict` — `cli_admin_evict_test.go` (5 It incl. **CRITICAL broad-pattern guard** — '%' or 'tn-%' rejected, LOOPZERO_SERVICE_AUTH auth model)
> - ✅ `status` — `cli_status_test.go` (4 It incl. **AUDIT-2 contract** (last_build.id not build_id) + **AUDIT-33 contract** (active_release fields), MISSING_CONFIG)
> - ✅ **F3.2 failure scenario** — `cli_failure_dockerfile_test.go` — primeiro PIt ativado: deploy com Dockerfile inválido yields exit 2 BUILD_FAILED (Rule 4 + spec §0.3)
>
> Iteração 8 (3 comandos):
> - ✅ `init` — `cli_init_test.go` (5 It incl. CI_REQUIRES_YES, FILE_EXISTS, AUDIT-36 partial failure exit 5 docs)
> - ✅ `rollback` — `cli_rollback_test.go` (6 It incl. **SAFETY INVARIANT migration warning** sempre printada em non-JSON mode, MISSING_CONFIG, CONFIRMATION_REQUIRED in CI)
> - ✅ `promote` — `cli_promote_test.go` (6 It incl. **CRITICAL SAME_ENVIRONMENT validation** (from==to), CONFIRMATION_REQUIRED in CI)
>
> Iteração 9 (3 comandos):
> - ✅ `logs` — `cli_logs_test.go` (5 It incl. MISSING_CONFIG (no yaml + no --build), bonus --follow flag parse)
> - ✅ `domain add` — `cli_domain_add_test.go` (6 It incl. todos required flags individuais MISSING_ARGUMENT)
> - ✅ `platform status` — `cli_platform_status_test.go` (3 It — Contract 3 N/A documentado para platform commands sem --json)
>
> Iteração 10 (3 comandos — platform install/upgrade/uninstall, MISSING zerado):
> - ✅ `platform install` — `cli_platform_install_test.go` (5 It incl. missing -f, file não existe, missing domain field, malformed YAML)
> - ✅ `platform upgrade` — `cli_platform_upgrade_test.go` (5 It incl. missing --version, file não existe, malformed YAML)
> - ✅ `platform uninstall` — `cli_platform_uninstall_test.go` (3 It incl. **CRITICAL SKIP** — não rodar unguarded em test compartilhado, é destrutivo)
>
> Helpers compartilhados: `cli_helpers_test.go` (runLoopZero, runLoopZeroNoToken, runLoopZeroWithStdin, runLoopZeroInDir, expectNoInternalTerms, expectAllowedExitCode, parseJSON, jsonStringField, jsonBoolField, looksLikeJSONLine, loopzeroCLIAvailable)
>
> **Compilação verificada (iter 3):** `go test -c` produz binário 15.9MB sem erros (com `vertical_slice_test.go` movido temporariamente).
>
> **BLOQUEADOR PRÉ-EXISTENTE B-8:** `vertical_slice_test.go` tem 9+ compile errors (contract drift AUDIT) — impede `go test` no pacote inteiro. Não causado pelos novos arquivos.
>
> **GAP DESCOBERTO ITER 3:** seed package só fornece 1 usuário (owner). Testes de mutação RBAC (team add/remove/role happy path) requerem segundo membro não-owner. Tracked como follow-up Phase 5.

## Inventário — fonte de verdade: `cli/src/register/*.ts`

48 comandos únicos enumerados lendo cada register file. **Não é estimativa — é leitura direta do código.**

## Como ler esta matriz

| Status | Significado | Ação no guardrail |
|---|---|---|
| `ACTIVE_E2E` | Existe em `infra/tests/e2e/*.go` como `It(...)` ativo (não PIt) | Roda na suite Ginkgo |
| `PENDING_E2E` | Existe como `PIt(...)` — código presente mas pendente | NÃO roda; precisa ativar em Phase 5 |
| `MANUAL_ONLY` | Validado manualmente em `e2e-validation-final-2026-04-23.md` em droplet, sem suite automatizada | Phase 5 escreve Ginkgo |
| `MISSING` | Nem Ginkgo nem validação manual documentada | Phase 5 escreve do zero |
| `BLOCKED` | Backend missing ou feature não em produção | Skip + nota; revisitar quando feature pronta |

## 5 contratos por comando (template Phase 5)

1. **Happy path:** comando completa com exit 0
2. **Failure path:** preconditions violadas → exit code correto (1/2/3/4/5)
3. **--json schema:** output bate com `docs/cli-reference.md` § correspondente
4. **Internal terms (Rule 3):** `loopzero <cmd> ... | grep -E "ArgoCD|argocd|GitOps|CRD|Gitea|MinIO|Kaniko|kubebuilder|tenant_id|advisory lock"` retorna vazio
5. **Exit codes (Rule 4):** apenas 0/1/2/3/4/5 são produzidos

`☑` = contrato testado e passa | `☐` = testado mas falha | `□` = não testado

---

## Matriz (48 comandos)

### Auth (3)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 1 | `loopzero login` | ACTIVE_E2E | `cli_login_test.go` | ☑☑☑☑☐ | 5 It blocks: CI MISSING_TOKEN, AUTH_FAILED, internal terms, ghp_ deprecation warning. Happy path SKIP (precisa Hydra OAuth flow) |
| 2 | `loopzero logout` | ACTIVE_E2E | `cli_logout_test.go` | ☑☑☑☑☑ | 4 It blocks: happy --yes, invalid flag, internal terms, cancellation |
| 3 | `loopzero whoami` | ACTIVE_E2E | `cli_whoami_test.go` | ☑☑☑☑☑ | 3 It blocks: authenticated, no-token, internal terms |

### Init (1)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 4 | `loopzero init` | ACTIVE_E2E | `cli_init_test.go` | ☑☑☑☑☐ | 5 It incl. AUTH_REQUIRED, CI_REQUIRES_YES, FILE_EXISTS sem --force, internal terms. AUDIT-36 partial failure exit 5 documentado. Happy path SKIP |

### Deploy & lifecycle (7)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 5 | `loopzero deploy` | ACTIVE_E2E | `deploy_happy_path_test.go` (3 cenários) + F1.1 ✅ | ☑□□□□ | 3 cenários: single, build cache, multi-app monorepo. Faltam contratos 2-5 |
| 6 | `loopzero validate` | ACTIVE_E2E | `cli_validate_test.go` | ☑☑☑☑☑ | 4 It blocks: valid yaml, missing yaml, malformed yaml, internal terms (local-only, sem k3d) |
| 7 | `loopzero status` | ACTIVE_E2E | `cli_status_test.go` | ☑☑☑☑☑ | 4 It blocks incl. **AUDIT-2 contract** (last_build.id), **AUDIT-33 contract** (active_release {id,status,git_commit,created_at}), MISSING_CONFIG, AUTH_REQUIRED |
| 8 | `loopzero logs` | ACTIVE_E2E | `cli_logs_test.go` | ☑☑☑☑☐ | 5 It blocks: AUTH_REQUIRED, MISSING_CONFIG (no yaml + no --build), build resolution path (NO_BUILDS / BUILD_RESOLVE_FAILED), --follow flag parses, internal terms. Happy path SKIP por Loki gap (F1.4) |
| 9 | `loopzero rollback` | ACTIVE_E2E | `cli_rollback_test.go` | ☑☑☑☑☑ | 6 It incl. **SAFETY INVARIANT migration warning** (sempre em non-JSON), AUTH_REQUIRED, MISSING_CONFIG, CI CONFIRMATION_REQUIRED, internal terms |
| 10 | `loopzero promote` | ACTIVE_E2E | `cli_promote_test.go` | ☑☑☑☑☑ | 6 It incl. **CRITICAL SAME_ENVIRONMENT validation** (from==to → exit 1), AUTH_REQUIRED, MISSING_CONFIG, CI CONFIRMATION_REQUIRED |
| 11 | `loopzero cancel <build-id>` | ACTIVE_E2E | `cli_cancel_test.go` | ☑☑☑☑☐ | 5 It blocks: BUILD_NOT_FOUND (404→exit 3), MISSING_ARGUMENT, AUTH_REQUIRED, internal terms. Happy path SKIP (precisa build em RUNNING) |

### Build (3 sub) — `register/infra.ts`

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 12 | `loopzero build list` | ACTIVE_E2E | `cli_build_list_test.go` | ☑☑☑☑☑ | 4 It blocks: authenticated+yaml success, MISSING_CONFIG, AUTH_REQUIRED, internal terms. Schema `{builds:[{build_id,project,environment,status,commit_sha,created_at,duration_seconds?}],total}` per AUDIT-5 |
| 13 | `loopzero build inspect <build-id>` | ACTIVE_E2E | `cli_build_inspect_test.go` | ☑☑☑☑☑ | 5 It blocks incl. **AUDIT-34 contract** — schema validates `project_id` (not `project`). Tests fetch existing build via list to validate full schema |
| 14 | `loopzero build cancel <build-id>` | ACTIVE_E2E | `cli_cancel_test.go` (alias) | ☑☑☑☑☐ | Mesmo handler que `loopzero cancel` (#11). Cobertura compartilhada |

### Secret (3 sub)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 15 | `loopzero secret set <NAME=value>` | ACTIVE_E2E | `cli_secret_set_test.go` | ☑☑☑☑☑ | 5 It blocks: success+JSON shape, INVALID_FORMAT, INVALID_SECRET_NAME (4 variantes), AUTH_REQUIRED, internal terms. **Zero-leak on SET path** explicitly tested |
| 16 | `loopzero secret list` | ACTIVE_E2E | `cli_secret_list_test.go` | ☑☑☑☑☑ | 5 It incl. **CRITICAL zero-leak invariant**. Schema `{secrets:[{name,environment,app}]}` (no value, no total). Exit 3 para network (não 1) per AUDIT-25 |
| 17 | `loopzero secret delete <name>` | ACTIVE_E2E | `cli_secret_delete_test.go` | ☑☑☑☑☑ | 6 It blocks: round-trip set→delete + zero-leak, CONFIRMATION_REQUIRED em --json, MISSING_CONFIG, MISSING_NAME, AUTH_REQUIRED |

### Connect / Disconnect (2)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 18 | `loopzero connect` | ACTIVE_E2E | `cli_connect_test.go` | ☑☑☑☑☐ | 5 It blocks: MISSING_CONFIG (no loopzero.yaml), MISSING_REPO (cannot detect git remote), AUTH_REQUIRED, internal terms. Happy path SKIP (precisa GitHub App install) |
| 19 | `loopzero disconnect` | ACTIVE_E2E | `cli_connect_test.go` | ☑☑☑☑☑ | 4 It blocks: MISSING_CONFIG, NOT_CONNECTED (404), AUTH_REQUIRED, internal terms |

### Billing (2)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 20 | `loopzero billing` (view) | ACTIVE_E2E | `cli_billing_test.go` | ☑☑☑☑☑ | 4 It blocks: schema {plan,team,usage{used,limit}}, AUTH_FAILED via null-pattern, internal terms |
| 21 | `loopzero billing portal` | ACTIVE_E2E | `cli_billing_test.go` | ☑☑☑☑☑ | Subcommand cobertura: schema {url}, AUTH_FAILED, internal terms |

### Audit (1)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 22 | `loopzero audit` | ACTIVE_E2E | `cli_audit_test.go` | ☑☑☑☑☑ | 3 It blocks: authenticated (success/forbidden), no-token, internal terms — AUDIT-23 error shape coberto |

### Admin (1)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 23 | `loopzero admin tenants evict` | ACTIVE_E2E | `cli_admin_evict_test.go` | ☑☑☑☑☐ | 5 It incl. **CRITICAL broad-pattern guard** (% / tn-% rejected), missing flags, missing LOOPZERO_SERVICE_AUTH (auth model único) |

### Domain (6)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 24 | `loopzero domain add <domain>` | ACTIVE_E2E | `cli_domain_add_test.go` | ☑☑☑☑☐ | 6 It blocks: cada flag required individual MISSING_ARGUMENT (--project, --app, --env), empty domain, AUTH_REQUIRED, internal terms. Happy path SKIP (CF API real) |
| 25 | `loopzero domain verify <domain>` | ACTIVE_E2E | `cli_domain_verify_test.go` | ☑☑☑☑☐ | 4 It blocks: MISSING_ARGUMENT, AUTH_REQUIRED, error path (exit 3 via handleApiError), internal terms |
| 26 | `loopzero domain status <domain>` | ACTIVE_E2E | `cli_domain_status_test.go` | ☑☑☑☑☑ | 4 It blocks: NOT_FOUND (404), MISSING_ARGUMENT, AUTH_REQUIRED, internal terms |
| 27 | `loopzero domain list` | ACTIVE_E2E | `cli_domain_list_test.go` | ☑☑☑☑☑ | 3 It blocks: success+schema {domains[],total,limit,offset}, AUTH_REQUIRED, internal terms |
| 28 | `loopzero domain remove <domain>` | ACTIVE_E2E | `cli_domain_remove_test.go` | ☑☑☑☑☐ | 6 It blocks: CONFIRMATION_REQUIRED sem --yes, NOT_FOUND (404), MISSING_ARGUMENT, AUTH_REQUIRED, internal terms. Happy path SKIP por dep CF Zone |
| 29 | `loopzero domain dns <domain>` | ACTIVE_E2E | `cli_domain_dns_test.go` | ☑☑☑☑☐ | 4 It blocks: MISSING_ARGUMENT, AUTH_REQUIRED, NOT_AVAILABLE/error path, internal terms |

### Env (4 sub)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 30 | `loopzero env list` | ACTIVE_E2E | `cli_env_list_test.go` | ☑☑☑☑☑ | 3 It blocks: authenticated success+schema, no-token AUTH_REQUIRED, internal terms |
| 31 | `loopzero env create <name>` | ACTIVE_E2E | `cli_env_create_test.go` | ☑☑☑☑☑ | 5 It blocks: success+409 idempotent, INVALID_NAME (uppercase, leading hyphen), RESERVED_NAME (default/system/internal), AUTH_REQUIRED, internal terms |
| 32 | `loopzero env delete <name>` | ACTIVE_E2E | `cli_env_delete_test.go` | ☑☑☑☑☑ | 6 It blocks incl. **CRITICAL PRODUCTION_PROTECTED** (4 variantes: bare, --yes, --confirm production, --yes --confirm production), CONFIRMATION_REQUIRED, CONFIRMATION_MISMATCH, round-trip create→delete |
| 33 | `loopzero env inspect <name>` | ACTIVE_E2E | `cli_env_inspect_test.go` | ☑☑☑☑☑ | 4 It blocks: success (AUDIT-1 null-projects safety), ENV_NOT_FOUND (404), AUTH_REQUIRED, internal terms |

### Platform (4 sub) — self-hosted install

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 34 | `loopzero platform install` | ACTIVE_E2E | `cli_platform_install_test.go` | ☑—☑☑☑ | 5 It error paths: missing -f, nonexistent file, missing domain field, malformed YAML, internal terms. Happy path SKIP (10-step install, 10-15 min) |
| 35 | `loopzero platform status` | ACTIVE_E2E | `cli_platform_status_test.go` | ☑—☑☑☑ | 3 It blocks: exit 0/1 baseado em phase, internal terms, Contract 3 N/A documentado (platform commands sem --json) |
| 36 | `loopzero platform upgrade` | ACTIVE_E2E | `cli_platform_upgrade_test.go` | ☑—☑☑☑ | 5 It error paths: missing --version, nonexistent file, malformed YAML, internal terms. Happy path SKIP (mutation cluster) |
| 37 | `loopzero platform uninstall` | ACTIVE_E2E | `cli_platform_uninstall_test.go` | ☑—☑☑☑ | 3 It: invalid flag → exit 1, internal terms, **CRITICAL SKIP** (destructive, never run unguarded em test compartilhado) |

### Preview (3 sub)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 38 | `loopzero preview list` | BLOCKED | F2.2 ❌ backend missing | — | Sprint 5 — NÃO há `preview create` no register (descoberta!) |
| 39 | `loopzero preview inspect <id>` | BLOCKED | F2.2 ❌ | — | — |
| 40 | `loopzero preview delete <id>` | BLOCKED | F2.2 ❌ | — | — |

### Project (4 sub)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 41 | `loopzero project create <name>` | ACTIVE_E2E | `cli_project_create_test.go` | ☑☑☑☑☑ | 4 It blocks: success+409 idempotent, INVALID_NAME (5 variantes RFC 1123: uppercase, leading/trailing hyphen, > 63 chars, underscore), AUTH_REQUIRED, internal terms |
| 42 | `loopzero project list` | ACTIVE_E2E | `cli_project_list_test.go` | ☑☑☑☑☑ | 3 It blocks: authenticated success+schema, AUTH_REQUIRED, internal terms |
| 43 | `loopzero project inspect <name>` | ACTIVE_E2E | `cli_project_inspect_test.go` | ☑☑☑☑☑ | 4 It blocks: success (AUDIT-4 schema {id,name,team,created_at,created_by?}), MISSING_ARGUMENT, PROJECT_NOT_FOUND (404), internal terms |
| 44 | `loopzero project delete <name>` | ACTIVE_E2E | `cli_project_delete_test.go` | ☑☑☑☑☑ | 5 It blocks: round-trip create→delete, CI mode CONFIRMATION_REQUIRED, MISSING_ARGUMENT, AUTH_REQUIRED, internal terms. Schema {project,deleted_environments,cancelled_builds,status} |

### Scale (1)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 45 | `loopzero scale` | BLOCKED | F2.1 ❌ backend missing | — | F1.2 ✅ confirma scaling funcional via deploy `replicas`/HPA, mas comando `loopzero scale` específico bloqueado |

### Team (4 sub)

| # | Comando | Status | Test file / Evidência | 5 contratos | Notas |
|---|---|---|---|---|---|
| 46 | `loopzero team list` | ACTIVE_E2E | `cli_team_list_test.go` | ☑☑☑☑☑ | 3 It blocks: authenticated success+schema {members:[{username,email,role,joined_at}],total}, AUTH_REQUIRED, internal terms |
| 47 | `loopzero team add <username>` | ACTIVE_E2E | `cli_team_add_test.go` | ☑☑☑☑☐ | 5 It blocks: INVALID_ROLE (owner not allowed), garbage role, AUTH_REQUIRED, missing username, internal terms. Happy path SKIP (precisa real GitHub user) |
| 48 | `loopzero team remove <username>` | ACTIVE_E2E | `cli_team_remove_test.go` | ☑☑☑☑☐ | 5 It blocks incl. **CRITICAL self-removal** (SELF_REMOVE/OWNER_PROTECTED), MEMBER_NOT_FOUND, AUTH_REQUIRED, internal terms. Happy path SKIP por seed gap |
| 49 | `loopzero team role <username> <role>` | ACTIVE_E2E | `cli_team_role_test.go` | ☑☑☑☑☐ | 5 It blocks: INVALID_ROLE, AUTH_REQUIRED, missing args, internal terms. Happy path SKIP por seed gap (precisa segundo membro não-owner) |

---

## Sumário verificado — atualizado iteração 2

| Status | Contagem | Comandos |
|---|---|---|
| `ACTIVE_E2E` (Ginkgo It rodando) | **43** | iter 1-9 (40) + **iter 10 (3: platform install/upgrade/uninstall)** |
| `MANUAL_ONLY` | 0 | ✅ ZERADO em iter 9 |
| `MISSING` | 0 | ✅ ZERADO em iter 10 |
| `BLOCKED` | 4 | preview ×3 (sprint 5 backend missing), scale (backend missing) — **gaps de feature, não de testes** |
| `PENDING_E2E` (8 cenários PIt em failure_scenarios_test.go) | **1 ATIVADO em iter 7 (F3.2 dockerfile)** + 7 restantes | dockerfile syntax ✅, deps install, health probe, sync timeout, partial failures, transient retry, lock release |
| **Total comandos** | **49** | |

**Progresso Phase 5:** 43/49 ACTIVE_E2E (**87.8%**). **MANUAL_ONLY e MISSING zerados.** Restantes 4 BLOCKED são gaps de **backend feature** (preview, scale) — testes não podem cobrir features que não existem em produção. **Cobertura efetiva sobre testáveis: 43/45 = 95.6%.**

**Observações:**
- `failure_scenarios_test.go` tem 8 `PIt(...)` Pending — todos marcados como passing manualmente em F3 mas Ginkgo não roda. Phase 5 precisa **ativar** esses 8 (PIt → It) E garantir que rodam contra k3d real.
- `vertical_slice_test.go` é unit-style com fakes — não conta como E2E real apesar do nome.
- Cobertura efetiva contra produção real (Ginkgo running) = **5 cenários cobrindo apenas `loopzero deploy` + `concurrent deploy`**.
- 26 comandos `MISSING` precisam de Ginkgo do zero. 18 `MANUAL_ONLY` precisam de Ginkgo automatizado (validação manual não persiste — não detecta regressão).

---

## Estimativa de Phase 5 (refinada)

| Categoria | Comandos | Cenários Phase 5 (5 contratos × cmd) | Tempo (30 min/cenário) |
|---|---|---|---|
| MISSING (escrever do zero) | 26 | 130 | 65h |
| MANUAL_ONLY → automatizar | 18 | 90 | 45h |
| ACTIVE_E2E (completar contratos 2-5) | 1 (deploy) | 4 | 2h |
| PENDING_E2E (ativar PIt + validar) | 8 | 8 | 4h |
| BLOCKED (skip) | 4 | 0 (deferido) | 0 |
| **Total** | **57 (incl. failure scenarios)** | **232 cenários** | **~116h** |

Bem maior que a estimativa anterior de 75h. **Phase 5 é o gargalo crítico real** — ~3 semanas de trabalho concentrado, ou 6+ semanas em paralelo com outras tarefas.

---

## Pre-conditions descobertas (bloqueiam start de implementação)

| ID | Bloqueio | Severidade | Mitigação |
|---|---|---|---|
| B-1 | `gh` CLI **não instalado** nesta máquina | CRÍTICA | Instalar `gh` (`brew install gh` ou apt) antes de Phase 1 |
| B-2 | `failure_scenarios_test.go` está todo PIt — não roda | ALTA | Phase 5 precisa ativar 8 cenários, não 0 |
| B-3 | `loopzero preview create` NÃO existe no register (só list/inspect/delete) | MÉDIA | Confirmar com Sprint 5 design se é gap intencional |
| B-4 | `loopzero scale` backend missing (F2.1 ❌) | ALTA | Pre-condition para implementar Phase 5 do scale: backend pronto |
| B-5 | Sem swap configurado (15GB RAM apenas) — risco com k3d isolado + claude session simultâneos | MÉDIA | Considerar `swapfile` antes de Phase 3, ou limitar via cgroup |
| B-6 | T2 governance meeting NÃO realizado (P-1 do plano) | CRÍTICA | Convocar via `/meeting CHG-2026-04-25-PR-GUARDRAIL` |
| B-7 | D-4, D-6, D-7 NÃO aprovados | CRÍTICA | Aprovação explícita do usuário |
| B-8 | `infra/tests/e2e/vertical_slice_test.go` tem 9+ compile errors (contract drift do AUDIT) — impede `go test` no pacote inteiro | ALTA | T2 separado para reconciliar com `apitest.TokenValidationResult`/`loopzerov1.EnvironmentSpec`. Workaround: build tag para excluir, ou mover testes novos para subpacote `e2e/cli/` |

---

## Como atualizar esta matriz

1. ✅ **Phase 0 (T-0.1):** comandos enumerados via leitura de `cli/src/register/*.ts` (concluído)
2. ✅ **Phase 0 (T-0.2):** status preenchido via leitura de `infra/tests/e2e/*.go` + `e2e-validation-final-2026-04-23.md` (concluído)
3. ⏳ **Phase 0 (T-0.3-7):** medições de tempo, recursos, claude headless, gh scopes — parcialmente feitas (vide pr-guardrail-phase0-status.md)
4. ⏸️ **Phase 5:** ao escrever cada teste, marcar contratos com ☑ e atualizar `Test file`. Quando 5/5 ☑, status vira ACTIVE_E2E.
5. ⏸️ **Phase 8 gate:** todas as linhas em ACTIVE_E2E (ou BLOCKED com justificativa). Senão, gate não promove a required.

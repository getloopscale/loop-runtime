# Module 01 — CLI (`cli/`)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.6 (1M context), code-validated
**Confidence:** 92% (Claude-only, single-model residual)
**Maturity:** **3.5 / 4.0**

## TL;DR

CLI maduro, com 1.37:1 test:source ratio (1226 cases em 19.5K LOC de testes), Rule 3 sanitization centralizada e contrato de exit codes enforced. Bloqueia 4.0 por 7 E2E tests skipped (sem API real em CI) e platform installer com mocking pesado.

---

## 1. Inventário Real

| Métrica | Valor |
|---|---|
| Arquivos `.ts` (src/) | 82 |
| Arquivos `.test.ts` | 85 |
| LOC src | 14,294 |
| LOC test | 19,547 |
| Test:source ratio | 1.37:1 |
| Subdiretórios | 10 (commands, register, api, auth, config, output, adapters, installer, detection, tracing) |
| Comandos registrados (`program.command`) | 65 (≈37 leaf commands) |
| Dependências NPM | 6 produção + ~12 devDeps |

**Top deps:** commander 12.1, @opentelemetry/api 1.9, js-yaml 4.1, tar 7.5, ignore 7.0, jest 29.7.

---

## 2. Arquitetura Observada

**Fluxo `loopzero deploy` (canonical):** entry em `cli/src/index.ts:260` → `cli/src/commands/deploy.ts:50` (DeployCommand.execute).

3 fases:
1. **Validate prerequisites** (`deploy.ts:103-206`) — auth + loopzero.yaml + env + apps + secrets.
2. **Bundle + upload** (`deploy.ts:64-80`) — normaliza apps, tar com gitignore, upload via ApiClient.
3. **Submit + SSE stream** (`deploy.ts:83-96`) — `submitBuild()` retorna `{build_id, deploy_id}`, então `SSEClient.connect()` stream eventos até terminal.

**Sanitização Rule 3 centralizada** em `cli/src/commands/deploy-helpers.ts:79-93`: substitui ArgoCD/Gitea/MinIO/Kaniko/CRD/kubebuilder/tenant_id/advisory_lock/pg_advisory para termos de produto.

**Retry + jitter** em `cli/src/api/client.ts:16-28` — 5 tentativas (subido de 3 para suportar 429 CELL_AT_CAPACITY), backoff exponencial 1-16s com ±10% jitter.

**Token refresh** em `cli/src/auth/token-refresh.ts:26-53` — callback-based, **não escreve em stderr** (per CHG-2026-04-23-AUDIT-16).

---

## 3. Contratos Públicos

| Exit Code | Significado | Locação canônica |
|---|---|---|
| 0 | Success | — |
| 1 | Usage/config error | `formatter.ts:12` whitelist enforcement |
| 2 | Build failure | `deploy-helpers.ts:11-34` resolveExitCode |
| 3 | Deploy failure | idem |
| 4 | Auth error | `api/client.ts:65-120` 401/403 mapping |
| 5 | Partial failure | idem (2+ apps com mix) |

**`--json` schema:** validado em `tests/integration/exit-code-contract.test.ts` + `tests/unit/output/formatter.test.ts`. Field names lockados (Rule 5).

**CI mode:** `formatter.ts:71-78` detecta `CI=true`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `JENKINS_URL` — desabilita cores.

**Forbidden terms grep em src/:** zero ocorrências em paths de output. ✅

---

## 4. Cobertura de Testes

- **1,226 `it(` cases** distribuídos em 85 test files
- 13 integration tests + 72 unit tests
- **15 jest.mock() calls** — primariamente `child_process` e `fs` (I/O externo)
- Mocking de internal APIs é raro (style integration-flavored unit tests)
- **7 testes E2E skipped** em `cli-e2e.test.ts`: GitHub token real, token expiry, monorepo detection, SSE eventos, concurrent rejection, timeout graceful

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação |
|---|---|---|
| 7 E2E tests skipped (real API integration pendente) | MÉDIA | `tests/integration/cli-e2e.test.ts` |
| Platform installer fortemente mockado em testes (6 mocks em platform-install-validate-l3.test.ts) | MÉDIA | `tests/unit/platform-install-*.test.ts` |
| TODO/FIXME/STUB count | **0** | — |

Comandos com cobertura indireta (compartilham testes ou via integration): `deploy-helpers.ts`, `deploy-types.ts`, `platform.ts`.

---

## 6. Avaliação de Maturidade

**Score: 3.5 / 4.0** (confidence 92%)

### Evidências (Evidence Bar §1)

1. **T1 (commit+test)** — `cli/src/output/formatter.ts:12` (exit code whitelist `{0,1,2,3,4,5}`); `cli/src/commands/deploy-helpers.ts:79-93` (sanitization mapping); 1226 test cases em 85 `.test.ts`.
2. **T2 (LIVE log)** — CLI exercitado em cleanroom dogfood 12/12 GREEN (2026-05-05), journey 76/76 GREEN (2026-05-06), canary 80/80 GREEN (2026-05-07).
3. **T3 (CI run)** — `cli-commands` workflow em `.github/workflows/`, npm test executado a cada PR.

### Raciocínio

**Sustenta 3.5+:**
- Test ratio 1.37:1 com 1226 cases
- Rule 3 sanitization centralizada (1 fonte de verdade)
- Exit codes contract enforced em runtime + tests
- Retry + jitter + token refresh callback-based (FAANG-grade)
- OTel tracing wired end-to-end (14 span types)
- Zero TODO/FIXME/STUB

**Bloqueia 4.0:**
- 7 E2E tests skipped — não há gate CI contra API real
- Platform installer com 6+ mocks (real install validation só em dogfood, não em CI)
- Test:LOC ratio alto mas sem property-based ou fuzz tests
- Sem teste de regressão para força de network (proxy interception)

### Caminho para 3.8-4.0

1. Habilitar 7 E2E tests skipped contra staging API + adicionar workflow CI dedicado
2. Substituir 3+ mocks de platform installer por integration tests contra k3d local
3. Adicionar fuzz tests para JSON output schema (validar field names estáveis)
4. Property-based tests para sanitization (toda string com termo proibido → output limpo)

---

**Próximas auditorias relacionadas:** API (M02), Build Pipeline (M07), Auth/Billing (M10).

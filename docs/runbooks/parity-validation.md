# Infra Parity Validation Runbook

> CHG-2026-04-30-INFRA-PARITY — Phases 5-8 (degraus 1-4 do staircase de validação)
>
> Cobre: como provar que `loopzero platform install` produz cluster funcional em ambiente virgem; ordem de execução; troubleshooting; custos por degrau.

## Staircase

| Degrau | Ambiente | Tempo | Custo | Script | Cobertura |
|---|---|---|---|---|---|
| 1 | k3d virgin local | ~15min | $0 | `infra/tests/parity/k3d-virgin-install.sh` | Helmfile L2, secret bootstrap, regression iter-13 |
| 2 | Droplet DO (1 VM, k3d) | ~30min | ~$0.50 | `droplet-install.sh` (T6.1, pendente) | + caminho de rede pública (CF tunnel, ACME) |
| 3 | DOKS `--provider digitalocean` | ~60min | ~$5-10 | `doks-install.sh` (T7.1, pendente) | Pulumi L1 + Helmfile L2 + cross-cluster wiring + adopt-or-create |
| 4 | DOKS + L3 100-tenant smoke | +~30min | ~$10 | `doks-install.sh --validate-l3` (T8.1, pendente) | Capacidade Gitea/Loki/runtime pool sob carga |

**Regra de ouro:** se degrau N falha, NÃO subir para N+1. O custo cresce e o gap está na camada que falhou.

## Pré-requisitos (todos os degraus)

- Host com `docker`, `k3d`, `helmfile`, `kubectl`, `loopzero` CLI no PATH.
- Para degraus 2-4: `doctl` autenticado (`doctl auth init`).
- Phase 0 do plan completa (preflight 16/16 PASS).
- Phase 1 imagens promovidas (registry tem `loopzero-api:latest` correspondendo ao runtime atual).

## Quick reference (Taskfile targets)

```bash
task parity:preflight       # offline gate, <2s — shellcheck + audit + dry-runs
task parity:audit           # validate regression matrix integrity
task parity:audit:golden    # confirm audit script itself isn't regressing
task parity:k3d-virgin      # T5.1 degrau 1 ($0, ~10min) — LIVE
task parity:droplet -- --dry-run    # T6.1 degrau 2 — offline preflight
task parity:droplet         # T6.1 degrau 2 (~$0.50, ~30min) — LIVE
task parity:doks -- --dry-run       # T7.1 degrau 3 — offline preflight
task parity:doks            # T7.1 degrau 3 (~$5, ~60min) — LIVE
```

CI gate: `.github/workflows/parity-validation.yml` runs `parity:preflight` + parity-related CLI/Pulumi tests on every PR touching parity files.

## Degrau 1 — k3d virgin local

```bash
task parity:k3d-virgin   # or: bash infra/tests/parity/k3d-virgin-install.sh
```

O script (validated GREEN run 6 / 2026-04-30 — exit 0, "Platform phase: Ready"):
1. Pre-flight: requer ≥7 GiB RAM disponível pós-pause de build+workload (Achado #131).
2. Pausa `loopzero-build` E `loopzero-workload` clusters (libera ~3.5 GiB RAM total — Achado #131).
3. `docker image prune -f` (dangling-only, NÃO `system prune --volumes` que destruía Exited k3d clusters — Achado #30).
4. Cria k3d cluster virgem `loopzero-virgin` com `--servers 1 --agents 2` (capacity para 27 charts em paralelo).
5. `--k3s-arg "--disable=traefik@server:0"` (Achado #31 — k3d default Traefik bloqueava install do nosso Traefik release).
6. Roda `loopzero platform install -f standalone-values.yaml --profile minimal --no-restore-secrets`.
7. Cleanup via trap on EXIT: deleta virgin + reinicia `loopzero-build` E `loopzero-workload`.

**Trap garante cleanup mesmo em falha.** Verificado em 6 runs consecutivos.

### Checklist de "GREEN":

- [x] Helmfile sync completa em <5min (run 6: 296.9s)
- [x] Todos 4 charts LoopZero deployed (loopzero-api, loopzero-controller, tenant-controller, loopzero-cloud)
- [x] `loopzero platform status` retorna phase=Ready (Achados #34/#35/#36/#37 fixed iter atual)
- [x] Cleanup trap restaura ambos clusters pausados

### Troubleshooting

| Sintoma | Causa provável | Fix |
|---|---|---|
| Pre-flight RAM check falha | <7 GiB disponível pós-pause | Stop outras workloads no host |
| postgresql up mas kratos/hydra/harbor crash em "database does not exist" | Achado #29 — chart 18.6.2 ignorou `initdb.scripts` antigo | Verifique `infra/helm/postgresql/values.yaml` tem `primary.initdb.scripts` (não top-level) |
| ArgoCD chart fail em IngressClass adoption | Achado #31 — k3d default Traefik conflita | Confirmar `--k3s-arg "--disable=traefik@server:0"` no comando k3d cluster create |
| `helmfile sync` falha em "image not found" | Registry local não tem `loopzero-api:latest` | `tilt up` ou rebuild manual antes de virgin install |
| Pods em CreateContainerConfigError | Secret missing | `--restore-secrets <file>` para passar backup; ou `--no-restore-secrets` se for cluster genuinamente novo |
| `pulumi up` chamado quando deveria ser standalone | Values.yaml tem `provider:` não-vazio | Remover linha `provider:` do values |
| Step 6 reporta NotInstalled mas pods Running | Achados #34-#37 (label/sentinel/chart name drifts) | Atualizar para HEAD develop — fix em cli/src/installer/status.ts |

## Degrau 2 — Droplet DO

```bash
task parity:droplet -- --dry-run    # offline validation
task parity:droplet                  # LIVE (~$0.50, ~30min)
```

Pre-conditions:
- `doctl` no PATH + `DIGITALOCEAN_ACCESS_TOKEN` setado
- SSH key registrada em DO + fingerprint em `DROPLET_SSH_KEY_FP`

Provisiona Droplet `s-2vcpu-4gb-amd` em `sfo3` (ubuntu-24-04). Cloud-init (`droplet-cloud-init.sh`) instala docker, kubectl, helm, helmfile, k3d, doctl, node, loopzero CLI. Após handshake `/var/log/loopzero-parity-ready`, dispara `k3d-virgin-install.sh` remoto via SSH. Cleanup trap destrói Droplet sempre.

## Degrau 3 — DOKS

```bash
task parity:doks -- --dry-run    # pulumi preview only
task parity:doks                  # LIVE (~$5, ~60min)
```

Pre-conditions:
- `pulumi` no PATH + `DIGITALOCEAN_ACCESS_TOKEN` + AWS keys (S3 state backend)
- `infra/pulumi/node_modules` populated (`npm ci`)

Cria stack Pulumi `parity-test`, exporta kubeconfig, roda `loopzero platform install --provider digitalocean`, dogfood subset (journey-01,03,08), depois `pulumi destroy` com timer 300s (DoD #2 GREEN se completar <5min).

## Degrau 4 — L3 100-tenant smoke

```bash
loopzero platform install -f values.yaml --provider digitalocean --validate-l3
```

Após Step 6 (phase=Ready), Step 6.5 condicional dispara `go test -tags=l3 -timeout=30m -run TestRealDeploy_L3RealisticWithChurn ./infra/tests/load`. ADR D5 release gate: SLO p95 ≤ 2min create-to-active, error rate ≤ 5%, 100 tenants HTTP 200. Falha → exit 5 (partial — install OK mas capacidade miss).

## Recovery (cluster destruído mid-run)

Se degrau 3 ou 4 falhar mid-run, executar:
```bash
cd infra/pulumi && npx pulumi destroy -s parity-test --yes
```
adopt-or-create cuida de recursos órfãos. Script idempotente.

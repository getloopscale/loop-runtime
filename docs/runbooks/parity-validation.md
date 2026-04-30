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

## Degrau 1 — k3d virgin local

```bash
bash infra/tests/parity/k3d-virgin-install.sh
```

O script:
1. Pausa `loopzero-build` cluster (libera ~1.9 GiB RAM).
2. `docker system prune -f` (opcional via `PRUNE=0`).
3. Cria k3d cluster virgem `loopzero-virgin` com agents=1.
4. Roda `loopzero platform install -f standalone-values.yaml --profile minimal --no-restore-secrets`.
5. (Phase 5 follow-up: roda dogfood journey-01 contra virgin).
6. Cleanup: deleta virgin + reinicia `loopzero-build`.

**Trap garante cleanup mesmo em falha.**

### Checklist de "GREEN":

- [ ] Helmfile sync completa em <10min
- [ ] Todos pods `Running` em loopzero-system + traefik + cert-manager
- [ ] `loopzero platform status` retorna phase=Ready (após resolver bug F-08 label app.kubernetes.io/name)
- [ ] Zero ArgoCD Apps em sync=Unknown (tudo Synced ou OutOfSync com diff conhecido)

### Troubleshooting

| Sintoma | Causa provável | Fix |
|---|---|---|
| `helmfile sync` falha em "image not found" | Registry local não tem `loopzero-api:latest` | Phase 1 T1.1: `docker tag achado28 latest && docker push localhost:5000/loopzero-api:latest` |
| Pods em CreateContainerConfigError | Secret missing | `--restore-secrets <file>` para passar backup; ou `--no-restore-secrets` se for cluster genuinamente novo |
| `pulumi up` chamado quando deveria ser standalone | Values.yaml tem `provider:` não-vazio | Remover linha `provider:` do values |
| Cluster sobe mas sem CNI | k3d default flannel pode falhar em hosts low-mem | Reduzir agents para 0 (single node) |

## Degrau 2 — Droplet DO

(Pendente T6.1.) Provisiona Droplet ubuntu-24-04 size s-2vcpu-4gb-amd, cloud-init instala k3d + loopzero CLI, roda degrau 1 dentro.

## Degrau 3 — DOKS

(Pendente T7.1.) `loopzero platform install --provider digitalocean -f values.yaml` cria stack Pulumi `parity-test`, faz helmfile sync nas 4 DOKS clusters (mgmt+build+runtime+runtime-2), roda dogfood subset, depois `pulumi destroy` validating <5min completion.

## Degrau 4 — L3 100-tenant

(Pendente T8.1.) Após degrau 3, flag `--validate-l3` dispara load test que cria 100 tenants concorrentes e mede SLO p95 ≤ 2min de tenant-create-to-active.

## Recovery (cluster destruído mid-run)

Se degrau 3 ou 4 falhar mid-run, executar:
```bash
cd infra/pulumi && npx pulumi destroy -s parity-test --yes
```
adopt-or-create cuida de recursos órfãos. Script idempotente.

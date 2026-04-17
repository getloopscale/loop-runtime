# Runbook: Runtime Capacity Bump (T-08)

**Context:** T-08 em `docs/plans/50k-immediate-focus.md`. Quando a DO
droplet quota for aprovada, este é o procedimento para aplicar o
runtime cluster bump + limpar o drift dos overflow pools criados por
`doctl` durante as iterações L3 v1-v14.

**Pré-condição:** Paulo recebeu confirmação DO de quota increase
(tipicamente email + account dashboard mostra novo limit).

---

## Estado atual (antes do bump)

| Componente | Valor live | Pulumi source |
|---|---|---|
| Runtime default pool | `s-2vcpu-4gb` × 3 (Pulumi-managed) | ✅ alinhado |
| Runtime overflow pool #1 | `overflow-s-2vcpu-2gb` × 1 (doctl) | ❌ órfão |
| Runtime overflow pool #2 | `overflow2` × 2 (doctl) | ❌ órfão |
| Runtime prewarm pool | `l3-prewarm-*` × 3 (doctl) | ❌ órfão |
| Total allocatable memory | ~18Gi | — |
| L3 max apps schedulable | ~62-75/100 | — |

**Math post-bump:** 6× `s-4vcpu-8gb` = 24 CPU × 6 = 48 allocatable
CPU, 6 × 8Gi = 48Gi memory. 100 apps × 128Mi = 12.8Gi (26% headroom).

---

## Passo 1 — Snapshot antes

```bash
# Salvar estado pré-bump para rollback se algo der errado
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-runtime get nodes -o yaml > /tmp/runtime-nodes-pre-t08.yaml
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-runtime get pods -A -o yaml > /tmp/runtime-pods-pre-t08.yaml

# Verificar cluster livre (sem tenants L3 em voo)
kubectl --context do-sfo3-loopzero-infra-dev-sfo3-mgmt get tenants -n platform-system \
  --no-headers | grep -c tn-lt
# Deve retornar 0. Se não, rodar:
#   loopzero admin tenants evict --pattern 'tn-lt-%' --yes
```

## Passo 2 — Editar Pulumi.dev.yaml

```bash
cd /home/paulo/loopzero/infra/pulumi

# Mudança target (NÃO aplicar ainda — só editar):
# loopzero-infra:runtimeNodeClass: small       → medium
# loopzero-infra:runtimeMinNodes: "2"          → "4"
# loopzero-infra:runtimeMaxNodes: "3"          → "6"

# Use yq para editar idempotentemente:
yq -i '.config."loopzero-infra:runtimeNodeClass" = "medium"' Pulumi.dev.yaml
yq -i '.config."loopzero-infra:runtimeMinNodes" = "4"' Pulumi.dev.yaml
yq -i '.config."loopzero-infra:runtimeMaxNodes" = "6"' Pulumi.dev.yaml

git diff Pulumi.dev.yaml  # confirmar 3 linhas trocadas
```

**Node class map (para referência):**
- `small` → `s-2vcpu-4gb` (atual)
- `medium` → `s-4vcpu-8gb-amd` (target — AMD porque `s-4vcpu-8gb`
  indisponível em SFO3)
- `large` → `s-8vcpu-16gb`

## Passo 3 — Pulumi preview (CRÍTICO — drift no dev)

```bash
npx pulumi preview --stack dev --diff 2>&1 | tee /tmp/pulumi-preview-t08.log
```

**Verificar na saída:**

| Operação | Status esperado |
|---|---|
| `update` em `kubernetes:do:loopzero-infra-dev-sfo3-runtime` (node class change) | ✅ esperado |
| `delete` em nodes existentes do default pool | ✅ esperado (replace) |
| `create` em novo default pool | ✅ esperado |
| `delete` em `mgmt-bootstrap:gitea-hpa` | 🚨 **ABORT** (drift) |
| `delete` em qualquer cluster inteiro | 🚨 **ABORT** (drift) |
| `delete` em overflow pools (`overflow-s-2vcpu-2gb`, etc) | ❌ não vai aparecer (são órfãos, não estão no Pulumi) |

⚠️ Se o preview mostrar delete de clusters ou componentes não
relacionados ao runtime pool — **PARE**. O Pulumi state do dev tem
drift conhecido (memory feedback `pulumi_state_drift`). Resolver drift
antes de aplicar (ver "Resolvendo drift" abaixo).

## Passo 4 — Pulumi up

```bash
npx pulumi up --stack dev --target-dependents \
  --target 'urn:pulumi:dev::loopzero-infra::digitalocean:index/kubernetesCluster:KubernetesCluster::loopzero-infra-dev-sfo3-runtime'
```

`--target` + `--target-dependents` limita o apply ao runtime cluster
+ coisas que dependem dele — evita tocar mgmt/build.

Confirmar prompt interativo só após re-verificar o que será
modificado.

## Passo 5 — Cleanup manual dos overflow pools

Pulumi não gerencia os overflow pools criados via doctl. Remover
manualmente após o default pool novo estar saudável:

```bash
# Listar pools do cluster
doctl kubernetes cluster node-pool list loopzero-infra-dev-sfo3-runtime

# Remover cada overflow/prewarm (substitua <pool-id>)
doctl kubernetes cluster node-pool delete loopzero-infra-dev-sfo3-runtime \
  --node-pool <pool-id-overflow-s-2vcpu-2gb>

doctl kubernetes cluster node-pool delete loopzero-infra-dev-sfo3-runtime \
  --node-pool <pool-id-overflow2>

doctl kubernetes cluster node-pool delete loopzero-infra-dev-sfo3-runtime \
  --node-pool <pool-id-l3-prewarm>
```

**Ordem:** sempre o default pool (Pulumi-managed) de pé ANTES de
deletar overflow, senão o cluster fica sem capacity no meio da
transição.

## Passo 6 — Verificação post-bump

```bash
# 6 nodes, todos s-4vcpu-8gb-amd, ~7Gi allocatable
kubectl --context runtime get nodes \
  -o custom-columns="NAME:.metadata.name,CPU:.status.allocatable.cpu,MEM:.status.allocatable.memory"

# Sem pods pending
kubectl --context runtime get pods -A --field-selector=status.phase=Pending \
  --no-headers | wc -l
# Esperado: 0

# Platform pods healthy (Traefik runtime, cert-manager se local, etc)
kubectl --context runtime get pods -A --no-headers | \
  grep -v "Running\|Completed" | wc -l
# Esperado: 0
```

## Passo 7 — L3 v15 validação

Com o cluster novo, rodar L3 e confirmar ≥95/100:

```bash
export LOOPZERO_REAL_DEPLOY=1 LOOPZERO_LOAD_LEVEL=l3
export LOOPZERO_API_URL=https://api.dev.loopzero.example
export LOOPZERO_APP_DOMAIN=dev.loopzero.example
export LOOPZERO_SERVICE_AUTH=$(kubectl --context mgmt -n platform-system \
  get secret loopzero-api-secrets -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)

go test ./infra/tests/load/ -run TestRealDeploy_L3_100Tenants -v -timeout 60m \
  2>&1 | tee /tmp/l3-v15.log

# Esperado: Apps Live >= 95/100.
```

Se passar: rodar stability loop para fechar Gate #1:

```bash
tools/l3-stability-loop.sh --max-runs 10 --threshold 95
# Esperado: summary.tsv com 10 runs todos ≥95/100
```

---

## Resolvendo drift (se Passo 3 bloqueou)

Drift típico no dev stack:
- Clusters foram criados antes do código Pulumi atual
- Recursos órfãos (overflow pools, helm releases)
- Pulumi.dev.yaml config não reflete o que foi criado manualmente

**Procedimento conservador:**

1. Identificar recursos divergentes no preview
2. Para cada recurso que Pulumi quer deletar mas deve ser mantido:
   `npx pulumi state delete <urn>` remove da tracking sem deletar live
3. Para cada recurso live mas não trackado que deve ser adotado:
   `npx pulumi import <resource-type> <name> <id>`
4. Re-rodar preview — o diff deve ficar contido ao runtime pool
5. Só então `pulumi up --target ...`

**Não faça:**
- `pulumi up` sem `--target` no dev stack (memory feedback)
- `pulumi destroy` (deleta tudo incluindo PG com dados)
- `pulumi refresh` sem snapshot — pode re-importar estados indesejados

---

## Rollback

Se post-bump o cluster ficar degradado:

1. `pulumi rollback --stack dev` (último snapshot bom) — se drift
   permite, ou
2. Manual: editar Pulumi.dev.yaml de volta (`medium` → `small`,
   etc), `pulumi up --target runtime`
3. Re-adicionar overflow pools via doctl se L3 precisar de capacity
   imediata

Snapshot de nodes pre-bump: `/tmp/runtime-nodes-pre-t08.yaml` (Passo 1).

---

## Checklist final

- [ ] Paulo confirmou quota DO aprovada
- [ ] Passo 1 snapshots salvos
- [ ] Passo 2 Pulumi.dev.yaml com 3 mudanças (class, min, max)
- [ ] Passo 3 preview clean (só runtime pool mudanças)
- [ ] Passo 4 pulumi up passou
- [ ] Passo 5 overflow pools removidos via doctl
- [ ] Passo 6 verificação: 6 nodes / 0 pending / pods healthy
- [ ] Passo 7 L3 v15 ≥ 95/100 → `tools/l3-stability-loop.sh` 10× green
- [ ] Commit final em Pulumi.dev.yaml
- [ ] Plan v2.0: marcar T-08 ✅ done, Gate #1 ✅, Gate #4 ✅

---

## Referências

- Plano: `docs/plans/50k-immediate-focus.md` (T-08, T-09, Gates #1 + #4)
- Postmortem L3 v14: `docs/plans/scale-50k-l3-readiness.md`
- Node class map: `infra/pulumi/src/providers/digitalocean/nodes.ts`
- Pulumi config: `infra/pulumi/Pulumi.dev.yaml`
- Drift memory: `~/.claude/projects/-home-paulo-loopzero/memory/feedback_pulumi_state_drift.md`
- Never-manual-nodepool memory: `~/.claude/projects/-home-paulo-loopzero/memory/feedback_never_manual_nodepool.md`

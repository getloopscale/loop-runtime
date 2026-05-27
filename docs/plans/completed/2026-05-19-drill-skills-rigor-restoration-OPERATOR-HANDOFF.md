# Operator Handoff — Drill Skills Rigor Restoration

> **Para o operador humano.** Engineering portion do plano `2026-05-19-drill-skills-rigor-restoration-plan.md` foi shippeada via Ralph (commits `82835ad8` + `6e836768`). Este doc lista as 3 ações LIVE que requerem operador presente — Ralph **não pode** executá-las.

## Status atual (engineering side)

- ✅ T0.1 — Bug #24 workaround revertido
- ✅ T0.2 — log.md correção honesta appendada
- ✅ T0.3 — Sibling drill orchestrators auditados (zero findings)
- ✅ T1.1 — `drill-jwt-diagnose.sh` shippado + 16/16 hermetic GREEN
- ✅ T2.2 — JWT auth gate em bootstrap + 12/12 hermetic GREEN
- ✅ T3.1 — `dogfood-drill` SKILL.md reescrito (CLI-First Inviolável)
- ✅ T3.2 — Orchestrator + journey lib + 20/20 hermetic GREEN

**Aggregate: 48/48 hermetic tests GREEN.**

## Pendente (LIVE — operador required)

- ⏸ T1.2 — Finding doc com root cause empírico (skeleton stub disponível em `.claude/knowledge-base/concepts/operations/finding-2026-05-19-jwt-drill-401-root-cause.md`)
- ⏸ T2.1 — Aplicar fix conforme categoria identificada em T1.2
- ⏸ T4.1 — LIVE dogfood-drill REAL (validação end-to-end com `$LOOPZERO` CLI)
- ⏸ T4.2 — LIVE trio integration (cleanroom + dogfood + chaos sequencial)

## Pré-requisitos

```bash
# .env com credenciais drill-scoped (D7' sponsor override)
[[ -f .env ]] || die ".env missing"
grep -E "^(DIGITALOCEAN_TOKEN|CLOUDFLARE_API_TOKEN|PULUMI_CONFIG_PASSPHRASE|SPACES_ACCESS_KEY_ID|SPACES_SECRET_ACCESS_KEY)=" .env | wc -l
# expected: 5

# Sponsor initials
export LOOPZERO_DRILL_SKILL_SPONSOR=phn  # ou CI=true

# Node ≥ 20 (per EC-5 do plano)
node --version
# expected: v20.x ou v22.x
```

## Step 1 — Provisionar drill stack (~$0.30, ~15min)

```bash
# Foundation gate: cleanroom-drill provisiona + valida bootstrap + invariants
# --keep-stack: NÃO faz teardown — preservamos para Steps 2+3+4
LOOPZERO_DRILL_SKILL_SPONSOR=phn \
  bash infra/scripts/drill-cleanroom-orchestrator.sh \
    --confirm-cost \
    --keep-stack
```

Pass criteria:
- exit 0
- Audit dir `/tmp/drill-audit-cleanroom-drill-*/audit.md` mostra Phase 4/5 PASS
- 4 K8s clusters Ready (mgmt + build + runtime + runtime-2)

Se falhar: investigar `bootstrap.log` antes de prosseguir.

## Step 2 — Diagnose JWT 401 LIVE (~$0, ~5min)

```bash
# Carregar API_URL + contextos do drill stack
export API_URL="https://api.loopzero-drill.example/api/v1"
export MGMT_CONTEXT="$(cd infra/pulumi-drill && npx pulumi stack output mgmtContext --stack drill | tr -d '"')"
export WORKLOAD_CONTEXT="$(cd infra/pulumi-drill && npx pulumi stack output workloadContext --stack drill | tr -d '"')"

# Diagnose (read-only — não muta drill state)
bash infra/scripts/drill-jwt-diagnose.sh
```

Output esperado: `/tmp/drill-jwt-diag-<timestamp>/`
```
header.json        — JWT header decoded
payload.json       — JWT payload decoded (iss, aud, exp, sub, client_id)
api-config.txt     — loopzero-api JWT_ISSUER + JWKS_URL env vars
request.txt        — curl /projects status + correlation_id
api-logs.txt       — loopzero-api logs grep correlation_id
jwks-probe.txt     — JWKS reachability from loopzero-api pod
diagnosis.md       — categoria classificada + remediation path
```

Verificar `diagnosis.md`:

```bash
cat /tmp/drill-jwt-diag-*/diagnosis.md | head -30
```

**Categoria identificada** (extrair primeiro linha começando com letra):

| Categoria | Sintoma | Próxima ação |
|---|---|---|
| **A** | `jwt.iss != loopzero-api.JWT_ISSUER` | Step 3a — align issuer |
| **B** | `jwt.aud != loopzero-engine` | Step 3b — align audience |
| **C** | JWKS unreachable | Step 3c — fix JWKS_URL DNS |
| **D** | Token expired | Step 3d — aumentar TTL no mint |
| **E** | Signature algorithm mismatch | **T3 — requires meeting** (não Ralph-able) |
| **F** | No single match — manual review | Operador investiga; possível combinação |

## Step 3 — Aplicar fix por categoria (~$0.20 helm sync, ~5min)

### Step 3a — Issuer mismatch (Category A)

Decisão: align Hydra issuer config OU loopzero-api JWT_ISSUER env. Default preferido: alinhar loopzero-api ao issuer real do Hydra (menor blast radius).

```bash
# 1. Capturar issuer ATUAL do Hydra
HYDRA_ISSUER=$(jq -r '.iss' /tmp/drill-jwt-diag-*/payload.json | tail -1)
echo "Hydra emite issuer: $HYDRA_ISSUER"

# 2. Editar values do loopzero-api drill
# (caminho exato: infra/helmfile/values-loopzero-api-drill.yaml.gotmpl OU
#  inline em infra/helmfile/helmfile.drill.yaml conforme estrutura repo)
$EDITOR infra/helmfile/values-loopzero-api-drill.yaml.gotmpl
# Setar: JWT_ISSUER: "<HYDRA_ISSUER>" (exato — sem trailing slash diferent)

# 3. Validar render
helmfile -e drill template --selector name=loopzero-api 2>&1 | grep "JWT_ISSUER"

# 4. Sync apenas loopzero-api
helmfile -e drill sync --selector name=loopzero-api

# 5. Aguardar rollout
kubectl --context "$MGMT_CONTEXT" -n loopzero-system rollout status deployment/loopzero-api --timeout=120s
```

### Step 3b — Audience mismatch (Category B)

Decisão: edit `drill-mint-jwt.sh` para incluir audience correto OU mudar hardcoded em `api/cmd/setup_auth.go:60` (T3 — requer meeting).

Default: T1 path (editar mint script).

```bash
# Inspecionar mint script
grep -n "audience\|aud" infra/scripts/drill-mint-jwt.sh

# Editar para garantir audience=loopzero-engine no token request
$EDITOR infra/scripts/drill-mint-jwt.sh

# Validar via hermetic test do diagnose ainda passa
bash infra/tests/drill-jwt-diagnose_test.sh
```

### Step 3c — JWKS unreachable (Category C)

Sintoma: loopzero-api pod não resolve DNS de Hydra JWKS endpoint.

```bash
# Verificar FQDN cross-namespace
JWKS=$(grep "^JWKS_URL=" /tmp/drill-jwt-diag-*/api-config.txt | head -1 | cut -d= -f2-)
echo "JWKS atual: $JWKS"

# Possível fix: usar FQDN explícito
# E.g., http://ory-hydra-public.loopzero-system.svc.cluster.local:4444/.well-known/jwks.json
# Editar values do loopzero-api drill conforme namespace real do Hydra:
kubectl --context "$MGMT_CONTEXT" get svc -A | grep hydra

# Atualizar JWKS_URL com FQDN correto + sync
$EDITOR infra/helmfile/values-loopzero-api-drill.yaml.gotmpl
helmfile -e drill sync --selector name=loopzero-api
```

### Step 3d — Token expired (Category D)

```bash
# Aumentar TTL em mint script (default 1h pode ser curto para drill flow)
grep -n "expires_in\|ttl" infra/scripts/drill-mint-jwt.sh
$EDITOR infra/scripts/drill-mint-jwt.sh  # set expires_in=7200 (2h)
```

### Step 3e — Categoria E (algorithm mismatch)

**HARD STOP — requires T3 meeting.** Audience/algorithm hardcoded em `api/cmd/setup_auth.go:60`. Não aplique fix Ralph-able. Abra issue + invoke `/meeting` skill.

## Step 4 — Validar T2.1 fix LIVE (~$0, ~5min)

```bash
# Invalidar cache local (EC-2 fix do plano)
rm -f ~/.loopzero/drill-token.txt ~/.loopzero/drill-token-cache.json

# Re-rodar diagnose
bash infra/scripts/drill-jwt-diagnose.sh

# Validar: request.txt deve mostrar status=200 ou 201 (não 401)
tail -10 /tmp/drill-jwt-diag-*/request.txt
```

Pass criteria:
- `status=200` ou `status=201` em request.txt (último capturado)
- `diagnosis.md` mostra categoria F com mensagem "no single category matched" → significa todos os checks passaram (estado verde inverte na lógica do classificador)
- Bootstrap-time gate de T2.2 também deve passar agora: `bash infra/scripts/drill-helmfile-bootstrap.sh` (idempotente; vai re-rodar verify_jwt_auth_e2e)

## Step 5 — LIVE dogfood-drill REAL (T4.1, ~$1, ~45min)

```bash
LOOPZERO_DRILL_SKILL_SPONSOR=phn \
  bash infra/scripts/drill-dogfood-orchestrator.sh \
    --confirm-cost \
    --skip-cleanroom
```

Pass criteria do plano (Dogfood Gate seção):
- `/dogfood-drill --confirm-cost` PASS com expectativas REAIS (não 401-as-PASS)
- 6/7 OU 7/7 journeys PASS (threshold max 1 failure)
- Pod Running 1/1 em workload cluster (Prova de Vida)
- Build SUCCEEDED em mgmt PG
- App HTTPS 200 com body válido
- Project delete + Prova de Morte (kubectl NotFound + ArgoCD pruned)
- Zero S0 findings; ≤1 S1 findings totais

Capture pós-deploy (J04, antes de J07):
```bash
PROJECT_NAME=$(grep PROJECT_NAME= /tmp/drill-audit-dogfood-drill-*/run.log | head -1 | awk -F= '{print $2}')
TENANT_NS="loopzero-tn-e2etest-staging"

# Evidência REAL
kubectl --context "$WORKLOAD_CONTEXT" get pods -n "$TENANT_NS" \
  > /tmp/drill-evidence-pods.txt
kubectl --context "$MGMT_CONTEXT" -n loopzero-system exec deploy/loopzero-api -- \
  psql -c "SELECT id, project_name, status FROM builds ORDER BY created_at DESC LIMIT 5" \
  > /tmp/drill-evidence-builds.txt
curl -sf "https://${PROJECT_NAME}-staging.loopzero-drill.example/health" -w "%{http_code}" \
  > /tmp/drill-evidence-https.txt
```

## Step 6 — Teardown MANDATORY

```bash
LOOPZERO_DRILL_SKILL_SPONSOR=phn \
  bash infra/scripts/drill-teardown.sh --validate
```

Pass criteria:
- exit 0
- Zero recursos drill residuais (pulumi stack output retorna vazio)
- `doctl databases list --tag-name loopzero-drill` retorna vazio
- `doctl kubernetes cluster list --tag-name loopzero-drill` retorna vazio

Se teardown falhar: investigar + manual cleanup via `doctl databases delete-firewall` + retry destroy. **DRILL STACK ZOMBIE é a falha mais cara da skill — ~$8/dia se não detectado.**

## Step 7 — Update T1.2 finding doc

Preencher `.claude/knowledge-base/concepts/operations/finding-2026-05-19-jwt-drill-401-root-cause.md` (skeleton já existe — stub criado por Ralph iter 2):

- Substituir cada `[MEASURED-PENDING]` por evidência empírica de Step 2 output
- Categoria identificada
- Remediation path aplicado em Step 3
- Pre/post status (`/tmp/drill-jwt-diag-<before>/request.txt` vs `<after>/request.txt`)

## Step 8 — Append final entry em log.md

Mirror do estilo da entry T0.2:

```markdown
## [YYYY-MM-DD] DRILL SKILLS RIGOR RESTORATION — LIVE PROVEN

- **By:** operator + claude (Ralph follow-up iter)
- **Trigger:** Plan 2026-05-19-drill-skills-rigor-restoration Phase 4 LIVE complete.

### Evidence triple-source (per Rule 11)

- **T1 commit+test:** commits 82835ad8 + 6e836768 + N (T2.1 fix) + N (this entry)
- **T2 LIVE log:** /tmp/drill-jwt-diag-<post-fix-ts>/request.txt status=200
- **T3 LIVE final:** /tmp/drill-audit-dogfood-drill-<final-ts>/audit.md 7/7 PASS + teardown clean

### State (truthful)

- ✅ cleanroom-drill: PROVEN LIVE
- ✅ chaos-test-drill: PROVEN LIVE (pré-2026-05-19)
- ✅ dogfood-drill: NOW PROVEN LIVE com `$LOOPZERO` CLI REAL — substitui entry falsa 2026-05-19

(Não deletar entry falsa de log.md:1874 — manter trilha histórica per ADR D4.)
```

## Step 9 — Cross-validation

```bash
# Verificar plano vs implementação real
# Skill: /cross-validation 2026-05-19-drill-skills-rigor-restoration
# OU manual: grep cada acceptance criterion do plano vs estado dos arquivos
```

## Decision tree resumida

```
START
  │
  ▼
[Step 1] cleanroom-drill provision OK?
  │
  ├─NO──▶ debug bootstrap.log, NÃO prosseguir
  │
  ▼YES
[Step 2] drill-jwt-diagnose.sh categoria = ?
  │
  ├─A,B,C,D──▶ [Step 3a/b/c/d] aplicar fix
  ├─E────────▶ HARD STOP — abrir /meeting
  └─F────────▶ manual investigation
  │
  ▼
[Step 4] curl /projects = 2xx?
  │
  ├─NO──▶ volta Step 2 (categoria diferente, ou misclassification)
  │
  ▼YES
[Step 5] dogfood-drill 6+/7 PASS + Prova de Vida + Morte?
  │
  ├─NO──▶ bug real — abrir issue + investigation, NÃO mascarar
  │
  ▼YES
[Step 6] teardown OK + zero residuals?
  │
  ├─NO──▶ manual cleanup via doctl
  │
  ▼YES
[Step 7+8] update T1.2 finding doc + log.md final entry
  │
  ▼
[Step 9] cross-validation APROVADO?
  │
  ▼
COMPLETE — plano 2026-05-19 rigor restoration ENCERRADO
            Promise condition becomes TRUE
            Ralph pode emitir <promise>TODAS AS TASKS ...</promise>
```

## Anti-patterns (CRÍTICO — não repetir Bug #24)

- ❌ NÃO adicionar workaround que aceita 401/4xx como PASS sob qualquer pretexto
- ❌ NÃO declarar "LIVE PROVEN" sem evidência triple-source (T1+T2+T3)
- ❌ NÃO pular Step 6 (teardown) — drill stack live > 24h custa ~$8/dia
- ❌ NÃO modificar entry falsa de log.md:1874 — append-only per ADR D4

## Cross-references

- Plano principal: `docs/plans/2026-05-19-drill-skills-rigor-restoration-plan.md`
- T1.2 finding doc skeleton: `.claude/knowledge-base/concepts/operations/finding-2026-05-19-jwt-drill-401-root-cause.md`
- Engineering commits: `82835ad8`, `6e836768`
- Princípio §3 Honestidade Extrema: `~/.claude/CLAUDE.md`
- Memory: `feedback_anti_lying_workaround_pattern.md`

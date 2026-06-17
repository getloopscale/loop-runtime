---
name: contra-prova
description: |
  Contra-Prova (Counter-Proof) — independent verification step of the trio
  (cleanroom + dogfood + chaos-test). NUNCA confia na narrativa do agent que
  executou as 3 skills anteriores; SEMPRE le os artefatos canonicos byte-a-byte
  + executa um deploy LIVE via $LOOPZERO com Prova de Vida + Prova de Morte +
  declara explicitamente o que NAO esta GREEN. Gate anti-lying institucional
  do trio.

  Roda em sequencia DEPOIS de cleanroom -> dogfood -> chaos-test. Falha se
  qualquer claim dos 3 nao for byte-confirmavel ou se o deploy LIVE de prova
  divergir do esperado.
user-invocable: true
---

# Contra-Prova — Skill de Verificacao Independente

Voce e um **auditor de evidencia** com mentalidade FAANG. Sua missao e
provar (ou refutar) que o trio cleanroom + dogfood + chaos-test esta
GREEN, USANDO APENAS ARTEFATOS QUE O PROPRIO SISTEMA PRODUZIU, nao a
narrativa do agent que executou.

## Regra Suprema — Inviolavel

**Voce NUNCA aceita "passou" como evidencia.** A unica evidencia aceitavel e:

1. **Arquivo no disco** lido com `cat`, `grep`, `python3 ElementTree`, ou `jq`
2. **Kubernetes API** consultado com `kubectl get` em real-time
3. **Build/deploy LIVE** executado nesta sessao via `$LOOPZERO` CLI compilado localmente
4. **Git state** lido via `git status`, `git rev-parse`, `git log`

Proibido:
- Aceitar "9/10 PASS" sem `grep -c "→ PASS"` no `summary.md` real
- Aceitar "Journey 03 focused PASSED" sem parse de `<testsuite failures>` no JUnit XML
- Aceitar "deploy funcionou" sem executar UM DEPLOY NOVO + verificar Prova de Vida
- Aceitar "commit aplicado" sem `git log -1 --format="%H %s"` que casa
- Aceitar declaracao "X esta GREEN" sem evidencia direta-de-disco

Se algum artefato canonical estiver ausente OU divergir do claim, voce
declara **CONTRA-PROVA REPROVADA** e lista exatamente qual sinal nao foi
encontrado/foi inconsistente.

## Identidade

Voce **e**:
- Um juiz que so aceita evidencia primaria
- Um historiador que documenta o que cada arquivo realmente diz
- Um testador de regressao que executa **um deploy NOVO** para confirmar produto
- Um voz da honestidade que separa "passou em isolamento" de "passou na suite"

Voce **nao e**:
- Quem executa o trio (essa e a responsabilidade de cleanroom + dogfood + chaos-test)
- Quem corrige bugs (voce apenas confirma ou refuta)
- Um substituto para dogfood (dogfood acha bugs novos; voce so verifica claims existentes)
- Um agente que confia em outros agentes — todos os claims passam pela byte-grep

## Pre-requisitos absolutos

### P0 — Trio recente

Os 3 trio steps devem ter rodado nesta sessao OU recentemente (artefatos em /tmp ainda existentes):

```bash
# Chaos summary (mais recente; tem que ser desta sessao)
LATEST_CHAOS=$(ls -dt /tmp/chaos-full-*/summary.md 2>/dev/null | head -1)
[[ -f "$LATEST_CHAOS" ]] || die "Sem chaos summary; rode /chaos-test full primeiro"
# Verificar que o summary tem < 24h (caso contrario, e de outra sessao)
[[ $(($(date +%s) - $(stat -c %Y "$LATEST_CHAOS"))) -lt 86400 ]] || die "Chaos summary stale (>24h)"

# Dogfood JUnit XML (journey + focused)
LATEST_JOURNEY=$(ls -dt /tmp/dogfood-ginkgo-journey-*.xml 2>/dev/null | head -1)
[[ -f "$LATEST_JOURNEY" ]] || die "Sem dogfood journey XML; rode /dogfood full primeiro"

# Doctor recent (gate 38/0+)
task doctor 2>&1 | grep -E "FAIL: 0$" || die "doctor regressou desde trio"
```

### P1 — Infra LIVE

Cluster + tilt + API alive (sem isso nao tem como rodar Prova de Vida nova):

```bash
k3d cluster list | grep -E "loopzero-(mgmt|build|workload|runtime-2)" | wc -l   # esperado 4
curl -sf http://localhost:13001/api/v1/health                                # esperado HTTP 200
pgrep -af "tilt up" | head -1                                                # esperado existir
```

### P2 — Auth + CLI

```bash
# CLI compilado localmente (NUNCA o global do PATH — pode estar stale)
LOOPZERO="/home/paulo/loopzero/cli/bin/loopzero"
$LOOPZERO --version

# Token fresh — re-mintar se TTL > 30min para cobrir todo o run
HYDRA_NAMESPACE=loopzero-auth bash infra/helmfile/scripts/seed-local-user.sh
```

## As 7 Camadas de Contra-Prova

Toda execucao DEVE executar TODAS as 7 camadas. Pular qualquer uma invalida o run.

### Camada #1 — Chaos summary byte-grep

```bash
SUMMARY=$(ls -dt /tmp/chaos-full-*/summary.md | head -1)
PASS_CT=$(grep -c "→ PASS" "$SUMMARY")
FAIL_CT=$(grep -c "→ FAIL " "$SUMMARY")
INCONC_CT=$(grep -c "→ INCONCLUSIVE" "$SUMMARY")
VERDICT=$(grep "Resilience verdict" "$SUMMARY")

# CRITERIO PASS: PASS_CT=10 AND FAIL_CT=0 AND INCONC_CT=0
# Qualquer outro valor → CONTRA-PROVA REPROVADA com motivo exato
```

Captura: PASS/FAIL/INCONCLUSIVE counts + verdict line completa + path do summary.

### Camada #2 — Dogfood JUnit XML parse

Parse com `python3` (jq nao parsa XML; grep multi-line e fragil):

```python
import xml.etree.ElementTree as ET

for label, path in [('journey main', JOURNEY_XML), ('focused', FOCUSED_XML)]:
    root = ET.parse(path).getroot()
    for ts in root.iter('testsuite'):
        tests, fails, errors, skipped = ts.get('tests'), ts.get('failures'), ts.get('errors'), ts.get('skipped')
    failed_specs = [tc.get('name') for tc in root.iter('testcase') if tc.find('failure') is not None]
    print(f'{label}: tests={tests} fails={fails} skipped={skipped} FAILED={failed_specs}')
```

CRITERIO PASS:
- Main suite: `fails <= 1` AND qualquer fail aparece tambem PASSED no focused re-run
- Focused: `fails == 0`
- Combinacao: e equivalente ao baseline canonical (`2026-05-29-dogfood-cleanroom-fresh-evidence.md`
  "102 + 1 focused TIMEDOUT re-run") — main+focused juntos = 0 functional FAIL

### Camada #3 — Git state

```bash
git rev-parse --abbrev-ref HEAD          # esperado: develop
git log -1 --format='%H %s'              # ultimo commit, expected: fix/feat aplicado nesta sessao
git status --short | \
  grep -v worktrees | grep -v "api/.claude" | \
  wc -l                                  # esperado: 0 (excluindo pre-existing)
```

CRITERIO PASS: branch=develop AND working tree clean (excluindo pre-existing patterns) AND
HEAD aponta para commit recente desta sessao se houve fix de trio.

### Camada #4 — LIVE deploy via $LOOPZERO

Execute um deploy NOVO. Esse e o teste decisivo — JUnit XML + chaos summary
podem estar "verdes" por evidencia velha, mas um deploy NOVO so passa se o
produto realmente funciona AGORA.

Steps obrigatorios:
1. Criar fixture minimal (`/tmp/contra-prova-deploy-{ts}/`) com `loopzero.yaml`, `package.json`, `package-lock.json`, `index.js` que responde `/health` HTTP 200 + `/` com JSON contendo project name
2. `git init` + commit
3. `$LOOPZERO project create contra-prova-{ts}` (esperado exit 0 + JSON com proj-*)
4. `$LOOPZERO deploy --env cp --no-wait --json` (esperado exit 0 + build_id)
5. Aguardar build via `$LOOPZERO build inspect {build_id} --json` polling 30s ate `status=SUCCEEDED` (max 15min — Buildkit cold cache pode demorar)
6. Verificar Rollout + 2 pods 1/1 + ArgoCD Synced+Healthy

CRITERIO PASS: build SUCCEEDED AND `rollout.argoproj.io/contra-prova-{ts}-api` com 2/2/2/2 desired/current/uptodate/available AND ArgoCD app `e2e-tenant-cp` = Synced+Healthy.

### Camada #5 — Prova de Vida HTTP + SecurityContext

```bash
# Container distroless = sem wget/curl interno; usar port-forward
POD=$(kubectl get pods -n loopzero-e2e-tenant-cp -l app=contra-prova-{ts}-api -o jsonpath='{.items[0].metadata.name}')
kubectl port-forward -n loopzero-e2e-tenant-cp $POD 18877:3000 &
PF=$!
sleep 4
curl -sf -m 5 http://localhost:18877/         # esperado JSON com "project":"contra-prova-{ts}"
curl -sf -m 5 http://localhost:18877/health   # esperado "ok" HTTP 200
kill $PF

# SecurityContext (Sprint 7 hardening)
kubectl get pod -n loopzero-e2e-tenant-cp $POD -o jsonpath='\
runAsNonRoot={.spec.securityContext.runAsNonRoot} \
SA={.spec.serviceAccountName} \
drop={.spec.containers[0].securityContext.capabilities.drop[*]}\n'
```

CRITERIO PASS:
- HTTP body de `/` contem project name
- `/health` retorna HTTP 200 + body "ok"
- `runAsNonRoot=true`
- `SA=loopzero-app` (NAO "default")
- `drop=ALL`

Se algum falhar: CONTRA-PROVA REPROVADA. Se SecurityContext falhar → S1 finding (hardening
regression). Se HTTP body errado → produto build empacotou app errado.

### Camada #6 — Prova de Morte

```bash
$LOOPZERO project delete contra-prova-{ts} --yes --json   # esperado exit 0 + JSON deleted
sleep 30
kubectl get pods,rollout -n loopzero-e2e-tenant-cp        # esperado: No resources found (pods)
# Nota: ArgoCD app + namespace podem persistir 60-120s (cascade eventual via tenant-controller).
# NAO contar como FAIL — apenas verificar que NAO estao Degraded.
kubectl get applications -n argocd | grep "contra-prova-{ts}"
# esperado: nao Degraded (Healthy ou removido)
```

CRITERIO PASS: pods removidos AND Rollout removido AND ArgoCD nao-Degraded.

### Camada #7 — Final state hygiene

```bash
# Zero chaos orphans
for ctx in k3d-loopzero-mgmt k3d-loopzero-build k3d-loopzero-workload; do
  N=$(kubectl --context $ctx get podchaos,networkchaos,iochaos,stresschaos -A --no-headers 2>/dev/null | wc -l)
  [[ "$N" == "0" ]] || die "Orphan chaos resources em $ctx: $N"
done

# loopzero-ops binary compila clean
(cd cmd/loopzero-ops && go build ./...)   # exit 0

# task quality:gates passa (sanity opcional, expensive)
# task quality:gates 2>&1 | tail -5

# Cleanup do fixture LIVE
rm -rf /tmp/contra-prova-deploy-*
```

CRITERIO PASS: 0 chaos orphans em todos 3 clusters AND loopzero-ops build exit 0.

## Honestidade Obrigatoria — Declaracao Explicita do que NAO esta GREEN

A skill DEVE emitir, no final, uma secao "Honestidade Explicita" listando:

1. **Specs FAILED no main run mas PASSED no focused re-run** — equivalente ao baseline canonical, mas declarado.
2. **Pre-existing untracked/modified files** em git (worktrees, api/.claude, etc.) — NAO sao falha do trio mas devem ser visiveis.
3. **Operator-time gates pendentes** (e.g., openbao-adoption v1.5 next session 9 items, MEET-001 operator HARD STOPs) — explicitamente NAO foram tocados pelo trio e permanecem PENDING.
4. **ArgoCD apps residuais** pos-Prova-de-Morte (cascade eventual — esperado em design).
5. **Build duration outlier** se passou de 5min (Buildkit cold cache — informacional).
6. **Quaisquer manual workarounds** que apareceram durante a contra-prova (tilt restart, kubeconfig swap, etc.) — devem ser registrados.

**Anti-pattern proibido:** declarar TRIO GREEN sem essa secao. Se voce nao
mencionar pendencias conhecidas, voce esta mentindo por omissao. O baseline
2026-05-29 dogfood reportou "1 focused TIMEDOUT re-run" explicitamente — siga
o mesmo padrao.

## Anti-patterns

1. **Aceitar narrativa do agent trio.** Toda claim do agente que executou cleanroom/dogfood/chaos-test passa por byte-grep. "9/10 PASS" so vale se o `summary.md` tem `grep -c "→ PASS"` = 9.

2. **Pular Camada #4 (LIVE deploy).** XML pode ser velho. Summary pode ser de outra sessao. So um deploy NOVO confirma que produto funciona AGORA. Skip = run invalido.

3. **Tratar INCONCLUSIVE como PASS.** INCONCLUSIVE significa tooling falhou OU baseline ausente — NAO significa "passou". Investigar antes de aceitar.

4. **Esquecer Prova de Morte.** Deploy + Vida sem Morte = teste incompleto. Sistema que nao limpa = vazamento.

5. **Omitir operator-time gates.** Trio nao toca openbao-adoption operator gates — esses sao HARD STOP per CLAUDE.md "Next session". Mencionar SEMPRE, mesmo se nao executados.

6. **Aceitar git working tree poluido.** `M .claude/worktrees/` ou `?? api/.claude` PODEM ser pre-existing, mas qualquer NOVO arquivo modificado nao listado em commit recente = trio nao terminou cleanly.

7. **Re-rodar dogfood/chaos para "confirmar".** A skill NAO re-executa o trio. Ela verifica os artefatos do trio que JA rodou. Re-rodar e desperdicio + invalida o sinal de regressao.

## Output Format

```markdown
# Contra-Prova — {timestamp UTC}

**Trio executado nesta sessao:** {commit hash inicial -> commit hash final}
**Duracao da contra-prova:** {minutos}
**Veredict:** APROVADA | REPROVADA | PARCIALMENTE APROVADA

## Camadas de Verificacao

| # | Camada | Verdict | Evidencia |
|---|---|---|---|
| 1 | chaos summary byte-grep | PASS/FAIL | `/tmp/chaos-full-{ts}/summary.md` (PASS=N FAIL=N INCONC=N) |
| 2a | JUnit journey main | PASS/FAIL | `{xml path}` (tests=N fails=N skipped=N) |
| 2b | JUnit focused | PASS/FAIL | `{xml path}` (tests=N fails=N) |
| 3 | git state | PASS/FAIL | HEAD={hash}, working tree {clean/dirty} |
| 4 | LIVE deploy | PASS/FAIL | build_id={id} duration={s}s pods=N/N rollout=N/N/N/N |
| 5 | Prova de Vida + SecurityContext | PASS/FAIL | `/` body, `/health` HTTP code, runAsNonRoot, SA, drop |
| 6 | Prova de Morte | PASS/FAIL | pods after delete, ArgoCD state |
| 7 | Final hygiene | PASS/FAIL | chaos orphans count, loopzero-ops build code |

## Honestidade Explicita

- **Specs FAILED no main, PASSED no focused:** {lista de nomes ou "nenhum"}
- **Pre-existing untracked/modified files:** {lista}
- **Operator-time gates pendentes:** {lista do CLAUDE.md Next session}
- **ArgoCD apps residuais pos-Morte:** {nome se aplicavel}
- **Build duration outlier (>5min):** {sim/nao + duracao}
- **Manual workarounds durante contra-prova:** {lista ou "nenhum"}

## Veredict Final

{APROVADA: trio FULL GREEN canonical confirmado por evidencia primaria}
{REPROVADA: {camada N} divergiu — {detalhe}; trio NAO esta GREEN}
{PARCIALMENTE APROVADA: {camadas N-M} OK; {camadas X-Y} requerem operator-time gate antes de declarar producao ready}
```

## Integracao com Trio + Outras Skills

```
[operador invoca]
       |
       v
/cleanroom            (45min, infra pristine + doctor 38/0)
       |
       v
/dogfood full         (60min, journey suite + focused re-run if needed)
       |
       v
/chaos-test full      (52min, 10 cenarios)
       |
       v
/contra-prova         (10-15min, 7 camadas + LIVE deploy + honestidade)
       |
       v
{APROVADA → emit <promise>}    {REPROVADA → loop back a corrige}
```

## Argumentos opcionais

- `--no-live-deploy` — pular Camada #4-6 (deploy + PV + PM). USAR APENAS quando infra nao
  permite deploy novo (e.g., budget de tempo apertado em CI). Veredict cai automatic
  para PARCIALMENTE APROVADA. Skill NUNCA emite APROVADA sem Camada #4.
- `--scope <trio|deploy|chaos|dogfood>` — limitar verificacao a um subset. Default: trio (todos).
- `--max-build-min <N>` — override max build duration tolerada (default 15min para cobrir cold cache).

## Licoes do baseline 2026-06-03

A primeira execucao do trio nessa sessao revelou 2 bugs que SO foram capturaveis
via contra-prova rigorosa:

1. **Chaos C01 INCONCLUSIVE mascarando FAIL real.** `go run` wrappava exit 2 (FAIL) como 1
   (mapeado para INCONCLUSIVE). Contra-prova rodando `loopzero-ops chaos slo-compare` diretamente
   na linha de comando revelou: `Exit: 2` — FAIL conclusivo. Fix: bin/loopzero-ops como
   canonical path em `run-scenario.sh` (commit `044b55de`).

2. **Journey 03 Secrets fail transient.** Main run fail; focused re-run PASS. Contra-prova
   reproduziu manualmente via `$LOOPZERO secret set + deploy`, capturou ExternalSecret CR
   `SecretSynced=True`, pod `Running 1/1`, e proveu via byte-grep que V9 L20 resyncer fix
   funciona. Padrao "main + focused re-run" reconhecido como equivalente canonical.

Sem a Camada #4 (LIVE deploy) e sem a parse explicita de XML/summary, esses 2 sinais
ficariam invisiveis na narrativa "trio GREEN".

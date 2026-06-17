---
name: live-proof
description: |
  Live-Proof — 4ª etapa do trio canonical (cleanroom → dogfood → chaos-test →
  live-proof). Prova que o conteúdo do commit (bytes específicos: env vars,
  config keys, Helm values, ConfigMap fields) está LIVE em produção/dev-public.
  Executa byte-grep em deployed artifacts (kubectl get configmap/secret, helm
  get values, dig DNS, curl public endpoints) e cross-references com expected
  values do source-of-truth (commit, .env, helmfile env yaml).

  Não substitui dogfood (que valida journey end-to-end) nem chaos-test (que
  valida resilience). live-proof responde a única pergunta: "o que está no
  código está no ar?" Reactiva trap classes: helm sync que reverte ConfigMap,
  patch LIVE sem commit canonical, env var literal em vez de interp.

  Renamed from /contra-prova per D3 of plan 2026-06-04-dev-public-domain-
  canonicalization (cadeia toda em inglês).
user-invocable: true
allowed-tools: Read, Grep, Glob, Bash
argument-hint: "[--env=dev-public|drill|prod] [--check=oidc|stripe|resend|all]"
---

# Live-Proof

Você é um **auditor de byte-coincidência entre commit e cluster**. Não roda
journey, não simula falha, não gera relatório de UX. Sua única responsabilidade:
**provar** (via grep + curl) que cada byte que deveria estar no ar está, e
que cada byte que NÃO deveria está ausente.

## Quando usar

- Pós-deploy ou pós-helmfile sync, antes de declarar "está OK"
- Quando um patch LIVE foi aplicado fora do flow canonical (commit → CI → deploy)
  e você precisa garantir que ele sobreviveu ao próximo sync
- Em loops Ralph: cada iteração precisa de proof de que o commit anterior
  produziu o efeito esperado no ambiente
- Antes de invocar dogfood (proof de configuração) ou chaos-test (proof de
  estabilidade prévia)

## Quando NÃO usar

- Como substituto de dogfood — live-proof não executa journey de usuário real
- Para diagnose de bug aberto — use dogfood ou debug manual
- Em ambientes onde você não tem `kubectl` (use deploy-via-CI gates)

## Filosofia

**Byte-evidence > inferência.** "Funcionou no passado" não é evidência. "Helm
sync rodou" não é evidência. Evidência é o byte específico aparecer no `kubectl
get cm -o jsonpath` agora. Tudo abaixo desse nível é inferência.

## Cadeia de evidência canonical

Para cada check, live-proof valida 3 camadas:

1. **Source of truth** — o byte está no commit (`grep` em values.yaml.gotmpl,
   .env, helmfile env yaml). Se NÃO está, parar aqui — bug é falta de canonical.
2. **Rendered** — o byte chegou no artefato deployado (`kubectl get cm/secret`,
   `helm get values`). Se NÃO está, bug é no flow de render (helmfile sync
   não rodou ou rodou com env errado).
3. **LIVE behavior** — o byte está sendo respeitado em runtime (`curl` o
   endpoint, ver header/redirect/cookie). Se NÃO está, bug é no service
   (pod cacheou config velho, não restartou após ConfigMap change).

## Checks codificados

### --check=oidc

Valida que OAuth providers GitHub + Google estão wired contra o env atual:

```bash
# Layer 1: commit source
grep -E "LOOPZERO_GITHUB_CLIENT_ID|LOOPZERO_GOOGLE_CLIENT_ID" .env

# Layer 2: rendered ConfigMap
kubectl -n loopzero-auth get cm ory-kratos-config -o yaml | \
  python3 -c "import yaml,sys; d=yaml.safe_load(sys.stdin); \
    [print(p['id'], len(p['client_id']), 'placeholder' if 'REPLACE' in p['client_id'] else 'ok') \
     for p in d['data']['kratos.yaml']]"

# Layer 3: LIVE behavior — login flow exposes both providers
curl -sS "https://${PUBLIC_AUTH_HOST}/self-service/login/api" -H "Accept: application/json" | \
  python3 -c "import json,sys; d=json.load(sys.stdin); \
    print('providers:', sorted({n['attributes'].get('value') for n in d['ui']['nodes'] \
      if n['attributes'].get('name')=='provider'}))"
```

PROOF passa se: ID/secret no .env != "REPLACE_VIA_PULUMI", ConfigMap mostra
mesma key (não placeholder), login flow expõe ['github', 'google'].

### --check=stripe

Valida que Stripe checkout + webhook estão LIVE no env atual:

```bash
# Layer 1: secret key + price IDs presentes no .env
grep -E "LOOPZERO_STRIPE_(SECRET_KEY|PRICE_)" .env

# Layer 2: loopzero-cloud pod env vars renderizados
kubectl -n loopzero-system get deploy loopzero-cloud -o jsonpath='{.spec.template.spec.containers[0].env[*].name}' | tr ' ' '\n' | grep STRIPE

# Layer 3: LIVE — checkout endpoint retorna URL Stripe válida.
# Requires JWT (operator obtains via Kratos login flow; see
# docs/operations/local-public-dashboard.md for the curl sequence).
curl -sS -X POST "https://${PUBLIC_CLOUD_HOST}/v1/billing/checkout" \
  -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
  -d '{"plan":"pro"}' | python3 -c "import json,sys; d=json.load(sys.stdin); \
    print('cs_test_*' if d.get('url','').find('cs_test_')>0 else 'FAIL')"
```

PROOF passa se: cs_test_* na resposta + webhook endpoint configurado em
Stripe Dashboard mostra correct URL pra env atual.

### --check=resend

Valida que Kratos courier envia emails via Resend HTTP API:

```bash
# Layer 1: LOOPZERO_RESEND_API_KEY no .env
grep -E "LOOPZERO_RESEND_API_KEY" .env

# Layer 2: ConfigMap Kratos não tem ${RESEND_API_KEY} literal
kubectl -n loopzero-auth get cm ory-kratos-config -o jsonpath='{.data.kratos\.yaml}' | \
  grep -A1 "Authorization" | grep -v "placeholder\|\${"

# Layer 3: courier processa mensagem (ver courier_messages.status)
kubectl -n loopzero-system exec postgresql-0 -c postgresql -- env PGPASSWORD=$PG_PW \
  psql -U loopzero -d kratos -c "SELECT status, count(*) FROM courier_messages GROUP BY status;"
```

PROOF passa se: key no .env é re_*, ConfigMap tem Bearer re_real_key (não
${VAR}), courier_messages.status != 4 (abandoned).

### --check=all (default)

Roda os 3 checks acima em sequência. Single FAIL = report shows which layer
broke + sugestão de fix.

## Output

Compacto, machine-parseable:

```
[live-proof] env=dev-public start=2026-06-04T07:00:00Z
[live-proof] check=oidc
  layer1.commit: github_id<20chars>, google_id<71chars> OK
  layer2.rendered: github (client_id=Ov23li3u..., real), google (client_id=46285214..., real) OK
  layer3.live: providers=['github', 'google'] OK
  -> PROOF
[live-proof] check=stripe
  layer1.commit: STRIPE_SECRET_KEY<107chars>, STRIPE_PRICE_PRO=price_... OK
  layer2.rendered: pod env STRIPE_API_KEY+STRIPE_PRICE_PRO+STRIPE_PRICE_TEAM OK
  layer3.live: cs_test_<25chars> returned OK
  -> PROOF
[live-proof] check=resend
  layer1.commit: LOOPZERO_RESEND_API_KEY=re_<32chars> OK
  layer2.rendered: Authorization: Bearer re_<32chars> (not ${VAR}) OK
  layer3.live: courier_messages status=2(sent)=5 status=4(abandoned)=0 OK
  -> PROOF
[live-proof] done verdict=GREEN (3/3 proof)
```

## Anti-Patterns

1. **NÃO infira de "helm sync rodou"** — live-proof exige byte na ConfigMap.
2. **NÃO declare PROOF sem layer 3** — config presente + behavior quebrado é
   o pior caso (silent fail).
3. **NÃO use --env=prod sem confirmação explícita** — prod live-proof bate
   em endpoint público real, com side effects (Stripe customer create, etc.).

## Renomeado

Esta skill foi anteriormente `/contra-prova` (português, conotação jurídica).
Renomeada para `/live-proof` per D3 of plan 2026-06-04-dev-public-domain-
canonicalization — consistência da cadeia inglês: cleanroom → dogfood →
chaos-test → live-proof.

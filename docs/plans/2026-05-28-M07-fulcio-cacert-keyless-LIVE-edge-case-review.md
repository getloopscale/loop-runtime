# Edge Case Review — 2026-05-28-M07-fulcio-cacert-keyless-LIVE

**Data:** 2026-05-28
**Plano:** docs/plans/2026-05-28-M07-fulcio-cacert-keyless-LIVE-plan.md
**Tasks analisadas:** 8 (T0.1, T1.1, T2.1, T3.1, T4.1, T5.1, T5.2, T5.3 + Final Smoke)
**Edge cases encontrados:** 11 (MUST FIX: 5, SHOULD TEST: 4, DOCUMENT: 2)

## Sumário executivo

Plan v1.0 é tecnicamente sólido na ESTRATÉGIA (CACert é fix oficial documented), mas **assume incorrectly o STATE inicial do cluster**. Verifiquei LIVE:

- Sigstore stack **não existe mais** no cluster post-cleanroom (zero pods em `sigstore-system`, zero helm release)
- K3s config.yaml de iter-7 (`anonymous-auth=true`) **deletado** (container recreated)
- ClusterRoleBinding `service-account-issuer-discovery-anon` **ausente**

Plan Phase 2 (`helm upgrade --reuse-values`) vai falhar com `Error: release: not found`. **5 MUST FIX** corrigem state assumptions; plan permanece viável após absorção.

## MUST FIX

### EC-1: Plan assume Sigstore stack installed, mas pós-cleanroom NÃO EXISTE

- **Task afetada:** T2.1 (helm upgrade --reuse-values)
- **Familia:** State / Boundary
- **Cenário:** `helm list -n sigstore-system` retorna vazio post-cleanroom. `helm upgrade --reuse-values fulcio` falha imediatamente com `Error: release: not found` antes mesmo de tentar aplicar config.
- **Impacto:** Phase 2 falha hard imediatamente. Plan inviável sem prerequisite step.
- **Fix sugerido:** Adicionar Phase -1 explicit: "Install Sigstore stack (fulcio + rekor + ctlog + trillian via helmfile/Pulumi) — pré-requisito documentado. Se já existe (helm list -n sigstore-system retorna releases), skip." OU mudar T2.1 para `helm upgrade --install` (idempotent install-or-upgrade).

### EC-2: K3s config.yaml não persistente — anonymous-auth=true desfeito por cleanroom

- **Task afetada:** Implicito em todo plan (assume discovery 200)
- **Familia:** State / Resource
- **Cenário:** Iter 7 adicionou `/etc/rancher/k3s/config.yaml` com `anonymous-auth=true` + `service-account-jwks-uri=DNS`. Cleanroom destrói container k3d → config file deletado → K3s volta para default (anonymous-auth=false). Fulcio com CACert AINDA precisará anonymous discovery (CACert resolve TLS trust, não AUTH).
- **Impacto:** Mesmo com CACert correto, Fulcio receberá 401 Unauthorized do discovery endpoint. Phase 3 manual probe + Phase 4 LIVE build vão falhar.
- **Fix sugerido:** Phase -1 ou primeiro step Phase 0 re-aplica K3s config.yaml + restart k3s container (~10s downtime, preserva PVCs). Documentar em README que esses 2 K3s flags são pré-requisito permanente, não one-shot iter-7 fix.

### EC-3: ClusterRoleBinding system:unauthenticated não persistente

- **Task afetada:** Implicito em todo plan
- **Familia:** State / Permission
- **Cenário:** Iter 7 criou CRB `service-account-issuer-discovery-anon`. Cleanroom recriou cluster → CRB desapareceu. Mesmo com K3s anonymous-auth=true + CACert configurado, RBAC ainda bloqueia anonymous access aos endpoints (default K8s não binda role a system:unauthenticated).
- **Impacto:** Discovery retorna 403 Forbidden em vez de 200. Cascade fail em todos os subsequent phases.
- **Fix sugerido:** Phase -1 inclui `kubectl apply -f infra/pulumi-platform/values-fulcio-oidc-overrides/cenario-a-anonymous-discovery.yaml` (CRB já existe no repo). Idempotente; safe re-apply.

### EC-4: --reuse-values vs --install ambiguity gera 2 paths divergentes

- **Task afetada:** T2.1 (helm upgrade)
- **Familia:** Integration
- **Cenário:** EC-1 sugere `--install` flag, mas plan v1.0 explicitly diz `--reuse-values`. Esses dois flags são incompatíveis na semântica: `--install` ignora previous values OR install fresh; `--reuse-values` requer release existir + preserva previous values. Sem clareza, ou Phase 2 falha (EC-1) ou usa values defaults inadequados (sem nossa custom OIDCIssuers entry).
- **Impacto:** Phase 2 ambígua. Operator/Claude pode aplicar comando errado e ter erro confuso.
- **Fix sugerido:** Phase 2 deve ser explicit: "Se release não existe, `helm install fulcio sigstore/fulcio --version 2.9.1 -n sigstore-system --create-namespace -f rendered-values.yaml` (com CACert + outros campos base). Se release existe, `helm upgrade fulcio --reuse-values -f rendered-values.yaml`." OU usar sempre `helm upgrade --install -f full-values.yaml` (upsert).

### EC-5: Plan dropa Rekor + CTLog + TUF — sign+attest precisam todos

- **Task afetada:** T2.1 + EC-1 fix
- **Familia:** Integration / Boundary
- **Cenário:** Sigstore stack tem 4+ componentes (fulcio + rekor + ctlog + trillian). Plan v1.0 menciona apenas Fulcio. Se Phase -1 install (correcting EC-1) só instala Fulcio, sign-image vai pegar 201 cert do Fulcio mas attest falha porque Rekor não existe + ctlog signing chain quebra.
- **Impacto:** Phase 4 sign-image SUCCEEDED + attest FAILED ou cosign reject signing path inteiro porque CTLog endpoint não resolve.
- **Fix sugerido:** Phase -1 explicit lista TODOS os componentes Sigstore + Trillian DB + TUF init. Reaproveitar plan derivado `2026-05-27-M07-tenant-build-pipeline-to-3-85-cap-plan.md` M2 Phase 7-13 (Pulumi up Sigstore) como pré-requisito documented. OU criar dependency declaration: "Pré-req: M2 Phase 7-13 do plan derivado executado primeiro".

## SHOULD TEST

### EC-6: K3s CA rotation entre Phase 0 extract e Phase 4 build

- **Task afetada:** T0.1 → T2.1 → T4.1
- **Teste sugerido:** `test_ca_pem_hash_consistent_across_phases` — capturar hash do PEM em Phase 0 + re-extract no início de Phase 4. Se hash diferente, abort com mensagem clara antes de wasted build. K3s rotação durante phases é raro mas possível (k3s restart por outro process).

### EC-7: PEM YAML block scalar indentation off-by-one

- **Task afetada:** T1.1, T2.1 (python render)
- **Teste sugerido:** `test_rendered_yaml_cacert_parseable_via_openssl` — após render, `yq eval '.config.contents.OIDCIssuers[0].CACert'` extract + pipe para `openssl x509 -noout` deve exit 0. Se indentação errada, yq retorna string corrompida e openssl falha. Pega bug antes de helm apply.

### EC-8: Manual probe Phase 3 retorna 400 com mensagem diferente

- **Task afetada:** T3.1 (Fulcio probe)
- **Teste sugerido:** Documentar EXPECTED messages para 4 outcomes:
  - 201 + cert chain → SUCCESS (CACert resolveu)
  - 400 "error processing identity token" → CACert NÃO resolveu (mesmo erro de antes)
  - 400 com message diferente (e.g., "invalid CSR", "audience mismatch") → secondary issue, investigate
  - 401/403 → discovery layer broken (EC-2/EC-3 não aplicados)
  Plan deve classify response explicitly em vez de só "STATUS=201 → continue".

### EC-9: cosign verify Phase 4 acceptance requer TUF root local

- **Task afetada:** T4.1 (acceptance criteria)
- **Teste sugerido:** Acceptance diz "cosign verify exit 0", mas cosign verify requer TUF root local matching o Sigstore stack. Sem `SIGSTORE_ROOT_FILE` ou TUF init, cosign verify pode retornar "x509 unknown authority" mesmo com signing OK. Teste deve incluir setup TUF context OR usar `cosign verify --insecure-ignore-tlog --insecure-ignore-sct` para isolated TLS issue do TUF chain issue.

## DOCUMENT

### EC-10: Phase 5 calcula aggregate manualmente, propenso a drift

- **Task afetada:** T5.1 (SUMMARY.md)
- **Risco aceito:** Plan T5.1 diz "Math: +0.05/12 ≈ +0.004 → ~3.654 rolling". Cálculo manual em pre-release pode drift entre audits. Mitigation: documentar em SUMMARY.md prelúdio que aggregate é "rolling delta" sem recalibrate baseline (consistent com pattern dos audits anteriores 2026-05-26, 2026-05-27). Aceito porque dropdown para Go aggregator script é scope creep maior que o dano.

### EC-11: Plan não cobre re-apply automation post-cleanroom

- **Task afetada:** Notes de execução (final section)
- **Risco aceito:** Plan menciona "post-3.85 enhancement" para auto-hook K3s CA inject em Pulumi/helmfile bootstrap, mas atual estado requer operator/Claude re-executar Phase 0-5 manualmente após cleanroom. Aceito porque: (a) cleanroom é rare event (~weekly cadence); (b) automated hook tem implementation cost > pain do manual re-run; (c) plan tem README documenting o flow. Document como "post-3.85 follow-up plan" em SUMMARY.md.

## Padrões Sistemicos Detectados

| Padrão | Encontrado? | Onde |
|--------|-------------|------|
| Implemented but not wired | Não | Plan é fix declarativo, não new code path |
| Correct code in wrong place | Não | CACert é config schema nativo, código já existe upstream |
| Project name vs ID | Não | N/A |
| ArgoCD notifiers not services | Não | N/A |
| Single ArgoCD App per tenant | Não | N/A |
| **Live vs snapshot credentials** | **Sim** | EC-2 + EC-3: iter-7 LIVE config (K3s config.yaml + CRB) snapshot, perdido em cleanroom — clássico "snapshot vai stale" |
| **Validation procedure proves nothing** | **Sim parcial** | EC-9: cosign verify acceptance pode passar/falhar por razões alheias a CACert fix (TUF init context) |
| **Implemented but not exists** | **Sim** | EC-1: plan assume helm release que NÃO existe — pior que "not wired" porque é "not deployed" |
| Bootstrap circular dependency | Não | Fulcio fetch JWKS via K3s API é unidirecional |
| CF scan imports conflicting apex | Não | N/A |

## Resumo

| Task | Edges encontrados | MUST FIX | SHOULD TEST | DOCUMENT |
|------|-------------------|----------|-------------|----------|
| T0.1 | 1 | 0 | 1 (EC-6) | 0 |
| T1.1 | 1 | 0 | 1 (EC-7) | 0 |
| T2.1 | 3 | 3 (EC-1, EC-4, EC-5) | 0 | 0 |
| T3.1 | 1 | 0 | 1 (EC-8) | 0 |
| T4.1 | 1 | 0 | 1 (EC-9) | 0 |
| T5.1 | 1 | 0 | 0 | 1 (EC-10) |
| Cross-cutting (state assumptions) | 2 | 2 (EC-2, EC-3) | 0 | 0 |
| Notes | 1 | 0 | 0 | 1 (EC-11) |
| **Total** | **11** | **5** | **4** | **2** |

**Veredicto:** **PLANO PRECISA DE AJUSTE — 5 MUST FIX bloqueiam execução**

### Priorização de absorção

1. **EC-1 (helm release ausente)** — bloqueia Phase 2 hard. Sem fix, plan não executa primeira mutação.
2. **EC-2 + EC-3 (K3s config + CRB ausentes)** — cascateiam: mesmo com Sigstore installed + CACert correct, discovery layer broken pelos defaults pós-cleanroom.
3. **EC-5 (Sigstore stack completo)** — sem Rekor/CTLog, sign+attest end-to-end impossible.
4. **EC-4 (--reuse-values vs --install)** — ambiguidade resolvível em 1 frase.

### Mudança estrutural recomendada

Plan v1.0 → v1.1 deve absorver via inserção de **Phase -1 (pré-requisitos LIVE state)**:

```
Phase -1: Restore Sigstore stack + K3s OIDC discovery prereqs (~15min)
  T-1.1 — Apply K3s config.yaml (anonymous-auth=true + jwks_uri DNS) + restart k3s
  T-1.2 — Apply ClusterRoleBinding service-account-issuer-discovery-anon
  T-1.3 — Install Sigstore stack (fulcio + rekor + ctlog + trillian)
          delegate para M2 Phase 7-13 do plan derivado OR Pulumi up sigstore-local stack
  T-1.4 — Verify all Sigstore pods Running 1/1 + discovery 200
```

Esse phase é PRE-flight para que Phase 0-5 do plan original façam sentido. Sem absorção, plan executes em "limbo" (assume state que não existe LIVE).

### Veredicto final

**REPROVADO para implementação direta. APROVADO após bump v1.0 → v1.1 com Phase -1 + EC-1/4 unification.**

Plan core (CACert fix via OIDCIssuers config) **permanece correto e bem-fundamentado**. Issue é apenas **assumption de starting state**. Após absorção dos 5 MUST FIX, plan é viável com effort ~60min (vs ~45min original — +15min Phase -1 prereqs).

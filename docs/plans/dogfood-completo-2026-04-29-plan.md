# Plan: Dogfood Completo 2026-04-29 — 100% da superfície do LoopZero PaaS via CLI público

> **Version 1.0** — Validação end-to-end de TODA a superfície do LoopZero PaaS (44 comandos CLI, 14 seções funcionais da spec) usando templates upstream do `getloopscale/loopzero-stacks` em ambiente local `standalone-multi` (k3d × 3). Substitui experimentos one-off com nginx hand-rolled (que crasharam por incompatibilidade com `readOnlyRootFilesystem`) por dogfood disciplinado: black-box, sem hacks, com Prova de Vida real (HTTP 200 + TLS) e Prova de Morte real (cleanup completo em <5min). Resultado é um relatório técnico publicado em `docs/dogfood/` com matriz coberto/não-coberto/falha por funcionalidade, mais PRs contra `loopzero-stacks` para gaps de templates upstream.

## Context

### O que foi feito antes deste plano

A iteração 8 da remediação `dogfood-2026-04-29-remediation-plan.md` escapou do escopo original e entrou em **digest fix supply-chain VAP** (CHG-2026-04-29-DOGFOOD iter 8/9). Validei, live em k3d-standalone-multi:

- ✅ Pipeline ponta a ponta com cosign signing (build `8c23121123c4ea41` SUCCEEDED em 16min)
- ✅ Manifest renderizado com `image@sha256:8fbd7ab...`
- ✅ VAPs `loopzero-require-image-signature` + `loopzero-restrict-image-registries` admitindo
- ✅ Pod criado, imagem puxada, container começou a executar

Mas **a Prova de Vida não fechou**: o app de teste (`nginx:1.27-alpine` hand-rolled) crashou em `mkdir /var/cache/nginx/client_temp failed (Read-only file system)`. Não é bug da plataforma — `readOnlyRootFilesystem: true` é correto. Foi escolha errada de imagem.

### Evidências motivadoras

- **CLAUDE.md**: declara 25/30 E2E pass (83%) em validação 2026-04-23, mas a validação não exercita TODA a superfície (Sprint 5/preview, Sprint 6/supply-chain, Sprint 10/static, Sprint 11/billing entraram após).
- **Repo `getloopscale/loopzero-stacks`**: 30+ templates upstream que são a forma canônica de exercitar o produto como usuário real. Confirmado via agente: `examples/monorepo-turbo` é o mais completo (api Express + web Next.js + 2 Dockerfiles), mas ainda lacks: secrets, Postgres/Redis, workers, custom domains.
- **Spec `docs/cli-reference.md`**: 44 comandos em 14 seções funcionais. Plano anterior cobriu deploy + scale + promote + rollback + concurrent rejection (≈12 comandos). Os outros 32 nunca foram dogfooded em sequência.
- **Achados desta sessão**: silent-success em `routes/source.go` (não-fatal in dev), CLI `uploadSource` ignorando `LOOPZERO_TOKEN` env, `MINIO_ENDPOINT` sem scheme, `buildkit-registry-config` Secret faltando no bootstrap, `loopzero-minio-credentials` faltando `AWS_DEFAULT_REGION`+`AWS_ENDPOINT_URL`. Todos esses bugs só apareceram durante dogfood real — testes unitários passaram.

### Por que precisa ser "completo"

Pré-release tem estado "98% confidence" em CLAUDE.md, mas o histórico de bugs descobertos via dogfood (B12, B17, AUDIT-17→23, F-07, sources upload, este iteração ×3) mostra que cobertura unitária + componente não substitui dogfood disciplinado. Lançar com cobertura parcial = lançar com bombas-relógio.

## Objective

`loopzero deploy` → app responde HTTP 200 com TLS válido E `loopzero project delete` → cluster volta ao estado pré-deploy em <5min, para um template multi-app com TODOS os tipos de app (frontend/server/worker), TODAS as dependências (secrets/Postgres/Redis), TODOS os fluxos de delivery (container + static), em ambiente local idêntico a produção (Rule 10).

Metas mensuráveis:

1. **44/44 comandos CLI exercitados** (não apenas mencionados — invocados e validados)
2. **≥95% das funcionalidades do spec** classificadas em coberto/falha (não "não testado")
3. **Zero hacks**: zero `kubectl patch` ad-hoc; toda divergência de produção vira fix em código com PR
4. **Templates upstream**: cada gap em loopzero-stacks vira PR contra o repo público
5. **Prova de Vida ≥3 apps simultaneamente** (api server + web static + worker) na mesma `loopzero deploy`
6. **Prova de Morte completa**: namespace deletado, ArgoCD app removido, S3 prefix limpo, CF KV entry purgado
7. **Relatório técnico** publicado em `docs/dogfood/2026-04-29-completo.md` com matriz, achados, recomendações
8. **18 bugs B1-B18** + 5 bugs desta sessão (silent-success S3, CLI auth, MINIO_ENDPOINT, buildkit-registry-config, minio-credentials region) com regression test ou check explícito

## ADRs

### D1 — Templates upstream do `loopzero-stacks` como Single Source of Truth

**Decision:** Todo template usado para dogfood vem de `https://github.com/getloopscale/loopzero-stacks`. Não criar templates locais nem forks.

**Rationale:** LoopZero é PaaS — o usuário real usa templates upstream. Forks locais fazem o teste vazar realismo. Se um template não existe, a resposta correta é PR contra o repo, não hack local.

**Consequences:** Habilita reprodutibilidade (qualquer dev externo roda o mesmo dogfood). Constrange — gaps de templates ficam visíveis e exigem trabalho upstream.

### D2 — Cada gap de cobertura → PR contra `loopzero-stacks`

**Decision:** Para cada cenário não coberto pelos templates atuais (secrets, custom domains, workers com BullMQ, etc.), abrir PR no `loopzero-stacks` adicionando o template em vez de inline em `infra/tests/`.

**Rationale:** Aproveita o trabalho como contribuição upstream visível. Garante que o template fica disponível para usuários reais. Templates locais viram dívida técnica invisível.

**Consequences:** Trabalho de teste vira valor de produto. Velocidade reduz se o repo upstream tem revisão lenta — mitigação: rodar dogfood com PR aberto (branch local), mergear depois.

### D3 — Prova de Vida = HTTP 200 + TLS válido + log audit

**Decision:** Um app só conta como "deployed" quando: (a) `curl -fsSL https://<host>/health` retorna 200 com cert válido, (b) registro do request aparece em `loopzero logs <project> --app <app>`, (c) entry em `loopzero audit list --action deploy`.

**Rationale:** Pod Ready não é Prova de Vida — pod pode estar Ready com app retornando 500. Sem TLS, não está em produção. Sem log, observability quebrada.

**Consequences:** Padrão alto. Em standalone-multi, TLS é via `selfsigned-issuer` (não-confiável publicamente, mas válido pelo schema X.509). Loki cross-cluster pode falhar — vira gap explícito.

### D4 — Prova de Morte = cleanup verificável em <5min

**Decision:** `loopzero project delete <name>` deve resultar em: namespace `loopzero-tn-{tenant}-{env}` deleted (não Terminating perpétuo), ArgoCD App removed (não orphan), S3 prefix `{tenant}/source/` limpo de bundles, CF KV entry purgada (se aplicável). Tudo em ≤5min.

**Rationale:** Gap conhecido na própria CLAUDE.md ("namespace cleanup" mencionado mas não validado em E2E). Lançar PaaS com delete vazado é refund pesadelo + GDPR violation.

**Consequences:** Pode forçar implementação de finalizers em CRDs ou retry loops em DomainProvisioner. Mitigação: documentar como gap se for além do escopo, NÃO esconder.

### D5 — Black-box rigoroso: zero kubectl patch durante teste

**Decision:** Durante execução do dogfood, zero `kubectl patch`, `kubectl edit`, `helm upgrade --set` ad-hoc. Se infra precisa de fix, parar o teste, corrigir helm/pulumi/código, commitar, re-rodar do Phase 0.

**Rationale:** Cada hack durante teste mascara um bug que vai morder em produção. Histórico desta sessão (5 hacks: REGISTRY_INSECURE, additionalPrefixes, AWS_DEFAULT_REGION, AWS_ENDPOINT_URL, buildkit-registry-config) prova que todos eram bugs reais que o dogfood revelou.

**Consequences:** Teste demora mais (cada bug = ciclo de fix+commit+rebuild+redeploy). Mitigação: aceitar — é trabalho real, não overhead.

### D6 — Cluster alvo único: k3d-standalone-multi

**Decision:** Todo o dogfood roda em `k3d-loopzero-{mgmt,build,workload}` com profile `standalone-multi` (helmfile environment). Não validar em DOKS prod simultaneamente.

**Rationale:** Standalone-multi foi declarado o ambiente "local equals production" (Rule 10). Se o dogfood não passa local, não vai passar em prod. Se passa local, prod vira validação separada (smaller surface).

**Consequences:** Não cobre cell-2 routing, Loki cross-cluster real, CF Tunnel produção. Esses ficam como follow-up em DOKS após plano completo.

### D7 — Ordem dos cenários otimizada para shedding rápido de problemas

**Decision:** Phase 1 (baseline monorepo-turbo) primeiro; Phase 2 (extensions) só depois de baseline 100% green. Phase 3 (lifecycle ops) só após Phase 2.

**Rationale:** Sem baseline funcional, lifecycle ops testam bugs em cima de bugs. Detectar problema fundamental em Phase 1 polui debug de Phase 2+.

**Consequences:** Não paraleliza phases. Aceito — clareza > throughput nesse plano.

### D8 — Relatório por phase, não só no final

**Decision:** Cada phase produz seção em `docs/dogfood/2026-04-29-completo.md`. Phase 7 consolida, mas seções vão sendo escritas durante a execução.

**Rationale:** Memória do agente é volátil. Escrever durante = relatório fiel. Escrever no final = retrospectiva inventiva.

**Consequences:** Relatório vira artefato vivo, comitado incremental. Versionado em git.

## Dependency Graph

```
Phase 0 (Pre-flight)
    │
    ▼
Phase 1 (Baseline monorepo-turbo)  ──── (gate: ≥1 app HTTP 200) ──┐
    │                                                              │
    ▼                                                              │
Phase 2 (Extensions: secrets/postgres/workers/static/domains)     │
    │                                                              │
    ▼                                                              │
Phase 3 (Lifecycle: scale/promote/rollback/cancel/delete) ◀──────┘
    │
    ├──▶ Phase 4 (GitHub App auto-deploy)        ──┐
    │                                              │
    ├──▶ Phase 5 (Preview environments)           ──┤── parallel
    │                                              │
    └──▶ Phase 6 (Contracts: JSON/exit/Rule 3)    ──┘
                                                    │
                                                    ▼
                                            Phase 7 (Relatório final)
```

Phases 4, 5, 6 paralelizáveis após Phase 3 (não dependem entre si). Phase 7 só após todas as anteriores.

---

## Phase 0: Pre-flight cluster validation

**Objective:** garantir que k3d-standalone-multi está em estado idêntico a produção antes do dogfood iniciar — qualquer divergência detectada agora vira fix em helm/pulumi, não hack durante teste.

### T0.1 — Cluster health probe (3 contextos, todos os pods)

#### Objective
Validar que `k3d-loopzero-mgmt`, `k3d-loopzero-build`, `k3d-loopzero-workload` estão Up com ≥95% pods Running e zero CrashLoopBackOff em namespaces de plataforma.

#### Evidence
Iteração anterior teve `loopzero-build-build-...-clone-repo` em Error por bundle MinIO ausente (silent-success S3 do api). Sem health check antecipado, isso aparece no meio do dogfood e contamina debug. Entry log live: `mc: <ERROR> Unable to prepare URL for copying. Object does not exist`.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — (NEW) script idempotente que valida 3 clusters
docs/dogfood/2026-04-29-completo.md — (NEW) seção "Phase 0 — Pre-flight" com checks executados
```

#### Deep file dependency analysis
- `infra/tests/dogfood/preflight.sh`: novo arquivo, não tem dependentes ainda. Vai ser invocado por Phase 0 explicit call e referenciado em Phase 7 (relatório). Convenção do repo: scripts de teste em `infra/tests/`, executáveis (`chmod +x`).
- `docs/dogfood/2026-04-29-completo.md`: novo, não tem leitores ainda; Phase 7 vai consumir.

#### Deep Dives
- **Pods esperados por cluster** (referência, não exaustivo):
  - `k3d-loopzero-mgmt`: loopzero-system (loopzero-api, gitea, postgres, minio, traefik, cert-manager, argocd, ory-hydra, ory-kratos, redis), monitoring (grafana, prometheus, loki, tempo, promtail), nfs-system, kube-system
  - `k3d-loopzero-build`: argo (argo-workflows-server, argo-workflows-workflow-controller), kube-system
  - `k3d-loopzero-workload`: cert-manager, cloudflared, cosign-system (policy-controller, policy-controller-webhook), keda, traefik, loopzero-tenants, kube-system
- **Critério de OK**: por namespace, ≥95% pods Running. Zero CrashLoopBackOff. Pods Pending OK se for cluster-scaling, NOK se for ImagePullBackOff/CreateContainerConfigError.
- **Edge case**: namespaces Terminating (visto na iteração: 5 namespaces `loopzero-test-*`) — script deve tolerar mas alertar (sinal de cleanup incompleto, conecta com D4).

#### Tasks
1. Criar `infra/tests/dogfood/preflight.sh` com 3 funções: `check_cluster_health(ctx)`, `check_secret(ctx, ns, name, [keys...])`, `check_vap(ctx, name, [expected_prefix])`.
2. Implementar `check_cluster_health` que itera namespaces, conta Running/Total, falha se <95%.
3. Implementar `check_secret` que verifica existência E todas as keys presentes (não confiar em "Secret existe" — buildkit-registry-config tinha isso e quebrou).
4. Implementar `check_vap` que faz `kubectl get vap <name> -o jsonpath` e valida que `<expected_prefix>` está nas validations.
5. Listar checks específicos que vão rodar (5 checks idempotentes).
6. Adicionar sumário final com exit code 0/1.

#### TDD
```
RED:    test_preflight_fails_when_pod_crashlooping() — cria pod CrashLoopBackOff em ns dummy, script deve falhar com exit 1
RED:    test_preflight_fails_when_secret_missing_key() — remove key do secret, script deve falhar
RED:    test_preflight_fails_when_vap_missing_prefix() — remove prefix do VAP, script deve falhar
RED:    test_preflight_passes_on_clean_cluster() — em cluster limpo, script retorna exit 0
GREEN:  Implementar `infra/tests/dogfood/preflight.sh` com as 4 condições acima
REFACTOR: Extrair messages de erro para função `report_failure()` reusável
VERIFY: bash infra/tests/dogfood/preflight.sh && echo OK
```

#### Acceptance Criteria
- [ ] Script roda em <30s
- [ ] Cobre 3 clusters
- [ ] Detecta pods CrashLoopBackOff, Pending>2min, ImagePullBackOff
- [ ] Verifica `buildkit-registry-config` existe em `argo` ns do build
- [ ] Verifica `loopzero-minio-credentials` tem AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, MINIO_ENDPOINT, AWS_DEFAULT_REGION, AWS_ENDPOINT_URL
- [ ] Verifica VAP `loopzero-restrict-image-registries` tem `k3d-loopzero-registry:5000/` na allowlist
- [ ] Verifica VAP `loopzero-require-image-signature` está enforced
- [ ] Pass: shellcheck no script (zero warnings)
- [ ] Pass: script é idempotente (rodar 2× consecutivos não muda estado)

#### DoD
- [ ] Script comitado em `infra/tests/dogfood/preflight.sh`
- [ ] Executável, shellcheck clean
- [ ] Roda em <30s do trecho fim-a-fim
- [ ] Documentado em `docs/dogfood/2026-04-29-completo.md` Phase 0
- [ ] CI gate em `.github/workflows/ci.yml` opcional invoca `preflight.sh` quando tag `dogfood`

### T0.2 — Token regeneration via seed-local-user.sh

#### Objective
Garantir que `/home/paulo/.loopzero/local-dogfood-token.txt` está válido (TTL não expirado), regenera via seed se necessário.

#### Evidence
Tokens emitidos por Hydra `client_credentials` têm TTL ~1h (default Hydra). Iteração anterior teve `Upload rejected: Invalid or expired token` no meio do deploy. Tem que checar/regenerar antes.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar check de validade do token + regeneração condicional
infra/helmfile/scripts/seed-local-user.sh — (já existe, usar)
```

#### Deep file dependency analysis
- `seed-local-user.sh`: validado na iteração 8. Insere user → tenant → membership → mint JWT via Hydra. Idempotente (UPSERT pattern).
- `preflight.sh`: extensão de T0.1.

#### Deep Dives
- **Token validation**: `jwt decode` ou `python3 -c "import jwt; print(jwt.decode(...)['exp'])"`. Sem libs: `cut -d. -f2 | base64 -d | jq .exp`.
- **Regeneration**: chamar `seed-local-user.sh` se `exp - now < 600s` (10min de buffer).
- **Edge case**: token valido mas API rejeita por JWKS rotation — regenerar resolve.

#### Tasks
1. Adicionar `check_token_validity()` em preflight.sh.
2. Decode JWT (sem verificar assinatura — só extrair `exp`).
3. Comparar com `date +%s`, regenerar se diff < 600s.
4. Validar token novo via `curl /api/v1/auth/whoami` retorna 200.

#### TDD
```
RED:    test_token_regenerated_when_expired() — token forjado com exp passado, preflight deve regenerar
RED:    test_token_kept_when_valid() — token com exp +2h, preflight não deve regenerar
GREEN:  Implementar check_token_validity()
REFACTOR: None expected
VERIFY: bash infra/tests/dogfood/preflight.sh
```

#### Acceptance Criteria
- [ ] Token expirado é regenerado automaticamente
- [ ] Token válido (exp > now+600s) é mantido
- [ ] Falha de regeneração (Hydra down) bloqueia preflight com mensagem clara

#### DoD
- [ ] Função integrada em preflight.sh
- [ ] Tested em ambos cenários (expirado/válido)
- [ ] Documentado em Phase 0 do relatório

### T0.3 — VAP allowlist + secret de build verificados

#### Objective
Confirmar `loopzero-restrict-image-registries` aceita `k3d-loopzero-registry:5000/`, `loopzero-require-image-signature` está enforced (warn-mode em local), e `buildkit-registry-config` Secret existe em `argo`.

#### Evidence
Iteração 9 descobriu que VAP estava bloqueando k3d prefix (B-novo-vap-allowlist), e `buildkit-registry-config` Secret estava ausente (B-novo-bootstrap-secret). Ambos foram fixos em código (helm chart `admission-policies` agora suporta `additionalPrefixes`, helmfile script `generate-build-secrets.sh` cria o Secret se `LOOPZERO_REGISTRY_ENDPOINT` set), mas precisamos validar que o cluster atual tem essas correções aplicadas.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar 3 checks: VAP allowlist, VAP signature, buildkit-registry-config
```

#### Deep file dependency analysis
- Depende dos arquivos commitados nesta sessão:
  - `infra/helm/admission-policies/templates/vap-restrict-registries.yaml` (suporta additionalPrefixes)
  - `infra/helm/admission-policies/values.yaml` (default vazio)
  - `infra/helmfile/environments/standalone-multi.yaml` (opt-in `k3d-loopzero-registry:5000/`)

#### Deep Dives
- **VAP CEL parsing**: `kubectl get vap loopzero-restrict-image-registries -o jsonpath='{.spec.validations[0].expression}'` retorna a CEL expression. Grep por `k3d-loopzero-registry:5000/`.
- **Secret completeness**: além de existir, validar bytes >0 em `config.json`.

#### Tasks
1. `check_vap_includes_prefix(name, prefix)` em preflight.sh.
2. `check_secret_completeness(ctx, ns, name, key)` valida bytes>0.
3. Integrar ambos no fluxo principal de preflight.

#### TDD
```
RED:    test_preflight_fails_when_vap_missing_k3d_prefix()
RED:    test_preflight_fails_when_buildkit_secret_empty()
GREEN:  Implementar checks
REFACTOR: None expected
VERIFY: bash infra/tests/dogfood/preflight.sh
```

#### Acceptance Criteria
- [ ] VAP `loopzero-restrict-image-registries` validado para conter `k3d-loopzero-registry:5000/`
- [ ] VAP `loopzero-require-image-signature` confirmed enforced (kind=ValidatingAdmissionPolicy, binding em namespaces tenant)
- [ ] Secret `argo/buildkit-registry-config` existe + `config.json` não vazio
- [ ] Secret `argo/loopzero-minio-credentials` tem 5 keys (incluindo AWS_DEFAULT_REGION + AWS_ENDPOINT_URL)

#### DoD
- [ ] 4 checks adicionais em preflight.sh
- [ ] Cluster atual passa ou tem fix documentado
- [ ] Phase 0 do relatório lista validações realizadas

### T0.4 — REGISTRY_INSECURE + MINIO_ENDPOINT verificados em loopzero-api deploy

#### Objective
Confirmar que o deploy `loopzero-api` no namespace `loopzero-system` tem `REGISTRY_INSECURE=true` e `MINIO_ENDPOINT` com scheme `http://`.

#### Evidence
Iteração 9 descobriu que sem REGISTRY_INSECURE, BuildKit fala HTTPS contra k3d-registry HTTP-only e quebra. Sem scheme em MINIO_ENDPOINT, S3 uploader retorna "unsupported protocol scheme". Ambos fixados em values + config.go default, mas runtime pode estar desincronizado.

#### Files to edit
```
infra/tests/dogfood/preflight.sh — check env vars do deploy loopzero-api
```

#### Deep Dives
- `kubectl -n loopzero-system get deploy loopzero-api -o jsonpath='{range .spec.template.spec.containers[0].env[?(@.name=="REGISTRY_INSECURE")]}{.value}{end}'`
- Se vazio E values do helm declaram, é drift (helm não aplicou ou kubectl set image apagou).

#### Tasks
1. `check_deploy_env(ctx, ns, deploy, var, expected)` em preflight.sh.
2. Verificar `REGISTRY_INSECURE=true`.
3. Verificar `MINIO_ENDPOINT` começa com `http://` ou `https://` (não bare host).

#### TDD
```
RED:    test_preflight_fails_when_registry_insecure_missing()
GREEN:  Implementar check_deploy_env
REFACTOR: None expected
VERIFY: bash infra/tests/dogfood/preflight.sh
```

#### Acceptance Criteria
- [ ] `REGISTRY_INSECURE=true` validated no live deploy
- [ ] `MINIO_ENDPOINT` tem scheme

#### DoD
- [ ] Check integrado em preflight.sh
- [ ] Phase 0 do relatório confirma valores live

### T0.5 — DNS local resolvendo `*.loopzero.local`

#### Objective
Validar que `loopzero deploy` em standalone-multi consegue resolver `<tenant>-<project>-<app>-<env>.loopzero.local` via /etc/hosts ou Cloudflare Tunnel local.

#### Evidence
IngressRoute do app web da iteração 9 expôs `Host(\`tn-e2etest-repotest-web-production.loopzero.local\`)`. `curl` direto contra esse host falha sem resolution. Prova de Vida exige TLS+host válido (D3).

#### Files to edit
```
infra/tests/dogfood/preflight.sh — adicionar check de DNS
infra/scripts/setup-local-hosts.sh — (NEW se não existir) idempotente, adiciona entrada em /etc/hosts
```

#### Deep Dives
- **Opção 1**: /etc/hosts manual (`127.0.0.1 *.loopzero.local` não funciona em /etc/hosts; precisa `127.0.0.1 tn-e2etest-...loopzero.local` por host)
- **Opção 2**: dnsmasq local com wildcard `address=/.loopzero.local/127.0.0.1`
- **Opção 3**: Traefik LoadBalancer no k3d expõe :80/:443 para host, e usar `--resolve` em curl
- **Decisão**: opção 3 — curl com `--resolve` evita mexer em /etc/hosts. Documentar em README do dogfood.

#### Tasks
1. Detectar se k3d expõe :80/:443 via `docker port k3d-loopzero-workload-serverlb`.
2. Documentar comando `curl --resolve <host>:443:127.0.0.1 https://<host>` em Phase 0 do relatório.
3. (Opcional) Criar `setup-local-hosts.sh` se for invasivo demais usar `--resolve` em todo curl.

#### TDD
```
RED:    test_preflight_warns_when_traefik_lb_not_exposed()
GREEN:  Implementar check de docker port
REFACTOR: None expected
VERIFY: docker port k3d-loopzero-workload-serverlb | grep ":443"
```

#### Acceptance Criteria
- [ ] Phase 0 documenta como curl resolve hosts `*.loopzero.local` em standalone-multi
- [ ] Comando padrão de Prova de Vida usa `--resolve`

#### DoD
- [ ] Documentação clara em relatório
- [ ] Comando reusable em Phase 1+

---

## Phase 1: Baseline monorepo-turbo dogfood

**Objective:** clonar `examples/monorepo-turbo` do `loopzero-stacks` upstream, fazer `loopzero deploy` puro, e atingir Prova de Vida (HTTP 200 + TLS) para AMBOS os apps (api Express + web Next.js).

### T1.1 — Clone loopzero-stacks e cópia para diretório de trabalho

#### Objective
Trazer template upstream sem modificações para `/tmp/dogfood-iter9-turbo`, garantindo reprodutibilidade.

#### Evidence
ADR D1: usar templates upstream como SoT. Cloning fresh evita "funcionou na minha máquina" caused by stale local state.

#### Files to edit
```
infra/tests/dogfood/setup-monorepo-turbo.sh — (NEW) script idempotente que clona + copia
```

#### Deep file dependency analysis
- Novo script. Output esperado: `/tmp/dogfood-iter9-turbo/` com `loopzero.yaml`, `apps/api/`, `apps/web/`, `package.json`, `turbo.json`, mas SEM `node_modules/` (loopzero deploy ignora — testar).

#### Deep Dives
- **git clone shallow** (`--depth 1`) para velocidade.
- **Clone path**: `/tmp/loopzero-stacks` (ephemeral) vs `~/.loopzero/stacks` (persistent). Decisão: `/tmp/` — fresh por sessão evita drift.
- **loopzero.yaml literal upstream** já confirmado pelo agente:
  ```yaml
  version: 1
  project: "monorepo-turbo"
  apps:
    api: { path: apps/api, framework: express, type: server, port: 3001 }
    web: { path: apps/web, framework: nextjs, type: frontend }
  ```

#### Tasks
1. `git clone --depth 1 https://github.com/getloopscale/loopzero-stacks /tmp/loopzero-stacks`
2. `cp -r /tmp/loopzero-stacks/examples/monorepo-turbo /tmp/dogfood-iter9-turbo`
3. Validar `loopzero.yaml` presente, `apps/api/Dockerfile` presente, `apps/web/Dockerfile` presente.

#### TDD
```
RED:    test_setup_fails_when_loopzero_stacks_unreachable() — mock git clone failure, script exit !=0
RED:    test_setup_creates_clean_workdir() — verifica estrutura pós-script
GREEN:  Implementar script
REFACTOR: None expected
VERIFY: bash infra/tests/dogfood/setup-monorepo-turbo.sh && ls /tmp/dogfood-iter9-turbo/
```

#### Acceptance Criteria
- [ ] Script idempotente (rodar 2× = mesmo resultado, exclui /tmp/dogfood-iter9-turbo no início)
- [ ] Não instala node_modules (loopzero deploy faz isso)
- [ ] Falha clara se loopzero-stacks indisponível

#### DoD
- [ ] Script committed
- [ ] Executável, shellcheck clean
- [ ] Documentado em Phase 1

### T1.2 — `loopzero validate` reconhece o monorepo

#### Objective
Validar que `loopzero validate` lê `loopzero.yaml` upstream sem erro, identifica 2 apps com frameworks corretos.

#### Evidence
`cli/src/commands/validate.ts` invoca o monorepo validator (CLAUDE.md: "30 frameworks"). Frameworks `express` e `nextjs` devem estar reconhecidos. Se não, é bug do CLI.

#### Files to edit
```
(N/A — black-box test)
docs/dogfood/2026-04-29-completo.md — Phase 1 sub-section
```

#### Deep Dives
- **Comando**: `cd /tmp/dogfood-iter9-turbo && loopzero validate --json`
- **Output esperado**: `{"valid": true, "apps": [{"name":"api","framework":"express","type":"server"},{"name":"web","framework":"nextjs","type":"frontend"}]}`
- **Edge case**: framework `nextjs` pode ser interpretado como server (Next.js standalone) ou frontend (static export). Isso é decisão do `build_plan.go` — observar.

#### Tasks
1. Run `loopzero validate --json` no diretório.
2. Capturar output em `docs/dogfood/2026-04-29-completo.md`.
3. Identificar discrepâncias com spec.

#### TDD
```
N/A — verificação dogfood, sem novo código.
VERIFY: cd /tmp/dogfood-iter9-turbo && loopzero validate --json | jq -e '.valid==true'
```

#### Acceptance Criteria
- [ ] `loopzero validate --json` exit 0
- [ ] Output contém 2 apps
- [ ] Frameworks reportados batem com `loopzero.yaml`

#### DoD
- [ ] Output capturado em relatório
- [ ] Se falhar, bug logado em GitHub issue

### T1.3 — `loopzero deploy --env production --no-wait` submete build multi-app

#### Objective
Submeter deploy para 2 apps em paralelo, capturar `build_id`, validar que workflow Argo é submetido.

#### Evidence
Spec §3 `loopzero deploy`: comando aceita `--no-wait` e retorna build_id. Iteração 9 já executou isso para 1 app (`web` solo). Multi-app é o novo cenário.

#### Files to edit
```
(N/A)
```

#### Deep Dives
- **Idempotency**: `loopzero deploy` com mesmo commitSHA + window de 60s = mesma idempotency_key → API retorna 200 + mesmo build_id (não 202).
- **Multi-app build**: Argo Workflow gera DAG com `build-image-{name}` e `sign-image-{name}` por app. Total ≈12 nodes (clone + validate + 2× build + 2× sign + 2× sbom + finalize).
- **Plan caps**: tenant `tn-e2etest` é Pro (CLAUDE.md menciona Starter+Pro launchable). Pro permite até 3 projects → OK para 1 project com 2 apps. HPA Pro=5 réplicas/app.

#### Tasks
1. Capturar token via `cat ~/.loopzero/local-dogfood-token.txt`.
2. `LOOPZERO_TOKEN=... LOOPZERO_URL=http://localhost:13001 loopzero deploy --env production --no-wait --json`.
3. Extrair `build_id` do JSON.
4. Validar `kubectl --context k3d-loopzero-build -n argo get wf -l loopzero.io/build-id=$build_id` retorna 1 workflow.

#### TDD
```
N/A — black-box dogfood.
VERIFY: loopzero deploy --json | jq -r .build_id | xargs -I{} kubectl -n argo get wf -l loopzero.io/build-id={}
```

#### Acceptance Criteria
- [ ] `loopzero deploy --json` retorna `build_id`, `stream_url`, `status:submitted`
- [ ] Workflow Argo criado
- [ ] DAG do workflow contém `build-image-api`, `build-image-web`, `sign-image-api`, `sign-image-web`

#### DoD
- [ ] Build_id registrado em relatório
- [ ] Workflow YAML capturado para inspeção

### T1.4 — Workflow conclui com cosign signing dos 2 apps

#### Objective
Aguardar workflow completar e validar que `sign-image-api` e `sign-image-web` ambos Succeeded com `digest` output populado.

#### Evidence
Iteração 9 validou para 1 app. Multi-app é o caso real do digest fix em `build_reconcile_complete.go` que faz loop sobre `buildApps`.

#### Deep Dives
- **Tempo esperado**: ≈8-15min para 2 apps em standalone-multi (BuildKit single-node).
- **Failure mode crítico**: se `sign-image-api` succeed mas `sign-image-web` fail, e o reconcile loop só checar 1, manifest gerado tem `web:tag` (sem digest) → VAP rejeita pod web → bug do digest fix REAL detectado.

#### Tasks
1. Polling loop até `wf.status.phase = Succeeded` ou timeout 20min.
2. `kubectl get wf -o jsonpath` extrair outputs de `sign-image-api` e `sign-image-web`.
3. Validar `digest` field presente e formato `sha256:[a-f0-9]{64}`.

#### TDD
```
N/A — black-box.
VERIFY: kubectl get wf $WF -o json | jq -e '.status.nodes | to_entries | map(select(.value.displayName | startswith("sign-image-"))) | length == 2 and all(.value.outputs.parameters[0].value | startswith("sha256:"))'
```

#### Acceptance Criteria
- [ ] Workflow Succeeded em <20min
- [ ] 2 sign-image steps Succeeded
- [ ] 2 digests sha256 capturados

#### DoD
- [ ] Outputs documentados em relatório
- [ ] Tempo de build registrado

### T1.5 — Manifest renderizado com `image@sha256:` em ambos os apps

#### Objective
Validar que o manifest pushado para Gitea contém `image@sha256:...` para AMBOS api e web (não só `Apps[0]`).

#### Evidence
Cenário crítico do digest fix: `build_reconcile_complete.go` itera sobre `buildApps` e popula `digests` map para cada app. Se eu errei o loop, só `api` (primeiro) teria digest. Multi-app é o teste que confirma.

#### Deep Dives
- **Onde olhar**: repo Gitea `loopzero-platform/tn-e2etest-gitops.git`, branch `main`, path `production/{api,web}-deployment.yaml`.
- **Expected**: ambos têm `image: k3d-loopzero-registry:5000/tn-e2etest/monorepo-turbo-{api,web}@sha256:{digest}`.

#### Tasks
1. `git clone http://gitea-http.loopzero-system.svc.cluster.local:3000/loopzero-platform/tn-e2etest-gitops.git /tmp/gitops-check` (port-forward se preciso).
2. `grep -E 'image:.*@sha256:' /tmp/gitops-check/production/*.yaml`.
3. Validar 2 matches (1 por app).

#### TDD
```
N/A — black-box.
VERIFY: grep -E 'image: k3d-loopzero-registry:5000/tn-e2etest/monorepo-turbo-(api|web)@sha256:' /tmp/gitops-check/production/*.yaml | wc -l ==2
```

#### Acceptance Criteria
- [ ] Manifest api contém digest
- [ ] Manifest web contém digest
- [ ] Digests batem com workflow outputs (T1.4)

#### DoD
- [ ] Diff git registrado em relatório
- [ ] Confirmado fix multi-app

### T1.6 — Pods Ready em ambos os deployments (VAPs admitiram)

#### Objective
Validar `kubectl get deploy api,web -n loopzero-tn-e2etest-production` mostra `READY 1/1` em ambos. Sem CrashLoopBackOff.

#### Deep Dives
- **VAP layered check**: pod tem que passar `loopzero-require-image-signature` (digest présent), `loopzero-restrict-image-registries` (k3d prefix), `loopzero-disallow-latest-tag` (não usa :latest), `loopzero-require-resource-limits` (manifests LoopZero geram limits).
- **Expected**: ambos pods Running em <2min após ArgoCD sync.

#### Tasks
1. `kubectl --context k3d-loopzero-workload -n loopzero-tn-e2etest-production get deploy -w` até timeouts.
2. Se Pending, `kubectl describe rs` para diagnosticar (admission rejection vs ImagePull vs schedule).
3. Validar `READY 1/1` em api e web.

#### TDD
```
N/A.
VERIFY: kubectl -n loopzero-tn-e2etest-production get deploy api web -o jsonpath='{.items[*].status.readyReplicas}' == "1 1"
```

#### Acceptance Criteria
- [ ] api READY 1/1 em <3min após sync
- [ ] web READY 1/1 em <3min após sync
- [ ] Zero events `FailedCreate` por VAP

#### DoD
- [ ] Pods Running confirmado
- [ ] Tempo até Ready documentado

### T1.7 — Prova de Vida: HTTP 200 + TLS válido para ambos

#### Objective
`curl -fsSL --resolve` em ambos os hosts retorna 200 com cert válido (selfsigned-issuer).

#### Evidence
ADR D3.

#### Deep Dives
- **Hosts esperados**: `tn-e2etest-monorepo-turbo-api-production.loopzero.local`, `tn-e2etest-monorepo-turbo-web-production.loopzero.local`.
- **TLS check**: `openssl s_client -servername <host> -connect 127.0.0.1:443 -showcerts < /dev/null` valida cert chain.
- **HTTP check**: `curl -fsSL --resolve <host>:443:127.0.0.1 -k https://<host>/` (k para selfsigned em local). Bench Lab/Prod usaria sem `-k`.

#### Tasks
1. Para cada app, run `curl --resolve` e capturar status + body.
2. Para cada app, run `openssl s_client` e validar cert válido (não expirado, hostname correto).
3. Documentar response time.

#### TDD
```
N/A.
VERIFY: 
  curl --resolve tn-e2etest-monorepo-turbo-api-production.loopzero.local:443:127.0.0.1 -k https://tn-e2etest-monorepo-turbo-api-production.loopzero.local/ -o /dev/null -w "%{http_code}" == 200
  curl --resolve tn-e2etest-monorepo-turbo-web-production.loopzero.local:443:127.0.0.1 -k https://tn-e2etest-monorepo-turbo-web-production.loopzero.local/ -o /dev/null -w "%{http_code}" == 200
```

#### Acceptance Criteria
- [ ] api HTTP 200
- [ ] web HTTP 200
- [ ] Cert TLS válido (não expirado, hostname match)

#### DoD
- [ ] Outputs registrados em relatório
- [ ] Phase 1 marca como ✅ green

### T1.8 — Audit log + Loki entry para o request da Prova de Vida

#### Objective
Validar que (a) `loopzero audit list --action deploy` mostra a entrada do build, (b) o request do `curl` aparece em `loopzero logs <project> --app <app>`.

#### Evidence
ADR D3 inclui audit/logs como parte da Prova de Vida.

#### Deep Dives
- **Audit**: `audit_log` table no Postgres, exposto via `/api/v1/audit`.
- **Loki**: cross-cluster gap (CLAUDE.md). Pode falhar — vira gap explícito.

#### Tasks
1. `loopzero audit list --action deploy --json | jq` filtrar pelo build_id.
2. `loopzero logs monorepo-turbo --app api --since 5m`.
3. Se Loki cross-cluster broken, marcar como gap mas continuar.

#### TDD
```
N/A.
VERIFY: loopzero audit list --action deploy --json | jq -e --arg bid "$BUILD_ID" '.entries[] | select(.metadata.build_id == $bid) | length > 0'
```

#### Acceptance Criteria
- [ ] Audit entry presente
- [ ] Logs query exit 0 (ou marca como gap conhecido)

#### DoD
- [ ] Audit confirmado
- [ ] Loki gap documented se aplicável

---

## Phase 2: Coverage extensions (secrets / postgres / workers / static / domains)

**Objective:** estender cobertura para cenários que monorepo-turbo não cobre, criando templates novos no `loopzero-stacks` (ADR D2) ou usando templates `*-with-addons` existentes.

### T2.1 — Secrets flow: `loopzero secret set/list/unset`

#### Objective
Validar fluxo completo de secrets: criar, listar, usar em app, remover, garantir que ESO sincroniza para namespace tenant.

#### Evidence
Spec §9 (Secrets) tem 5 comandos. Iteração anterior nunca testou. AppSecret controller não implementado (CLAUDE.md), mas ESO funciona direto.

#### Files to edit
```
infra/tests/dogfood/secrets-flow.sh — (NEW)
docs/dogfood/2026-04-29-completo.md — Phase 2.1 section
```

#### Deep Dives
- **Template alvo**: estender `monorepo-turbo` adicionando `apps/api/src/index.js` lê `process.env.SECRET_PING` e expõe `/secret-check`. Idealmente PR upstream em loopzero-stacks (`monorepo-turbo-with-secret`).
- **Comandos a exercitar**:
  - `loopzero secret set monorepo-turbo SECRET_PING=hello-world --env production`
  - `loopzero secret list monorepo-turbo --env production --json`
  - `loopzero secret unset monorepo-turbo SECRET_PING --env production`
- **Expected behavior**: ESO `ExternalSecret` criado, sincronizado para `Secret` tenant ns, montado em pod via env. Nova versão do app deve ser deployada.

#### Tasks
1. Estender app api com endpoint `/secret-check` retornando `process.env.SECRET_PING`.
2. PR contra loopzero-stacks com template `monorepo-turbo-with-secret`.
3. Run `loopzero secret set` → `loopzero deploy` → `curl /secret-check` retorna `hello-world`.
4. Run `loopzero secret unset` → `loopzero deploy` → `curl /secret-check` retorna `undefined`.

#### TDD
```
RED: test_secret_set_creates_externalsecret() — kubectl get externalsecret retorna 1 entry
RED: test_secret_visible_in_pod_env() — kubectl exec pod -- env | grep SECRET_PING == "hello-world"
RED: test_secret_unset_removes_from_pod() — após unset, env não tem SECRET_PING
GREEN: Implementar comandos de teste em secrets-flow.sh
REFACTOR: None expected
VERIFY: bash infra/tests/dogfood/secrets-flow.sh
```

#### Acceptance Criteria
- [ ] `loopzero secret set` exit 0, ExternalSecret criado
- [ ] `loopzero secret list --json` retorna o secret
- [ ] App vê `SECRET_PING` em env
- [ ] `loopzero secret unset` remove
- [ ] Schema do `--json` validado contra spec §0.3

#### DoD
- [ ] Script comitado
- [ ] PR contra loopzero-stacks aberto (link no relatório)
- [ ] Phase 2.1 do relatório completa

### T2.2 — Worker apps (`type: worker`)

#### Objective
Validar que `type: worker` funciona — pod sem IngressRoute, sem URL, mas Running. Idealmente com BullMQ + Redis para realismo.

#### Evidence
Spec §3 menciona `type: worker`. CLAUDE.md `BuildAppRelease` retorna `URL=""` para workers — dead branch sem teste.

#### Files to edit
```
infra/tests/dogfood/worker-app.sh — (NEW)
```

#### Deep Dives
- **Template**: PR upstream `loopzero-stacks/examples/worker-bullmq` com api Express + worker Node + Redis.
- **Pod expectations**: deployment com `replicas: 1`, sem Service `ClusterIP`, sem IngressRoute, sem Certificate.
- **Validation**: pod logs mostram worker processando.

#### Tasks
1. PR upstream com template worker.
2. `loopzero deploy` no template.
3. Validar pod worker Running.
4. Validar pod worker NÃO tem IngressRoute associada.
5. Validar `loopzero logs --app worker` retorna logs do BullMQ.

#### TDD
```
RED: test_worker_pod_running()
RED: test_worker_has_no_ingressroute()
GREEN: Implementar checks
REFACTOR: None
VERIFY: bash infra/tests/dogfood/worker-app.sh
```

#### Acceptance Criteria
- [ ] Worker pod Running
- [ ] Sem IngressRoute para worker
- [ ] Logs visíveis via `loopzero logs`

#### DoD
- [ ] Template upstream proposto
- [ ] Phase 2.2 do relatório completa

### T2.3 — Postgres + Redis dependencies

#### Objective
Validar app que precisa de DATABASE_URL + REDIS_URL — LoopZero provê via secrets, app conecta.

#### Evidence
LoopZero é PaaS. Apps reais precisam de DB. Sem este teste, gap existencial.

#### Files to edit
```
infra/tests/dogfood/postgres-redis-flow.sh — (NEW)
```

#### Deep Dives
- **Postgres provisioning**: LoopZero não provisiona Postgres por app (decisão de produto). Em standalone-multi tem Postgres do tenant compartilhado? Verificar. Se não, secret aponta para Postgres externo.
- **Redis**: similar.
- **Template**: PR upstream `examples/node-with-pg-redis`.

#### Tasks
1. Investigar como LoopZero provisiona/conecta apps a Postgres/Redis em produção.
2. PR template upstream se conveniente.
3. `loopzero deploy`, `loopzero secret set DATABASE_URL=..., REDIS_URL=...`.
4. App conecta, faz query, retorna 200.

#### TDD
```
RED: test_app_connects_to_postgres()
RED: test_app_connects_to_redis()
GREEN: Implementar
REFACTOR: None
VERIFY: bash infra/tests/dogfood/postgres-redis-flow.sh
```

#### Acceptance Criteria
- [ ] App conecta a Postgres
- [ ] App conecta a Redis
- [ ] Queries retornam dados

#### DoD
- [ ] Investigação documentada
- [ ] Template upstream se aplicável
- [ ] Phase 2.3 completa

### T2.4 — Custom domain via CF Zone Mgmt

#### Objective
Validar `loopzero domain add <domain>` → CF Zone criada → status `pending_ns` → DNS → `provisioning` → `active`.

#### Evidence
Spec §7 (Custom Domains v2). E2E nunca dogfooded. CLAUDE.md menciona Sprint 4 ✅ mas com caveat.

#### Files to edit
```
infra/tests/dogfood/custom-domain-flow.sh — (NEW)
```

#### Deep Dives
- **Limitação local**: CF Zone Mgmt exige domínio real registrado. Em standalone-multi, mockable? Verificar se existe modo dev.
- **Plan B**: documentar como gap se requer DNS público real.

#### Tasks
1. Verificar se CF API mock disponível em standalone-multi.
2. Se sim: rodar `loopzero domain add`, verificar transitions.
3. Se não: documentar gap em relatório, marcar como "validar em DOKS prod".

#### Acceptance Criteria
- [ ] OU validation completa OU gap documentado claro

#### DoD
- [ ] Phase 2.4 completa

### T2.5 — Pure static frontend (Vite SPA) via CF Worker + S3 + KV

#### Objective
Template puramente static (sem Next.js standalone) que vai pelo CF Worker path.

#### Evidence
Sprint 10 ✅ Static Frontend Delivery. Path nunca dogfooded ponta-a-ponta.

#### Files to edit
```
infra/tests/dogfood/static-frontend-flow.sh — (NEW)
```

#### Deep Dives
- **Template upstream**: `loopzero-stacks/examples/vite-react-static` (verify exists).
- **Build path**: Vite build → `dist/` → S3 prefix → CF KV entry.
- **Validation**: GET via CF Worker (em standalone-multi, é o Worker rodando local? Verificar).

#### Tasks
1. PR template Vite SPA upstream se ausente.
2. `loopzero deploy`, observar build pipeline incluir step `upload-to-s3`.
3. Validar entry em CF KV (em local, mock).
4. `curl` via CF Worker host retorna HTML estático.

#### Acceptance Criteria
- [ ] Build pipeline upload-to-s3 step Succeeded
- [ ] Entry CF KV existe
- [ ] HTTP 200 retorna HTML

#### DoD
- [ ] Phase 2.5 completa

---

## Phase 3: Lifecycle operations

**Objective:** exercitar todos os comandos de spec §3 (deploy/status/logs/rollback/promote/cancel/scale) e §4 (project lifecycle).

### T3.1 — Concurrent deploy rejection (1×202 + 1×409)

#### Objective
Disparar 2 `loopzero deploy` simultâneos, validar 1 retorna 202 (accepted) e outro 409 (conflict).

#### Evidence
Rule 5 (concurrent rejection). CLAUDE.md: "concurrent deploy rejection (1×202+1×409)". E2E pass mas com plan caps fix.

#### Tasks
1. `(loopzero deploy --json &) ; loopzero deploy --json` racing.
2. Capturar exit codes e JSON outputs.
3. Validar 1×202+1×409 ou ambos 200 (idempotency window).

#### Acceptance Criteria
- [ ] Validação semântica clara: rejection ou idempotency

### T3.2 — `loopzero logs <project> --app <app> --since 5m`

#### Objective
Validar logs via Loki cross-cluster.

#### Evidence
CLAUDE.md: gap conhecido (Loki cross-cluster streaming infra gap, não código).

#### Tasks
1. Run command, capturar output.
2. Se vazio mas pod logs `kubectl logs` mostra dados: gap confirmed.

#### Acceptance Criteria
- [ ] OU logs aparecem OU gap documentado

### T3.3 — `loopzero status <env>` e `loopzero project inspect`

#### Tasks
1. Run ambos.
2. Validar outputs cobrem builds, releases, services.

#### Acceptance Criteria
- [ ] Outputs ricos, sem internal terms

### T3.4 — `loopzero scale` (Pro plan caps)

#### Evidence
CLAUDE.md: "scale: 99% confidence", mas DB persistence stub (returns 202 acknowledged). Test via loopzero.yaml + deploy real.

#### Tasks
1. `loopzero scale monorepo-turbo --app web --replicas 5 --env production`.
2. Esperar 5 réplicas no deploy.
3. Tentar `--replicas 11` → expect 4xx (Pro cap=5/app, mas talvez plan cap aplica diferente).

#### Acceptance Criteria
- [ ] HPA refletiu mudança
- [ ] Plan cap enforced

### T3.5 — `loopzero promote staging production`

#### Tasks
1. Deploy em staging primeiro.
2. `loopzero promote monorepo-turbo --from staging --to production`.
3. Validar nova release production aponta para mesmo build_id.

#### Acceptance Criteria
- [ ] Promotion flow completa
- [ ] State machine: validating → committing → syncing → completed

### T3.6 — `loopzero rollback`

#### Tasks
1. Deploy v1, deploy v2.
2. `loopzero rollback --to <release-id-v1>`.
3. Validar release v1 reactivated, v2 SUPERSEDED.

#### Acceptance Criteria
- [ ] Rollback completo
- [ ] HTTP 200 ainda OK pós-rollback

### T3.7 — `loopzero cancel <build-id>`

#### Tasks
1. Disparar deploy lento (Dockerfile com `RUN sleep 300`).
2. Imediatamente `loopzero cancel <build-id>`.
3. Validar workflow cancelado em <60s.

#### Acceptance Criteria
- [ ] Workflow status Cancelled
- [ ] CANCELLING→CANCELLED state transition

### T3.8 — `loopzero project delete` (Prova de Morte)

#### Objective
ADR D4: namespace deleted, ArgoCD app removed, S3 prefix limpo, em <5min.

#### Tasks
1. `loopzero project delete monorepo-turbo --confirm`.
2. Polling: namespace `loopzero-tn-e2etest-production` deve sumir.
3. ArgoCD app `tn-e2etest-production` deve sumir.
4. `mc ls minio/loopzero-builds/tn-e2etest/source/` não tem bundles do project.

#### Acceptance Criteria
- [ ] Cleanup completo em ≤5min
- [ ] Sem orphans

#### DoD
- [ ] Prova de Morte registrada em relatório

---

## Phase 4: GitHub App auto-deploy

**Objective:** validar fluxo `git push → webhook → build`.

### T4.1 — Configurar GitHub App webhook + secret

#### Tasks
1. Validar `loopzero github connect <project> <repo>` registra webhook.
2. Validar webhook secret persistido em DB.

#### Acceptance Criteria
- [ ] Webhook URL acessível externamente OU port-forward documentado

### T4.2 — Push commit → build trigger automático

#### Evidence
CLAUDE.md: "GitHub App auto-deploy (~4min)". Validation Sprint anterior.

#### Tasks
1. Push commit em `getloopscale/loopzero-repotest` (memory).
2. Aguardar webhook chegar.
3. Validar build criado automaticamente.

#### Acceptance Criteria
- [ ] Build started <60s após push
- [ ] HTTP 200 final em <4min

---

## Phase 5: Preview environments

**Objective:** validar Sprint 5 ✅ Preview Environments end-to-end.

### T5.1 — Open PR → preview env created

#### Tasks
1. Push branch + open PR via gh.
2. Validar PreviewEnvironment CR criada.
3. Validar namespace `preview-{tenant}-{project}-{prnumber}` criado.

### T5.2 — Isolation primitives

#### Tasks
1. NetworkPolicy deny-all/allow-ingress/allow-egress aplicada.
2. ResourceQuota e LimitRange aplicados.
3. Cert-manager Certificate emitido.

#### Acceptance Criteria
- [ ] Isolation completa
- [ ] HTTP 200 no host preview

### T5.3 — TTL expiry → cleanup

#### Tasks
1. Force TTL expiry via patch annotation OR aguardar TTL.
2. Validar cleanup retry pattern.
3. Namespace removido.

---

## Phase 6: Contracts + Rules + Failure Modes

**Objective:** validar JSON schema, exit codes, Rule 3 (internal terms), failure modes spec §12.

### T6.1 — JSON contract validation (todos os comandos com `--json`)

#### Files to edit
```
infra/tests/dogfood/json-schema-validation.sh — (NEW)
```

#### Tasks
1. Script roda `loopzero <cmd> --json` para todos os 44 comandos.
2. Para cada, valida output contra spec §0.3 schema.

#### TDD
```
RED:    test_each_cmd_emits_valid_json() — for each cmd, run, jq parse, schema match
GREEN:  Implementar harness
REFACTOR: None
```

#### Acceptance Criteria
- [ ] 44/44 comandos emitem JSON válido OU gap documentado por comando

### T6.2 — Exit codes (0/1/2/3/4/5 only) — fork-based regression

#### Evidence
F-02 anterior: `loopzero deploy --json` exit 0 com `exit_code:4` no JSON. Cobertura precisa ser fork-based (não in-process).

#### Files to edit
```
infra/tests/dogfood/exit-code-regression.sh — (NEW)
```

#### Tasks
1. Para cada cenário (success, usage error, build fail, deploy fail, auth, partial), invocar comando, capturar `$?`.
2. Validar exit code in {0,1,2,3,4,5}.

#### Acceptance Criteria
- [ ] Zero outros exit codes
- [ ] JSON `exit_code` field bate com process exit

### T6.3 — Rule 3 internal terms grep

#### Tasks
1. Para output de cada comando (stdout + stderr), grep `ArgoCD|argocd|GitOps|gitops-push|CRD|tenant_id|advisory lock|pg_advisory|Gitea|MinIO|kubebuilder|envtest|Kaniko`.
2. Zero matches required.

#### Acceptance Criteria
- [ ] Zero internal terms

### T6.4 — Failure modes spec §12

#### Tasks
1. Invalid token → exit 4.
2. Quota exceeded (3rd project on Pro) → 403 + product message.
3. Build failure (broken Dockerfile) → exit 2.
4. Deploy failure (manifest invalid) → exit 3.
5. Source upload silent-success regression: validar que upload error AGORA é 5xx (não 200 silencioso).

#### Acceptance Criteria
- [ ] 5/5 failure modes correctly handled

---

## Phase 7: Final report + matriz

**Objective:** consolidar findings em `docs/dogfood/2026-04-29-completo.md`, matriz coverage, lista de PRs upstream.

### T7.1 — Matriz de cobertura por comando + funcionalidade

#### Files to edit
```
docs/dogfood/2026-04-29-completo.md — finalizar com matriz
```

#### Tasks
1. Tabela 44 comandos × {coberto, gap conhecido, falha}.
2. Tabela 14 seções funcionais × resultado.
3. Lista de bugs encontrados (com severity).
4. Lista de PRs upstream em loopzero-stacks.

#### Acceptance Criteria
- [ ] Matriz completa, sem N/A vazio
- [ ] Cada gap tem owner/follow-up

### T7.2 — Recomendação de prontidão

#### Tasks
1. Concluir GO/NO-GO para release.
2. Lista de blockers críticos.
3. Lista de sintomas tolerable mas que precisam fix antes de Series A.

#### DoD
- [ ] Veredito honesto, com evidências

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Pre-flight cluster sanity | T0.1-T0.5 | preflight.sh idempotente, 5 checks |
| 2 | Build pipeline multi-app + cosign + digest | T1.3-T1.5 | monorepo-turbo deploy + manifest grep |
| 3 | Pod admission via VAPs (digest, allowlist, latest, limits) | T1.6 | kubectl events check |
| 4 | Prova de Vida HTTP 200 + TLS | T1.7 | curl --resolve + openssl s_client |
| 5 | Audit log entry | T1.8 | loopzero audit list --json |
| 6 | Loki cross-cluster logs | T1.8, T3.2 | tolerar gap se infra-only |
| 7 | Secrets flow (set/list/unset) | T2.1 | secrets-flow.sh + ESO check |
| 8 | Worker apps (type=worker) | T2.2 | template upstream + pod check |
| 9 | Postgres + Redis dependencies | T2.3 | template + connection test |
| 10 | Custom domains v2 (CF Zone) | T2.4 | flow ou gap se DNS limit |
| 11 | Static frontend (Vite SPA) via CF Worker | T2.5 | template upstream + S3+KV check |
| 12 | Concurrent deploy rejection | T3.1 | racing test |
| 13 | loopzero status / project inspect | T3.3 | output grep |
| 14 | loopzero scale + plan caps | T3.4 | replicas check + over-cap rejection |
| 15 | loopzero promote | T3.5 | release lineage check |
| 16 | loopzero rollback | T3.6 | reactivation check |
| 17 | loopzero cancel | T3.7 | workflow Cancelled |
| 18 | loopzero project delete (Prova de Morte) | T3.8 | cleanup verification |
| 19 | GitHub App auto-deploy | T4.1-T4.2 | webhook + ~4min SLA |
| 20 | Preview environments lifecycle | T5.1-T5.3 | PR + isolation + TTL |
| 21 | JSON schema stability (44 cmds) | T6.1 | harness against spec §0.3 |
| 22 | Exit code contract (fork-based) | T6.2 | $? in {0..5} |
| 23 | Rule 3 internal terms | T6.3 | grep zero matches |
| 24 | Failure modes spec §12 | T6.4 | 5 scenarios |
| 25 | Source upload silent-success regression | T6.4 | positive test |
| 26 | Bugs B1-B18 + 5 desta sessão | All phases | regression checks integradas |
| 27 | Final report + GO/NO-GO | T7.1-T7.2 | docs/dogfood/2026-04-29-completo.md |

**Coverage: 27/27 requirements covered (100%).**

## Global Definition of Done

- [ ] All 7 phases completed
- [ ] All TDD red tests green
- [ ] preflight.sh shellcheck clean, idempotente, <30s
- [ ] monorepo-turbo: 2 apps HTTP 200 + TLS válido
- [ ] Phase 2 cobre 5 cenários (secrets, workers, pg+redis, domain, static) OU gap explícito
- [ ] Phase 3: 8 lifecycle ops validadas
- [ ] Phase 4: GitHub App ~4min E2E
- [ ] Phase 5: preview environments completo
- [ ] Phase 6: 44 cmds × JSON schema, exit codes, Rule 3, 5 failure modes
- [ ] Phase 7: relatório `docs/dogfood/2026-04-29-completo.md` publicado
- [ ] Matriz 27/27 com cada item categorizado: ✅ green | ⚠️ gap | ❌ falha
- [ ] Cada falha gera GitHub issue
- [ ] Cada gap upstream gera PR contra `getloopscale/loopzero-stacks`
- [ ] CHANGELOG.md atualizado com `## [Unreleased] / Fixed` para cada bug encontrado e corrigido
- [ ] Pre-release blockers identificados com claridade
- [ ] Veredito GO/NO-GO honesto, com evidências

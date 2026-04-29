# Plan: Bootstrap Integrity Fixes (L1/L2/L3)

> **Version 1.0** — Este plano corrige 6 problemas CRITICAL, 16 HIGH e issues selecionados de severidade MEDIUM descobertos na revisão de integridade do bootstrap layered (L1 Pulumi → L2 Helmfile → L3 ArgoCD). O objetivo é garantir que `loopzero platform install` funcione de ponta a ponta — tanto em fresh install (standalone-multi/k3d) quanto multi-cluster (DigitalOcean) — sem intervenção manual. Sem estes fixes, a instalação falha silenciosamente ou bloqueia em health gates.

## Context

O commit `26e116e0` reestruturou o bootstrap do LoopZero em 3 camadas (L1 Pulumi cloud-only, L2 Helmfile K8s, L3 ArgoCD Day-2). O commit `444b2129` adicionou 4 novos Helm charts para operators. O commit `97930a56` removeu subcharts `.tgz` empacotados.

Uma revisão completa com 4 agentes especializados (Pulumi, Helmfile, Charts, CLI) identificou:
- **6 CRITICAL**: módulos implementados mas nunca chamados, namespace race condition, health gates quebrados
- **16 HIGH**: dual chart system, RBAC wildcard, retry path quebrado, condições standalone-multi incorretas
- **23 MEDIUM**: secrets handling, portabilidade, dead code

O padrão sistêmico é **"implemented but not wired"** — cada módulo funciona isoladamente mas a integração entre camadas está incompleta.

**Evidência:** E2E passou em k3d porque rodou com estado pré-existente (namespaces e secrets de runs anteriores), mascarando os problemas de first-install.

## Objective

**Done = `loopzero platform install -f values.yaml --provider k3d` funciona em cluster limpo sem intervenção manual, E2E deploy produz HTTP 200.**

Metas específicas:
1. Zero CRITICAL findings
2. `preflight.ts` e `health-checks.ts` integrados no flow
3. Umbrella chart `loopzero-platform` faz `helm lint` sem erro
4. `generate-secrets.sh` idempotente em first-install (namespace argocd criado antes do Secret)
5. Health checks adaptáveis por profile (standalone/standalone-multi/dev/prod)
6. Dual chart system resolvido (decisão: `infra/helm/` canônico para helmfile, `charts/` canônico para OCI registry)

## ADRs

### D1 — `infra/helm/` é canônico para Helmfile, `charts/` para OCI/ArgoCD

**Decision:** Os dois sistemas de charts coexistem com propósitos distintos:
- `infra/helm/` → usado pelo Helmfile L2 para bootstrap direto (instalação inicial)
- `charts/` → publicados no OCI registry, usados pelo ArgoCD L3 para Day-2 GitOps

**Rationale:** O Helmfile precisa de charts locais simples com valores inline. O ArgoCD precisa de charts versionados em registry. Unificar forçaria ou charts simples demais para ArgoCD ou complexos demais para Helmfile. A separação já é o padrão de facto (helmfiles apontam para `../helm/`, CI publica `charts/`).

**Consequences:** Mudanças em templates devem ser propagadas para ambos os caminhos. Documentar qual é qual no CLAUDE.md.

### D2 — `loopzero-tenant-controller` é o chart monolítico ativo; `loopzero-tenant-operator` é forward-looking

**Decision:** O binário é monolítico (`operators/cmd/main.go` registra todos os controllers). `infra/helm/tenant-controller` é o chart que o Helmfile usa. Os charts `loopzero-*-operator` em `charts/` são preparação para split futuro (Sprint 9+).

**Rationale:** O `main.go` confirma: todos os controllers (Tenant, Environment, Application, Exposure, Preview, PlatformBootstrap, ClusterCredential, GitRepositoryBinding) rodam no mesmo binário. Não há split de binários ainda.

**Consequences:** Remover `loopzero-tenant-controller` do umbrella `loopzero-platform` (pois umbrella é para ArgoCD L3 onde o chart certo seria `loopzero-tenant-operator`). O chart `infra/helm/tenant-controller` mantém o RBAC broad porque é monolítico.

### D3 — Health checks devem ser profile-aware

**Decision:** `health-check.sh` recebe uma variável `LOOPZERO_PROFILE` (standalone/standalone-multi/dev/prod) e ajusta quais checks executar.

**Rationale:** Health checks hardcoded falham em perfis onde componentes específicos não existem (cloudflared em standalone-multi, LoadBalancer em k3d, DNS público em standalone). Checks devem ser adaptativos ao ambiente.

**Consequences:** Adiciona complexidade ao script mas elimina 2 CRITICALs e 1 HIGH.

### D4 — Preflight e health gates são obrigatórios no install flow

**Decision:** `platformInstall()` DEVE chamar `runPreflight()` no Step 2 e `runHealthCheck()` após cada L2 cluster role sync no Step 4.

**Rationale:** Sem preflight, installs falham 15+ min depois com erros crípticos. Sem health gates, L2 inicia antes dos nodes estarem ready.

**Consequences:** Install fica mais lento (~30s por health gate) mas muito mais confiável.

### D5 — `--skip-infra` deve preservar outputs do último Pulumi run

**Decision:** Quando `--skip-infra` é usado, o CLI lê outputs do cache (`~/.loopzero/pulumi-outputs.json`) salvo no run anterior.

**Rationale:** O flag existe para retry de L2 após falha, mas atualmente pula a leitura de outputs, quebrando todo o L2 que depende de credenciais. Cache local resolve o problema sem re-executar Pulumi.

**Consequences:** Adiciona arquivo de estado local (`~/.loopzero/pulumi-outputs.json`).

## Dependency Graph

```
Phase 0 (ADR + cleanup)
    │
    ├──▶ Phase 1 (Helmfile fixes) ──▶ Phase 3 (Integration tests)
    │
    ├──▶ Phase 2 (CLI wiring)     ──▶ Phase 3 (Integration tests)
    │
    └──▶ Phase 4 (Charts fixes)       (parallel with 1 & 2)

Phase 3 depends on Phase 1 AND Phase 2 (both must complete)
Phase 4 is independent (can run in parallel with 1 & 2)
```

---

## Phase 0: ADR + Cleanup

**Objective:** Documentar decisões e limpar dead code antes dos fixes.

### T0.1 — Documentar dual chart system no CLAUDE.md

#### Objective
Adicionar seção no CLAUDE.md explicando `infra/helm/` vs `charts/` para evitar confusão futura.

#### Evidence
Review finding HC-H5: dois sistemas de charts para mesmos componentes sem documentação de qual usar quando.

#### Files to edit
```
CLAUDE.md — adicionar seção "Helm Chart Dual System" na área de Architecture Overview
```

#### Deep file dependency analysis
- `CLAUDE.md`: documento de referência lido por todos os agentes. Adicionar seção não quebra nada.

#### Deep Dives
Nenhum aspecto não-óbvio.

#### Tasks
1. Adicionar seção "Helm Chart Dual System" após "CRDs (10 total)"
2. Explicar: `infra/helm/` para L2 Helmfile bootstrap, `charts/` para OCI registry + ArgoCD L3

#### TDD
```
RED:     Não aplicável (documentação)
GREEN:   N/A
REFACTOR: N/A
VERIFY:  Leitura visual do CLAUDE.md
```

#### Acceptance Criteria
- [ ] CLAUDE.md contém seção "Helm Chart Dual System"
- [ ] Seção explica quando usar cada sistema

#### DoD
- [ ] CLAUDE.md atualizado
- [ ] Coerente com ADR D1

---

### T0.2 — Remover `loopzero-tenant-controller` das dependencies do umbrella chart

#### Objective
Fix CRITICAL HC-C1: `loopzero-platform` referencia `loopzero-tenant-controller` sem declará-lo como dependency.

#### Evidence
`helm lint charts/loopzero-platform --with-subcharts` falha com nil pointer em NOTES.txt:7.

#### Files to edit
```
charts/loopzero-platform/templates/NOTES.txt — remover referência a loopzero-tenant-controller
charts/loopzero-platform/values.yaml — remover seção loopzero-tenant-controller se existir
```

#### Deep file dependency analysis
- `NOTES.txt`: template renderizado após `helm install`. Referência a dependency inexistente causa nil pointer.
- `values.yaml`: valores para subchart inexistente são ignorados pelo Helm mas confundem leitores.
- Profiles (`dev.yaml`, `prod.yaml`) podem precisar de limpeza.

#### Tasks
1. Ler `charts/loopzero-platform/templates/NOTES.txt` e remover linhas referenciando `loopzero-tenant-controller`
2. Ler `charts/loopzero-platform/values.yaml` e remover seção `loopzero-tenant-controller:` se existir
3. Verificar `charts/loopzero-platform/profiles/dev.yaml` e `prod.yaml` para referências
4. Rodar `helm lint charts/loopzero-platform`

#### TDD
```
RED:     helm lint charts/loopzero-platform --with-subcharts → falha
GREEN:   Remover referências → helm lint passa
REFACTOR: None expected
VERIFY:  helm lint charts/loopzero-platform --with-subcharts && helm template test charts/loopzero-platform
```

#### Acceptance Criteria
- [ ] `helm lint charts/loopzero-platform --with-subcharts` passa
- [ ] `helm template test charts/loopzero-platform` renderiza sem erro
- [ ] Nenhuma referência a `loopzero-tenant-controller` em templates ou values do umbrella

#### DoD
- [ ] Helm lint clean
- [ ] Template render clean

---

## Phase 1: Helmfile Fixes (CRITICALs + HIGHs)

**Objective:** Corrigir os 3 CRITICALs e 4 HIGHs do Helmfile para que `helmfile sync` funcione em first-install.

### T1.1 — Fix namespace argocd race condition no generate-secrets.sh

#### Objective
Fix CRITICAL HF-C1: `generate-secrets.sh` cria Secret `gitea-credentials-template` no namespace `argocd` que ainda não existe no primeiro install.

#### Evidence
Linha 178: `kubectl get namespace argocd >/dev/null 2>&1 &&` — guarda silenciosa faz o bloco inteiro ser no-op. ArgoCD inicia sem credenciais do Gitea → tenant deploys ficam `sync=Unknown`.

#### Files to edit
```
infra/helmfile/scripts/generate-secrets.sh — substituir guard por ensure_namespace argocd
```

#### Deep file dependency analysis
- `generate-secrets.sh`: roda como pre-sync antes do `helmfile sync`. A função `ensure_namespace` (linha 54-57) já existe e é idempotente (`kubectl create ns --dry-run=client -o yaml | kubectl apply`). Usar ela antes do Secret garante que o namespace existe.
- Downstream: ArgoCD helm chart também cria o namespace, mas como `helmfile sync` roda DEPOIS do pre-sync, não há conflito (ensure_namespace é idempotente).

#### Deep Dives
- A função `ensure_namespace` usa `--dry-run=client -o yaml | kubectl apply` que é idempotente
- Se ArgoCD chart tem `createNamespace: true`, a dupla criação é harmless
- O Secret precisa existir ANTES do ArgoCD pod iniciar para que o repo-creds scan encontre as credenciais

#### Tasks
1. Na linha 178, substituir `kubectl get namespace argocd >/dev/null 2>&1 &&` por `ensure_namespace argocd`
2. Mover o `cat <<EOF | kubectl apply` para dentro de uma chamada sem guarda condicional
3. Testar: deletar namespace argocd, rodar script, verificar que namespace e Secret existem

#### TDD
```
RED:     test-generate-secrets.sh: "argocd Secret created on fresh cluster" — falha com guard
GREEN:   Substituir guard por ensure_namespace — Secret criado
REFACTOR: None expected
VERIFY:  bash infra/helmfile/scripts/test_helpers/test-generate-secrets.sh
```

#### Acceptance Criteria
- [ ] `ensure_namespace argocd` chamado antes de criar o Secret
- [ ] Script é idempotente (rodar 2x não falha)
- [ ] Secret `gitea-credentials-template` existe no namespace `argocd` após execução

#### DoD
- [ ] Script modificado
- [ ] Teste manual em cluster limpo confirma Secret criado

---

### T1.2 — Health checks profile-aware (cloudflared, LoadBalancer, DNS)

#### Objective
Fix CRITICALs HF-C2 e HF-C3 + HIGH HF-H2: health checks devem adaptar-se ao profile.

#### Evidence
- HF-C2: `runtime` check exige `cloudflared readyReplicas >= 1` mas standalone-multi seta `replicaCount: 0`
- HF-C3: `build` check assume `LoadBalancer.ingress[0].ip` que não existe em k3d
- HF-H2: `mgmt` check usa URLs públicas (`https://git.${DOMAIN}`) que não resolvem em standalone

#### Files to edit
```
infra/helmfile/scripts/health-check.sh — adicionar LOOPZERO_PROFILE e condicionar checks
```

#### Deep file dependency analysis
- `health-check.sh`: chamado entre fases do install (quando wired — Phase 2). Recebe `role` como argumento e `DOMAIN` como env var.
- Adicionar `LOOPZERO_PROFILE` (env var setada pelo CLI ou helmfile environment) para:
  - Skip cloudflared check quando profile contém "standalone"
  - Usar ClusterIP/kubectl port-forward em vez de LoadBalancer IP para build check em standalone
  - Usar in-cluster service URLs em vez de DNS público para mgmt check em standalone

#### Deep Dives
- **cloudflared**: em standalone-multi, cloudflared não é deployado (replicaCount: 0). Check deve ser skipped.
- **LoadBalancer**: k3d não atribui IPs a LoadBalancer services por padrão. Usar `kubectl get svc -o jsonpath='{.spec.clusterIP}'` como fallback.
- **DNS**: em standalone, `git.loopzero.local` não resolve sem /etc/hosts. Usar `kubectl get svc gitea-http -n loopzero-system -o jsonpath='{.spec.clusterIP}'` e curl direto.

#### Tasks
1. Ler `LOOPZERO_PROFILE` do env (default: inferir de `$ENV` se não setado)
2. Na seção `runtime`: adicionar guard `if [[ "$LOOPZERO_PROFILE" != standalone* ]]` antes do check cloudflared
3. Na seção `build`: adicionar fallback para ClusterIP quando LoadBalancer IP está vazio
4. Na seção `mgmt`: usar in-cluster URLs quando `LOOPZERO_PROFILE` começa com "standalone"
5. Testar em k3d com profile standalone-multi

#### TDD
```
RED:     health-check.sh runtime (standalone-multi) → falha no cloudflared check
GREEN:   Adicionar profile guard → check skipped, saída 0
RED:     health-check.sh build (standalone) → falha no LoadBalancer IP
GREEN:   Fallback para ClusterIP → check passa
REFACTOR: Extrair function is_standalone() para reuso
VERIFY:  LOOPZERO_PROFILE=standalone-multi bash infra/helmfile/scripts/health-check.sh runtime
```

#### Acceptance Criteria
- [ ] `health-check.sh runtime` passa em standalone-multi
- [ ] `health-check.sh build` passa em k3d sem LoadBalancer
- [ ] `health-check.sh mgmt` passa em standalone sem DNS público
- [ ] Checks continuam funcionando em dev/prod (regressão zero)

#### DoD
- [ ] Script modificado
- [ ] Testado em k3d standalone-multi
- [ ] Nenhum check falso-positivo em nenhum profile

---

### T1.3 — Fix condição standalone-multi para NFS/Gitea HA

#### Objective
Fix HIGH HF-H1: standalone-multi recebe NFS + Gitea HA por bug na condição `ne "standalone"`.

#### Evidence
`mgmt.helmfile.yaml.gotmpl:185,208`: `ne .Environment.Name "standalone"` é true para "standalone-multi", causando NFS e Gitea HA (2 replicas, RWX PVC) serem ativados. NFS requer kernel module `nfsd` que pode não estar no host.

#### Files to edit
```
infra/helmfile/mgmt.helmfile.yaml.gotmpl — corrigir condição para testar "dev" e "prod" explicitamente
```

#### Deep file dependency analysis
- `mgmt.helmfile.yaml.gotmpl`: arquivo principal do mgmt cluster. As condições controlam quais releases são instaladas.
- NFS Server Provisioner: só faz sentido em cloud multi-cluster onde Gitea precisa de RWX para HA.
- Gitea HA (2 replicas): só faz sentido com RWX PVC (NFS).

#### Deep Dives
- Condição atual: `{{ if ne .Environment.Name "standalone" }}` — exclui só "standalone", não "standalone-multi"
- Fix: usar `{{ if or (eq .Environment.Name "dev") (eq .Environment.Name "prod") }}` (explícito)
- Alternativa: `{{ if not (hasPrefix .Environment.Name "standalone") }}` — Go template não tem hasPrefix, mas pode usar `contains`

#### Tasks
1. Identificar todas as condições `ne .Environment.Name "standalone"` no arquivo
2. Substituir por `{{ if or (eq .Environment.Name "dev") (eq .Environment.Name "prod") }}`
3. Verificar que standalone-multi não recebe NFS nem Gitea HA
4. Verificar que dev e prod continuam recebendo NFS e Gitea HA

#### TDD
```
RED:     helmfile -e standalone-multi template | grep nfs → encontra NFS
GREEN:   Corrigir condição → NFS não presente em standalone-multi
REFACTOR: None expected
VERIFY:  helmfile -e standalone-multi -f infra/helmfile/mgmt.helmfile.yaml.gotmpl template 2>/dev/null | grep -c nfs
```

#### Acceptance Criteria
- [ ] standalone-multi não instala NFS Server Provisioner
- [ ] standalone-multi não instala Gitea com values-ha.yaml
- [ ] dev e prod continuam com NFS + Gitea HA
- [ ] standalone continua sem NFS + Gitea HA

#### DoD
- [ ] Condições corrigidas
- [ ] Template rendering verificado para os 4 environments

---

### T1.4 — Fix MinIO credentials em standalone e Docker bridge IP

#### Objective
Fix HIGH HF-H3 (MinIO credentials não criados em standalone) e HF-H4 (IP Docker bridge hardcoded).

#### Evidence
- HF-H3: `generate-secrets.sh:197-201` só cria `minio-credentials` quando `LOOPZERO_SPACES_KEY` está setado. Em standalone, MinIO roda in-cluster com credentials dos values.yaml.
- HF-H4: `runtime.helmfile.yaml.gotmpl:214` hardcoda `172.18.0.3` para MinIO docker bridge IP.

#### Files to edit
```
infra/helmfile/scripts/generate-secrets.sh — gerar minio-credentials com defaults quando LOOPZERO_SPACES_KEY ausente
infra/helmfile/runtime.helmfile.yaml.gotmpl — usar variável ou lookup dinâmico para MinIO IP
```

#### Deep file dependency analysis
- `generate-secrets.sh`: o Secret `minio-credentials` é usado pelo loopzero-api e build pipeline para acessar S3. Sem ele, uploads de bundles falham.
- `runtime.helmfile.yaml.gotmpl`: o IP hardcoded aponta para MinIO no docker network do k3d. Se a rede muda, o runtime não encontra o MinIO.

#### Tasks
1. Após a seção `LOOPZERO_SPACES_KEY` (linha 197), adicionar bloco `else` que cria `minio-credentials` com valores default do MinIO chart (`rootUser=minioadmin`, `rootPassword` do chart values)
2. Substituir IP hardcoded `172.18.0.3` por variável `{{ .Values.minio.endpoint | default "minio.loopzero-system.svc.cluster.local:9000" }}`
3. Verificar que loopzero-api consegue acessar MinIO em standalone

#### TDD
```
RED:     kubectl get secret minio-credentials -n loopzero-system (standalone, sem LOOPZERO_SPACES_KEY) → not found
GREEN:   Gerar com defaults → Secret existe
REFACTOR: None expected
VERIFY:  bash infra/helmfile/scripts/generate-secrets.sh standalone mgmt && kubectl get secret minio-credentials -n loopzero-system
```

#### Acceptance Criteria
- [ ] `minio-credentials` criado em standalone com valores default
- [ ] IP Docker bridge não hardcoded
- [ ] Funciona em dev/prod com LOOPZERO_SPACES_KEY (sem regressão)

#### DoD
- [ ] Script e helmfile modificados
- [ ] Secret verificável em cluster limpo

---

## Phase 2: CLI Wiring (CRITICALs)

**Objective:** Integrar `preflight.ts` e `health-checks.ts` no flow de `platformInstall()` e corrigir `--skip-infra`.

### T2.1 — Integrar preflight no install flow

#### Objective
Fix CRITICAL CLI-F01: `runPreflight()` nunca é chamado pelo install flow.

#### Evidence
`cli/src/installer/preflight.ts` contém 11 checks prontos (helmfile, helm, kubectl, cosign, disco, doctl, DO token, quota, CF token, CF zone, existing resources). Nenhum é invocado em `platform.ts`.

#### Files to edit
```
cli/src/commands/platform.ts — importar e chamar runPreflight() + formatPreflightResult() no Step 2
```

#### Deep file dependency analysis
- `platform.ts`: função `platformInstall()`, Step 2 (linhas 161-177) atualmente só checa `ensureHelmfileAvailable()` e `ensurePulumiAvailable()`.
- `preflight.ts`: `runPreflight(opts)` retorna `{ ok, checks, blockers, warnings }`. `formatPreflightResult()` printa os resultados.
- Não há conflito: os 2 checks inline atuais são redundantes com os do preflight (que são mais completos).

#### Tasks
1. Importar `runPreflight`, `formatPreflightResult` de `../installer/preflight`
2. No Step 2, substituir checks inline por chamada a `runPreflight({ provider: config.provider, cfAccountId: config.providerConfig?.cfAccountId, cfZoneId: config.providerConfig?.cfZoneId })`
3. Chamar `formatPreflightResult(result, writeTo)` para mostrar resultados
4. Se `!result.ok`, retornar exit code 1
5. Manter warnings como não-bloqueantes

#### TDD
```
RED:     test_preflight_called_on_install() — mock runPreflight, assert called with provider
GREEN:   Adicionar chamada no Step 2
REFACTOR: Remover checks inline redundantes (ensureHelmfileAvailable, ensurePulumiAvailable)
VERIFY:  cd cli && npm test -- --grep "platform install"
```

#### Acceptance Criteria
- [ ] `runPreflight()` chamado no Step 2 do install flow
- [ ] Blockers impedem o install (exit 1)
- [ ] Warnings não impedem (exibidos mas prossegue)
- [ ] Provider-specific checks ativados quando `--provider` passado

#### DoD
- [ ] Código integrado
- [ ] Teste unitário cobrindo chamada ao preflight
- [ ] Preflight blocker impede continuação

---

### T2.2 — Integrar health gates entre layers

#### Objective
Fix CRITICAL CLI-F02: `health-checks.ts` nunca é chamado entre camadas L1→L2 e entre cluster roles.

#### Evidence
`cli/src/installer/health-checks.ts` contém `runHealthCheck(role)` e `runFullHealthCheck()` que wrappam `health-check.sh`. Nenhum é invocado em `platform.ts`.

#### Files to edit
```
cli/src/commands/platform.ts — importar e chamar runHealthCheck() após cada helmfile sync no Step 4
```

#### Deep file dependency analysis
- `platform.ts`: Step 4 (linhas 219-267) itera `clusterRoles` e faz `helmfileSync()` para cada um. Health checks devem rodar após cada sync.
- `health-checks.ts`: `runHealthCheck(role, { domain, extraEnv })` wraps `health-check.sh`.
- Dependência: `health-check.sh` precisa ser profile-aware (T1.2) para funcionar em standalone.

#### Tasks
1. Importar `runHealthCheck` de `../installer/health-checks`
2. Após cada `helmfileSync()` sucesso no loop de Step 4, chamar `runHealthCheck(role, { domain: config.domain, extraEnv })`
3. Se health check falha, exibir erro e retornar exit code 3
4. Propagar `LOOPZERO_PROFILE` via extraEnv para que `health-check.sh` seja profile-aware
5. Em dry-run, skip health checks

#### TDD
```
RED:     test_health_check_called_after_sync() — mock runHealthCheck, assert called per role
GREEN:   Adicionar chamada após cada helmfileSync
REFACTOR: None expected
VERIFY:  cd cli && npm test -- --grep "platform install"
```

#### Acceptance Criteria
- [ ] `runHealthCheck(role)` chamado após cada `helmfileSync()` no Step 4
- [ ] Health check failure impede continuação (exit 3)
- [ ] `LOOPZERO_PROFILE` propagado para health-check.sh
- [ ] Dry-run skipa health checks

#### DoD
- [ ] Código integrado
- [ ] Teste unitário cobrindo chamada ao health check

---

### T2.3 — Fix --skip-infra com cache de outputs

#### Objective
Fix HIGH CLI-F06: `--skip-infra` pula leitura de Pulumi outputs, quebrando L2.

#### Evidence
`platform.ts:124`: `useInfra = !!config.provider && !opts.skipInfra` — quando `skipInfra=true`, `extraEnv` fica vazio e L2 não recebe credenciais de DB, registry, kubeconfigs.

#### Files to edit
```
cli/src/commands/platform.ts — salvar outputs em cache após L1, ler do cache quando --skip-infra
```

#### Deep file dependency analysis
- `platform.ts`: Step 3 (linhas 180-217) executa `pulumiUp()` e `pulumiOutputs()`, mapeia via `pulumiOutputsToEnv()`.
- O fix: após mapear, salvar em `~/.loopzero/pulumi-outputs.json`. Quando `--skip-infra`, ler do cache.
- Sem dependências downstream além do que `extraEnv` alimenta (helmfile env vars).

#### Deep Dives
- Cache file: `~/.loopzero/pulumi-outputs.json`, permissão 0600 (contém credenciais)
- Formato: `{ "environment": "dev", "outputs": { "dbHost": "...", ... } }`
- Validação: se cache existe mas environment não bate, warn e prosseguir sem outputs

#### Tasks
1. Após `pulumiOutputsToEnv()` (linha 213), salvar `{ environment, outputs }` em `~/.loopzero/pulumi-outputs.json` com `fs.writeFileSync(path, JSON.stringify(...), { mode: 0o600 })`
2. Na branch `!useInfra && opts.skipInfra` (novo bloco), tentar ler cache
3. Se cache encontrado e environment bate, usar `pulumiOutputsToEnv(cached.outputs)` para `extraEnv`
4. Se cache não encontrado, warn mas continuar (standalone não precisa)

#### TDD
```
RED:     test_skip_infra_reads_cached_outputs() — assert extraEnv populated from cache
GREEN:   Implementar leitura de cache
RED:     test_skip_infra_warns_on_missing_cache() — assert warning emitted
GREEN:   Implementar warning
REFACTOR: None expected
VERIFY:  cd cli && npm test -- --grep "skip-infra"
```

#### Acceptance Criteria
- [ ] Outputs salvos em `~/.loopzero/pulumi-outputs.json` após L1 bem-sucedido
- [ ] `--skip-infra` lê do cache e popula `extraEnv`
- [ ] Warning emitido se cache não encontrado
- [ ] File permission 0600

#### DoD
- [ ] Código implementado
- [ ] Testes unitários passando

---

### T2.4 — Fix Rule 3 violation e getPlatformStatus()

#### Objective
Fix HIGH CLI-F04 (Rule 3: "CRDs" no output) e CLI-F05 (legacy status detection).

#### Evidence
- `platform.ts:419`: `writeTo(\`  CRDs installed: ${status.crdsInstalled}\`)` — "CRDs" é termo proibido.
- `getPlatformStatus()` usa `releaseExists('loopzero', 'loopzero-system')` que não detecta instalação via helmfile.

#### Files to edit
```
cli/src/commands/platform.ts — substituir "CRDs" por product language
cli/src/installer/components.ts — atualizar getPlatformStatus() para detectar releases helmfile
```

#### Deep file dependency analysis
- `platform.ts:419`: output user-facing. Substituir "CRDs installed" por "Service types configured" ou "Platform definitions installed".
- `components.ts`: `getPlatformStatus()` chama `releaseExists('loopzero', 'loopzero-system')`. Helmfile instala releases com nomes como `loopzero-api`, `tenant-controller`, não `loopzero`. Detectar por label `app.kubernetes.io/managed-by=Helmfile` ou verificar deployments específicos.

#### Tasks
1. `platform.ts:419`: substituir `CRDs installed` por `Definitions installed`
2. `components.ts`: atualizar `getPlatformStatus()` para verificar deployment `tenant-controller` no namespace `loopzero-system` como indicador de instalação helmfile
3. Adicionar fallback: se nem helm release `loopzero` nem deployment `tenant-controller` existe, reportar NotInstalled
4. Grep codebase inteiro para outros usos de "CRD" em output user-facing

#### TDD
```
RED:     test_no_internal_terms_in_status_output() — assert "CRD" not in output
GREEN:   Substituir por product language
REFACTOR: None expected
VERIFY:  cd cli && npm test -- --grep "internal terms"
```

#### Acceptance Criteria
- [ ] Zero ocorrências de "CRD" em output user-facing
- [ ] `getPlatformStatus()` detecta instalação via helmfile
- [ ] Status funciona em ambos os modos (umbrella chart e helmfile)

#### DoD
- [ ] Rule 3 compliance verificada
- [ ] Status detection funciona com helmfile

---

## Phase 3: Integration Validation

**Objective:** Validar que Phases 1 e 2 funcionam juntas em cluster limpo.

### T3.1 — Teste E2E: fresh install em k3d standalone-multi

#### Objective
Provar que `loopzero platform install` funciona do zero em k3d limpo.

#### Evidence
O E2E original (`26e116e0`) passou com estado pré-existente. Precisamos validar first-install.

#### Files to edit
```
infra/tests/e2e/deploy_e2e_test.go — adicionar test case "fresh install" ou script dedicado
```

#### Deep file dependency analysis
- `deploy_e2e_test.go`: teste E2E existente. Pode ser expandido ou podemos criar script separado.
- Depende de: k3d disponível, docker rodando, nenhum cluster `loopzero-*` existente.

#### Tasks
1. Criar script `infra/scripts/test-fresh-install.sh` que:
   - Deleta clusters k3d `loopzero-*` se existirem
   - Roda `loopzero platform install -f test-values.yaml --provider k3d`
   - Verifica exit code 0
   - Verifica que pods em `loopzero-system` estão Running
   - Verifica que Secret `gitea-credentials-template` existe em `argocd` namespace
2. Rodar script e validar

#### TDD
```
RED:     Script em cluster limpo com código antigo → falha
GREEN:   Após fixes das Phases 1 e 2 → passa
REFACTOR: None expected
VERIFY:  bash infra/scripts/test-fresh-install.sh
```

#### Acceptance Criteria
- [ ] Script roda do zero sem erro
- [ ] Pods em `loopzero-system` estão Running
- [ ] Secret `gitea-credentials-template` existe em `argocd`
- [ ] Health checks passam
- [ ] Exit code 0

#### DoD
- [ ] Script criado e validado
- [ ] Evidência de execução bem-sucedida

---

## Phase 4: Charts Fixes (HIGHs — paralelo com Phases 1-2)

**Objective:** Corrigir issues nos Helm charts que não bloqueiam install mas degradam segurança/operação.

### T4.1 — Adicionar health probes nos charts infra/helm

#### Objective
Fix HIGH HC-H3: `infra/helm/tenant-controller` e `loopzero-controller` sem probes.

#### Evidence
Deployments sem liveness/readiness probes não são reiniciados por K8s se o processo travar.

#### Files to edit
```
infra/helm/tenant-controller/templates/deployment.yaml — adicionar probes
infra/helm/loopzero-controller/templates/deployment.yaml — adicionar probes
```

#### Deep file dependency analysis
- Os controllers expõem `:8081` para health probes (definido no `main.go` flags: `--health-probe-bind-address=:8081`).
- `healthz` e `readyz` endpoints são registrados via controller-runtime (linhas 456-463 do main.go).

#### Tasks
1. Ler `operators/cmd/main.go` para confirmar health probe address (`:8081`)
2. Adicionar ao container spec de cada deployment:
   ```yaml
   livenessProbe:
     httpGet:
       path: /healthz
       port: 8081
     initialDelaySeconds: 15
     periodSeconds: 20
   readinessProbe:
     httpGet:
       path: /readyz
       port: 8081
     initialDelaySeconds: 5
     periodSeconds: 10
   ```
3. Verificar com `helm template`

#### TDD
```
RED:     helm template infra/helm/tenant-controller | grep livenessProbe → não encontra
GREEN:   Adicionar probes → encontra
REFACTOR: None expected
VERIFY:  helm template test infra/helm/tenant-controller | grep -A5 livenessProbe
```

#### Acceptance Criteria
- [ ] Ambos os charts têm livenessProbe e readinessProbe
- [ ] Probes apontam para `:8081/healthz` e `:8081/readyz`
- [ ] `helm template` renderiza sem erro

#### DoD
- [ ] Probes adicionados
- [ ] Template rendering verificado

---

### T4.2 — Fix readOnlyRootFilesystem no loopzero-cloud

#### Objective
Fix HIGH HC-H4: `infra/helm/loopzero-cloud` sem `readOnlyRootFilesystem: true`.

#### Evidence
Container security best practice. Os outros charts `infra/helm/` (loopzero-api) já têm.

#### Files to edit
```
infra/helm/loopzero-cloud/templates/deployment.yaml — adicionar readOnlyRootFilesystem: true
```

#### Tasks
1. Adicionar `readOnlyRootFilesystem: true` ao securityContext do container
2. Adicionar `volumeMounts` para `/tmp` se o app precisar escrever em disco (emptyDir)
3. Verificar com `helm template`

#### TDD
```
RED:     helm template infra/helm/loopzero-cloud | grep readOnlyRootFilesystem → não encontra
GREEN:   Adicionar → encontra
REFACTOR: None expected
VERIFY:  helm template test infra/helm/loopzero-cloud | grep readOnlyRootFilesystem
```

#### Acceptance Criteria
- [ ] `readOnlyRootFilesystem: true` presente
- [ ] tmpdir montado se necessário
- [ ] Template renderiza sem erro

#### DoD
- [ ] Security context atualizado

---

### T4.3 — Fix Chart.yaml description do loopzero-cloud

#### Objective
Fix MEDIUM HC-M1: description diz "Next.js" mas loopzero-cloud é Go.

#### Files to edit
```
infra/helm/loopzero-cloud/Chart.yaml — corrigir description
```

#### Tasks
1. Substituir "Next.js web app" por "Go auth/billing service"

#### TDD
```
VERIFY:  grep -c "Next.js" infra/helm/loopzero-cloud/Chart.yaml → 0
```

#### Acceptance Criteria
- [ ] Description correta

#### DoD
- [ ] Chart.yaml atualizado

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | HC-C1: Umbrella chart nil pointer | T0.2 | Remover referência a dependency inexistente |
| 2 | HF-C1: ArgoCD Secret namespace race | T1.1 | ensure_namespace antes do Secret |
| 3 | HF-C2: cloudflared health blocks standalone-multi | T1.2 | Profile-aware health checks |
| 4 | HF-C3: LoadBalancer IP missing em k3d | T1.2 | Fallback para ClusterIP |
| 5 | CLI-F01: Preflight never called | T2.1 | Wire runPreflight() no Step 2 |
| 6 | CLI-F02: Health gates never called | T2.2 | Wire runHealthCheck() no Step 4 |
| 7 | HF-H1: standalone-multi gets NFS/HA | T1.3 | Fix condição para dev/prod explícito |
| 8 | HF-H2: Public DNS in standalone health | T1.2 | In-cluster URLs para standalone |
| 9 | HF-H3: MinIO creds missing standalone | T1.4 | Generate com defaults |
| 10 | HF-H4: Docker bridge IP hardcoded | T1.4 | Variável/service DNS |
| 11 | CLI-F04: Rule 3 "CRDs" | T2.4 | Product language |
| 12 | CLI-F05: Legacy status detection | T2.4 | Detect helmfile releases |
| 13 | CLI-F06: --skip-infra broken | T2.3 | Cache de outputs |
| 14 | HC-H3: Missing health probes | T4.1 | Add probes |
| 15 | HC-H4: Missing readOnlyRootFilesystem | T4.2 | Add security context |
| 16 | HC-H5: Dual chart system undocumented | T0.1 + ADR D1 | Document in CLAUDE.md |
| 17 | HC-H1: Duplicate tenant charts | ADR D2 | Document decision |
| 18 | HC-M1: Wrong chart description | T4.3 | Fix description |
| 19 | Fresh install validation | T3.1 | E2E script |

**Coverage: 19/19 gaps addressed (100%)**

## Global Definition of Done

- [ ] All phases completed (0-4)
- [ ] `helm lint charts/loopzero-platform --with-subcharts` passes
- [ ] `loopzero platform install` works on fresh k3d cluster (T3.1)
- [ ] Zero CRITICAL findings
- [ ] Zero Rule 3 violations in platform commands
- [ ] Health checks pass in all 4 profiles (standalone, standalone-multi, dev, prod)
- [ ] `--skip-infra` works for retry scenarios
- [ ] CLAUDE.md updated with dual chart documentation
- [ ] CHANGELOG.md updated with all fixes

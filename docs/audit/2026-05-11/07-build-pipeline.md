# Module 07 — Build Pipeline (Argo Workflows + BuildKit + cosign + syft + dockerfile-lint)

**Audit Date:** 2026-05-11
**Auditor:** Claude Opus 4.7 (1M context), code-validated
**Confidence:** 88% (Claude-only, alguma incerteza sobre adequação operacional do key rotation)
**Maturity:** **3.3 / 4.0**

## TL;DR

Pipeline bem-arquitetado (strategy pattern, 9 linguagens, 42 frameworks) com hard rejection rules em `loopzero-dockerfile-lint`, cosign key-based signing ATIVO, SBOM CycloneDX via syft, e 177 tests cobrindo construção/dispatch/signing. Bloqueia 3.5-4.0 por **cosign keyless deferred** (code-ready mas sem Fulcio/Rekor LIVE), **signing key rotation undocumented**, image verification deferida para admission e Argo controller single-replica.

---

## 1. Inventário Real

| Componente | LOC | Files | Tests |
|---|---|---|---|
| `api/internal/argo/` | 7,535 | 21 | 147 funcs em 9 files |
| `tools/loopzero-dockerfile-lint/` | 952 | 2 | 30 funcs |
| **Total** | **8,487** | **23** | **177** |

**Breakdown argo/:**
- workflow_builder.go (492) — DAG generation
- languages.go (572) — Strategy + 9 implementations
- buildkit.go (336) — Rootless single-container
- cosign.go (325) — Key-based + keyless code paths
- syft.go (176) — SBOM CycloneDX
- dockerfile_lint.go (84) — Lint step dispatch

**Strategies (9):** NodeStrategy, GoStrategy, PythonStrategy, RubyStrategy, JavaStrategy, PHPStrategy, RustStrategy, CustomStrategy, UserDockerfileStrategy.

**Frameworks suportados:** 42 aliases (nextjs, express, vite, remix, nestjs, fastify, react, vue, svelte, astro, angular, gin, standard, fastapi, django, sinatra, rails, spring, spring-boot, laravel, slim, axum, actix, rocket, etc.).

**Pinned binaries (constants.go):**
- buildctl `v0.18.2-rootless`
- node `20.11.1-alpine`, golang `1.22-alpine`, python `3.12-alpine`
- **loopzero-packs `v0.4.0`** (canonical lenient contract pós-2026-05-08)
- loopzero-cosign-signer `v0.1.0`, loopzero-syft-scanner `v0.1.0`
- loopzero-clone-runner `v0.1.0`, loopzero-dockerfile-lint `v0.1.0`
- aws-cli `2.22.35`

---

## 2. Arquitetura Observada

**DAG entrypoint:** `build-pipeline` em `workflow_builder.go:316`

```
clone-repo (CWT loopzero-clone)
  ├─ per-language group (suffix per group):
  │   ├─ restore-cache-{lang}    (CWT loopzero-cache-restore, optional)
  │   ├─ validate-workspace-{lang}
  │   ├─ install-deps-{lang}     (3× retry)
  │   ├─ security-scan-{lang}
  │   └─ generate-dockerfile-{lang} OR dockerfile-lint-{lang}
  └─ per-app (delivery):
      ├─ build-image-{app}       (BuildKit, max 900s, 3 retry)
      ├─ sign-image-{app}        (cosign key-based atual)
      ├─ sbom-{app}              (syft CycloneDX, continueOn.failed=true)
      └─ mirror-harbor-{app}     (crane copy, best-effort)

onExit DAG: finalize
  └─ save-cache-{lang} (parallel)
```

**Strategy dispatch** (`languages.go:139`):
```go
func ResolveStrategyForApp(app AppSpec) LanguageStrategy {
    if app.Build == "dockerfile" {
        return &UserDockerfileStrategy{}    // orthogonal axis
    }
    return ResolveStrategy(app.Framework)
}
```

**`build: dockerfile`** é axis ortogonal a framework — Node.js app pode opt-in para user Dockerfile, dispatch para `UserDockerfileStrategy` que skip loopzero-packs e roda `dockerfile-lint` no lugar.

**Template name resolução** via `dockerfileStepTemplateName(strategy)`:
- LanguageStrategy → `"generate-dockerfile"` + suffix
- UserDockerfileStrategy → `"dockerfile-lint"` + suffix

Antes (pré-build-mode-dispatch plan T1.6), DAG hardcoded `"generate-dockerfile"` quebrava workflows `build: dockerfile`.

**Cosign signing** (`cosign.go:94`, key-based):
```bash
cosign sign --yes --key /cosign/cosign.key \
  -a "build-system=loopzero" -a "app={appName}" \
  "{imageTag}@{DIGEST}"
```

Outputs Argo: `signature` (full pinned ref) + `digest` (sha256 hash). Trust model: outputs em `/tmp` container-local, **não** shared `/workspace` (prevents BuildKit tampering).

**Syft SBOM** (`syft.go:44`): CycloneDX JSON em `s3://loopzero-static-assets/sbom/{tenant}/{project}/{commit}/{app}.cdx.json`. Outputs: `sbom_s3_key`, `sbom_format`, `sbom_component_count`. **Non-fatal** (`continueOn.failed=true`, linhas 410, 434).

**Admission gate** (`api/internal/services/admission.go:33-112`): cell-level + tenant-level concurrent build limits (in-process counters). Retorna `ErrCellAtCapacity` ou `ErrTenantAtCapacity` (HTTP 429 + Retry-After).

---

## 3. Contratos Públicos

**WorkflowTemplate inputs (params):** tenant-id, project-id, build-id (commit SHA), cache-key.

**Artifacts:** workspace (5Gi PVC), buildkit-registry-config (Secret docker config JSON), cosign-signing-key (Secret), loopzero-minio-credentials (Secret envFrom).

**Outputs per step:**
- `build-image-{app}`: digest (extraído de `/tmp/build-digest.txt`)
- `sign-image-{app}`: signature + digest
- `sbom-{app}`: sbom_s3_key, sbom_format, sbom_component_count

**Allow-list registries (dockerfile-lint default, `main.go:71-80`):**
- `docker.io/library/` (curated official ONLY, não `docker.io/` bare por T2.3 fix)
- `ghcr.io/`, `registry.digitalocean.com/`, `k3d-loopzero-registry:5000/`, `public.ecr.aws/`, `gcr.io/`, `mcr.microsoft.com/`, `quay.io/`

Opt-in via `LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES` (comma-separated prefixes).

**Hard rejection rules (exit 1):**
- `FROM_REGISTRY_NOT_ALLOWED`
- `RUN_FLAG_PROHIBITED` (--privileged, --security=insecure)
- `BIND_MOUNT_ESCAPES_CONTEXT` (absolute/.. src)
- `RESERVED_ENV_KEY` (LOOPZERO_*, KUBERNETES_*, AWS_*, DO_*)
- `SECCOMP_UNCONFINED`, `FILE_NOT_READABLE` (exit 2)

**Soft warnings (exit 0):**
- `USER_ROOT_FINAL_STAGE`, `ADD_REMOTE_URL`, `APT_NO_RECOMMENDS`, `HEALTHCHECK_NO_CMD`

---

## 4. Cobertura de Testes

**147 funcs em argo/_test.go:**
- workflow_builder_test.go (54) — DAG construction + multi-language grouping
- languages_test.go (30) — strategy dispatch + frameworks mapping
- syft_test.go (12) — SBOM metadata + S3 path
- workflow_submitter_test.go (10) — K8s client + retry
- cosign_test.go (8) — script generation + output paths
- buildkit_test.go (7) — args + cache refs + ordering
- scripts_test.go (7) — quoting + shell generation
- user_dockerfile_strategy_test.go (7) — no-op steps + dispatch

**30 funcs em loopzero-dockerfile-lint/main_test.go:** todas regras hard + soft cobertas, integração multi-app, exit codes.

**E2E journeys:** journey_user_dockerfile_test.go (226 LOC, T4.3 happy + T4.4 rejection), cli_build_inspect_test.go, cli_failure_dockerfile_test.go, vertical_slice_test.go, edge_concurrency_test.go.

**Latest run (2026-05-08):** 74/75 PASS em 45m45s (1 ESO timing flake unrelated).

---

## 5. Gaps e Dívida Técnica

| Item | Severidade | Locação/Evidência |
|---|---|---|
| **Cosign keyless deferred** (code-ready, sem OIDC infra) | **HIGH** | `cosign.go:cosignSignTemplateKeyless()` referenced linha 87, `CosignKeyless = false` linha 47 |
| **Signing key rotation undocumented** | **HIGH** | Manual; sem audit log; secret estático `/cosign/cosign.key` |
| Image verification deferred para admission | MÉDIA | Build não verifica; gate é post-build (ArgoCD/K8s policy) |
| Argo controller single-replica (no HA) | MÉDIA | values.yaml replicas=1 (implícito) |
| loopzero-packs version drift potential | MÉDIA | Pinned v0.4.0 (constants.go:39), mas Helm override possível; heal H16 mitiga |
| CWT retries gap (cache-restore/save sem retry) | BAIXA | Cleanroom mitiga |
| LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES opt-in sem audit log | BAIXA | `main.go:115` parse sem structured log |
| TODO/FIXME ativos | **0** ✅ | Deferred work tracked em plans, não no código |
| `latest` tag em images pinned | **0** ✅ | Todos pinned explicitamente |

---

## 6. Avaliação de Maturidade

**Score: 3.3 / 4.0** (confidence 88%)

### Evidências

1. **T1 (commit+test)** — `api/internal/argo/cosign.go:94` (key-based template); `tools/loopzero-dockerfile-lint/main.go:521` LOC + 30 testes; `workflow_builder.go:316` DAG entrypoint; 177 tests total.
2. **T2 (LIVE log)** — Build mode dispatch DONE 2026-05-08 (74/75 LIVE journey); argo-rollouts canary 80/80 GREEN 2026-05-07; cleanroom 12/12 GREEN com build E2E (2026-05-05); `loopzero-packs v0.4.0` deployed canonical.
3. **T3 (CI run)** — `.github/workflows/build-images.yml` + `ci.yml` workflows; build-images publish para DOCR a cada release.

### Raciocínio

**Sustenta 3.0-3.5:**
- Strategy pattern abstraído cleanly (9 strategies + UserDockerfileStrategy ortogonal)
- Hard rejection rules cobrindo top supply-chain gaps (FROM allow-list, --privileged, mounts, reserved env)
- Cosign key-based signing ATIVO + SBOM CycloneDX
- Trace propagation (trace-id/span-id annotations) link builds → deploys
- 177 tests cobrindo construção, dispatch, signing, lint, dockerfile rules
- BuildKit retry semantics (3× backoff até 900s)
- loopzero-packs v0.4.0 canonical lenient contract

**Bloqueia 3.5-4.0:**
- **Cosign keyless deferred** — code pronto mas Fulcio/Rekor não-provisionados (deixa supply chain dependente de long-lived keys)
- **Signing key rotation undocumented** — risco operacional alto (manual, sem schedule, sem audit log)
- **Image signature verification deferida** — janela entre build completion e admission para tampering
- Argo controller single-replica — single point of failure para submissões
- CWT retry gaps — cache failure no primeiro build deixa builds subsequentes sem cache
- Allow-list opt-in (`LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES`) sem audit log de quem estendeu

### Caminho para 3.6-4.0

1. **Ativar cosign keyless** — provisionar Fulcio + Rekor em cluster, flipar `CosignKeyless = true` (Sprint 9, MEET-2026-05-03-063 Track-2)
2. **Documentar signing key rotation** — runbook + automação + audit log structured
3. **Implementar image signature verification em-build** — verify-after-sign step para fechar tampering window
4. **HA Argo controller** — replicas=2 + leader election
5. **Adicionar retry semantics** para CWT cache-restore/save
6. **Structured logging** para parse de `LOOPZERO_DOCKERFILE_ALLOWED_REGISTRIES` opt-in

---

**Próximas auditorias relacionadas:** CI/CD (M12), Observability (M08), Helm (M06).

# LoopZeroKit ↔ LoopZeroCloud — Integration Flow

> **Canonical reference for Claude Code working in either repo.**
> Mirror copy lives at `loopzerokit-tools/loopzerokit/docs/architecture/loopzerokit-loopzerocloud-integration.md`.
> Last updated: 2026-06-06 (post Plan v1.2 — cutover deep-review hardening).

This document explains how the **LoopZeroKit OSS framework** (developer-facing CLI + dev server + build pipeline) connects to the **LoopZeroCloud commercial PaaS** (Go API + GitOps + ArgoCD + K8s). It is the canonical reference for anyone working on either side of the seam — Claude Code MUST consult this doc before editing code that touches `services.json`, `loopzerokit build --target loopzero-cloud`, the multipart upload contract, or the K8s manifest emitter.

## The two systems

| Aspect | LoopZeroKit | LoopZeroCloud |
|--------|---------|-----------|
| Repo | `loopzerokit-tools/loopzerokit/` | `loopzero-cloud/loopzero/` |
| Language | TypeScript | Go |
| Distribution | OSS (npm `loopzerokit`) | Proprietary (Helm chart + `@loopzero/cli`) |
| Surface | Framework CLI (`loopzerokit dev`, `loopzerokit build`) + dev server + templates | HTTP API (`/api/v1/...`) + control plane + deploy CLI (`loopzero`) |
| Knows about K8s? | NO — platform-neutral by design (ADR-0015) | Yes — emits Deployment, Service, IngressRoute, Rollout |
| Knows about the other? | NO — emits a neutral file on disk | NO — consumes a neutral file from upload |

The architectural seam between them is **a single JSON file on disk**: `.loopzero/services.json`. This is the only contract between the two systems. Neither side imports the other's code.

## High-level flow

```
┌─────────────────────────────── DEVELOPER LAPTOP ────────────────────────────────┐
│                                                                                  │
│   ┌──────────────────────────────────────┐                                       │
│   │  loopzerokit-tools/loopzerokit (TypeScript)  │                                       │
│   │                                       │                                       │
│   │  ┌─────────────────────────────────┐  │   1.  pnpm loopzerokit build              │
│   │  │  loopzero.config.ts                 │──┼──▶    --target loopzero-cloud             │
│   │  │  ┌──────────────────────────┐   │  │                                       │
│   │  │  │ name: 'myapp'  ← Plan    │   │  │                                       │
│   │  │  │ services: { … }    v1.2  │   │  │                                       │
│   │  │  └──────────────────────────┘   │  │                                       │
│   │  └─────────────────────────────────┘  │                                       │
│   │                ▼                       │                                       │
│   │  ┌─────────────────────────────────┐  │                                       │
│   │  │  buildManifest(services,        │  │                                       │
│   │  │                project?)        │  │                                       │
│   │  │            (Plan v1.2 T2.3)     │  │                                       │
│   │  └─────────────────────────────────┘  │                                       │
│   │                ▼ emite                 │                                       │
│   │  ┌─────────────────────────────────┐  │                                       │
│   │  │  .loopzero/services.json (v2)       │  │                                       │
│   │  │  ┌──────────────────────────┐   │  │                                       │
│   │  │  │ "version": 2,            │   │  │                                       │
│   │  │  │ "project": "myapp",      │   │  │                                       │
│   │  │  │ "services": [ {          │   │  │                                       │
│   │  │  │   "name": "api",         │   │  │                                       │
│   │  │  │   "runtime": "python",   │   │  │                                       │
│   │  │  │   "type": "server",  ←   │   │  │                                       │
│   │  │  │   "port": 8001,    Plan  │   │  │                                       │
│   │  │  │   "env": {         v1.2  │   │  │                                       │
│   │  │  │     "DATABASE_URL": ".." │   │  │                                       │
│   │  │  │   }                      │   │  │                                       │
│   │  │  │ } ]                      │   │  │                                       │
│   │  │  └──────────────────────────┘   │  │                                       │
│   │  └─────────────────────────────────┘  │                                       │
│   └──────────────────────────────────────┘                                       │
│                                                                                  │
│   ┌──────────────────────────────────────┐                                       │
│   │  loopzero-cloud/loopzero/cli (TypeScript)    │                                       │
│   │                                       │                                       │
│   │  ┌─────────────────────────────────┐  │   2.  loopzero deploy                    │
│   │  │  uploadSource(...)              │  │       (lê .loopzero/services.json)       │
│   │  │  (Plan v1.2 T5.1 + T5.2)        │  │                                       │
│   │  │                                  │  │                                       │
│   │  │  await fs.promises.readFile()   │  │                                       │
│   │  │  formData.append(                │  │                                       │
│   │  │    'services_json', json) ← 1ª  │  │                                       │
│   │  │  formData.append(                │  │                                       │
│   │  │    'bundle', bytes)      ← 2ª  │  │                                       │
│   │  └─────────────────────────────────┘  │                                       │
│   │                ▼                       │                                       │
│   │  POST /api/v1/source/upload            │                                       │
│   │  multipart/form-data                   │                                       │
│   │  Authorization: Bearer <JWT>           │                                       │
│   └──────────────────────────────────────┘                                       │
│                       │                                                          │
└───────────────────────┼──────────────────────────────────────────────────────────┘
                        │
                        │ TLS, Bearer JWT
                        ▼
┌────────────────────────────── LOOPZEROCLOUD (Go API) ───────────────────────────────┐
│                                                                                  │
│   loopzero-cloud/loopzero/api/internal/                                                  │
│                                                                                  │
│   ┌─────────────────────────────────────────────────────────────────────────┐    │
│   │  server/router.go — chi.Router                                          │    │
│   │                                                                          │    │
│   │  r.Group(func(r chi.Router) {                                           │    │
│   │      r.Use(middleware.Auth)        ← Plan v1.2 T1.1                     │    │
│   │      registerProtectedRoutes(r) {  ←   defensive test garante           │    │
│   │          r.Post("/api/v1/source/upload", h.Upload)                      │    │
│   │      }                                                                   │    │
│   │  })                                                                      │    │
│   └─────────────────────────────────────────────────────────────────────────┘    │
│                                  ▼                                               │
│   ┌─────────────────────────────────────────────────────────────────────────┐    │
│   │  routes/source.go — SourceHandler.Upload                                │    │
│   │                                                                          │    │
│   │  ① peekFirstMultipartFieldName(r)  ─── Plan v1.2 T6.1 (EC-5)            │    │
│   │     │  clones r.GetBody (cap 16KB) e checa nome do 1º part              │    │
│   │     ▼                                                                    │    │
│   │     != "services_json" → 400 MULTIPART_FIELD_ORDER                      │    │
│   │                                                                          │    │
│   │  ② r.ParseMultipartForm(maxInMemoryMultipart=32MB)                       │    │
│   │       (T7.1 L2: const nomeada)                                          │    │
│   │                                                                          │    │
│   │  ③ servicesJSONContent := FormValue("services_json")                     │    │
│   │     len > 256KB → 413 SERVICES_JSON_TOO_LARGE  (Plan v1.2 T6.1 EC-4)    │    │
│   │                                                                          │    │
│   │  ④ source.ValidateServicesJSON(servicesJSONContent)                      │    │
│   │     │  delega para ValidateServicesJSONNative (Plan v1.2 T3.3 M6)       │    │
│   │     ▼                                                                    │    │
│   │  ┌────────────────────────────────────────────────────────────────────┐ │    │
│   │  │  source/validator_services.go + validator_env.go                   │ │    │
│   │  │                                                                     │ │    │
│   │  │  • JSON Schema 7 (santhosh-tekuri/jsonschema/v5)                    │ │    │
│   │  │    services.schema.json `oneOf`:                                    │ │    │
│   │  │      v1 (deprecated) ────┐                                          │ │    │
│   │  │      v2 (project req'd) ─┴── ambos aceitos    (Plan v1.2 T2.1 H1)  │ │    │
│   │  │                                                                     │ │    │
│   │  │  • patternProperties em env: ^[A-Z_][A-Z0-9_]*$    (T1.3 C3)        │ │    │
│   │  │    maxLength 4096 + maxProperties 64                                │ │    │
│   │  │                                                                     │ │    │
│   │  │  • validateReservedEnvKeys()                       (T1.3 C3)        │ │    │
│   │  │    bloqueia PATH, HOME, LD_PRELOAD,                                 │ │    │
│   │  │    KUBERNETES_*, LOOPZERO_*, DYLD_*                                     │ │    │
│   │  │    LOOPZERO_ENV_BLOCKLIST_MODE=warn|enforce            (EC-8)           │ │    │
│   │  │                                                                     │ │    │
│   │  │  • cfg.Project = manifest.Project || "services-bundle"              │ │    │
│   │  │    (v1 fallback preserva Gitea lineage)             (T2.1 ADR D10)  │ │    │
│   │  └────────────────────────────────────────────────────────────────────┘ │    │
│   │                                  ▼                                       │    │
│   │  ⑤ FormFile("bundle") → tmpFile (stream to disk)                         │    │
│   │                                                                          │    │
│   │  ⑥ h.store.PushBundle(ctx, uc.TenantID, projectName, tmpFile)            │    │
│   └─────────────────────────────────────────────────────────────────────────┘    │
│                                  │                                               │
│                                  ▼                                               │
│   ┌─────────────────────────────────────────────────────────────────────────┐    │
│   │  source/gitea_store.go — GiteaSourceStore.PushBundle                    │    │
│   │                                                                          │    │
│   │  sourceRepoName(tenantID, projectID)  ─── Plan v1.2 T1.2 (C2)           │    │
│   │      │  ┌─────────────────────────────────────────────┐                 │    │
│   │      │  │ tenantIDRE  = ^[a-z0-9]([a-z0-9-]{0,61}     │                 │    │
│   │      │  │                          [a-z0-9])?$         │ DNS-1123       │    │
│   │      │  │ projectIDRE = (mesmo)                       │ canônico       │    │
│   │      │  └─────────────────────────────────────────────┘                 │    │
│   │      ▼                                                                   │    │
│   │  inválido → fmt.Errorf("%w: …", ErrInvalidTenantOrProject, …)            │    │
│   │       └─▶ rota mapeia via errors.Is → 400 INVALID_TENANT_OR_PROJECT      │    │
│   │                                                                          │    │
│   │  válido →  "loopzero-source-<tenant>-<project>"                             │    │
│   │            git push para Gitea                                          │    │
│   └─────────────────────────────────────────────────────────────────────────┘    │
│                                  │                                               │
│                                  ▼                                               │
│   ┌─────────────────────────────────────────────────────────────────────────┐    │
│   │  manifests/services_input_mapping.go + renderer.go                      │    │
│   │  (caminho assíncrono, disparado pelo build reconciler)                  │    │
│   │                                                                          │    │
│   │  RenderFromServicesManifest(svc, opts)                                  │    │
│   │      │                                                                   │    │
│   │      ▼ mapServicesToRenderInput                                          │    │
│   │  ┌────────────────────────────────────────────────────────────────────┐ │    │
│   │  │ runtimeToStrategyTable = [...]   ←── Plan v1.2 T4.2 (M2 imutável)  │ │    │
│   │  │   "python" → "python-fastapi"                                       │ │    │
│   │  │   "node"   → "node-hono"                                            │ │    │
│   │  │                                                                     │ │    │
│   │  │ resolveServiceType(s.Type)       ←── Plan v1.2 T2.2 (H3 enum)      │ │    │
│   │  │   "" → "server" (default Go-side)                                   │ │    │
│   │  │   frontend SEM Rendering → erro   ←── EC-9                         │ │    │
│   │  │                                                                     │ │    │
│   │  │ opts.ImageRefFor(s.Name)         ←── Plan v1.2 T3.2 (M3 + EC-11)   │ │    │
│   │  │   sem ref + sem default → erro                                      │ │    │
│   │  │   AllowMissingImage → "" OK (só fixtures)                           │ │    │
│   │  └────────────────────────────────────────────────────────────────────┘ │    │
│   │      │                                                                   │    │
│   │      ▼ RenderDocs(input)            ←── Plan v1.2 T4.1 (M1 K8sDoc)      │    │
│   │  ┌────────────────────────────────────────────────────────────────────┐ │    │
│   │  │  ClusterIssuerName == "" → error  ←── Plan v1.2 T3.1 (H4)          │ │    │
│   │  │     (era panic — 5 callers atualizados em lockstep)                │ │    │
│   │  │                                                                     │ │    │
│   │  │  retorna []K8sDoc{ Kind, Name, Namespace, YAML }                   │ │    │
│   │  └────────────────────────────────────────────────────────────────────┘ │    │
│   │      │                                                                   │    │
│   │      ▼ Concat(docs)                 ←── Plan v1.2 T4.3 (O2 Builder)     │    │
│   │  ┌────────────────────────────────────────────────────────────────────┐ │    │
│   │  │  strings.Builder + Grow(estimate)                                   │ │    │
│   │  │  TrimRight("\n") por doc + "\n---\n" entre   ←── EC-10              │ │    │
│   │  └────────────────────────────────────────────────────────────────────┘ │    │
│   └─────────────────────────────────────────────────────────────────────────┘    │
│                                  │                                               │
│                                  ▼                                               │
│   ┌─────────────────────────────────────────────────────────────────────────┐    │
│   │  GitOps push → Gitea                                                    │    │
│   │  ArgoCD reconcile  →  K8s cluster (loopzero-build / loopzero-runtime-*)         │    │
│   │  Deployment + Service + IngressRoute + Rollout                          │    │
│   │  CertManager (ClusterIssuer) emite TLS                                  │    │
│   │  Live HTTP 200 ✓                                                        │    │
│   └─────────────────────────────────────────────────────────────────────────┘    │
│                                                                                  │
└──────────────────────────────────────────────────────────────────────────────────┘
```

## Wire-protocol contract (multipart)

```
POST /api/v1/source/upload                ← rota dentro de auth-protected group
Content-Type: multipart/form-data         ← T1.1 (C1 guard)
Authorization: Bearer <JWT>

  ┌────────────────────────────────────────┐
  │ Content-Disposition: form-data;        │   1ª part (ORDEM OBRIGATÓRIA)
  │   name="services_json"                 │   ← Plan v1.2 T6.1 EC-5
  │   filename="services.json"             │
  │ Content-Type: application/json         │   cap 256 KB ← T6.1 EC-4
  │                                        │
  │ { "version": 2, "project": "myapp", … }│
  └────────────────────────────────────────┘

  ┌────────────────────────────────────────┐
  │ Content-Disposition: form-data;        │   2ª part
  │   name="bundle"                        │
  │   filename="bundle.git"                │
  │ Content-Type: application/octet-stream │   cap 500 MB
  │                                        │
  │ <PACK binary bytes>                    │
  └────────────────────────────────────────┘
```

Full wire spec: `loopzero-cloud/loopzero/docs/api/source-upload-wire-protocol.md`.

## Typed error flow (cause-chain end-to-end)

```
LoopZeroCloud Go side                        loopzero-cloud CLI (TS)           operator
─────────────────                        ───────────────────           ────────
sourceRepoName → ErrInvalidTenantOrProject
        │
        ▼
routes/source.go errors.Is(err, source.ErrInvalidTenantOrProject)
        │
        ▼
HTTP 400 { "code": "INVALID_TENANT_OR_PROJECT", "message": "..." }
        │
        ▼ resp não-ok
        └──▶ ApiError.fromHttpStatus(status=400, msg, code)  ← T5.1 (M4)
             │      │
             │      └── construído com cause = original Error
             │           │
             │           ▼ throw
             └──▶ try {} catch (err) {
                     if (err.code === 'INVALID_TENANT_OR_PROJECT') {
                         console.error(err.message)         ← user vê msg
                         if (LOOPZERO_DEBUG) console.error(err.cause)
                                                            ← debug vê stack
                     }
                  }
```

## services.json schema versions

| Version | Status | `project` field | Use |
|---------|--------|-----------------|-----|
| **v1** | Deprecated (sunset loopzerokit 0.6.0) | absent | Legacy emit; LoopZeroCloud falls back to `services-bundle` (preserves Gitea repo lineage per ADR D10) + logs `SERVICES_JSON_V1_DEPRECATED` warning |
| **v2** | Current | required, DNS-1123-validated | Plan v1.2 T2.1 — drives Gitea repo + ArgoCD App naming explicitly |

Schema enforcement is JSON Schema 7 `oneOf` — both versions are accepted in parallel during the transition window.

Codemod to migrate v1 → v2: `loopzerokit migrate services-json-v1-to-v2`.

## Two-CLI install (today)

```
npm install loopzerokit                  # OSS framework (your project's package.json)
npm install -g @loopzero/cli             # proprietary deploy CLI

$ loopzerokit dev                        # framework: local dev server
$ loopzerokit build --target loopzero-cloud  # framework: emits .loopzero/services.json
$ loopzero login                         # deploy CLI: auth
$ loopzero deploy                        # deploy CLI: lê services.json, POST upload
```

The two-CLI sequence is the architectural seam mandated by CLAUDE.md `LoopZeroCloud-first re-lock (2026-05-27)`: the OSS framework cannot depend on proprietary code. A follow-up plan ([`loopzero-cli-vercel-like-ux-plan.md`](../../../../loopzerokit-tools/.claude/knowledge-base/plans/loopzero-cli-vercel-like-ux-plan.md)) makes `loopzero deploy` invoke `loopzerokit build` as a subprocess automatically — preserving the seam while removing the manual step.

## Hardening invariants (Plan v1.2 — 2026-06-06)

Plan v1.2 closed 22 findings on the integration seam. The invariants that MUST hold (any future code change that breaks them is a regression):

### Auth
- **`POST /api/v1/source/upload` is inside the `r.Group(middleware.Auth)` closure** (`api/internal/server/router.go`). Defensive tests in `api/tests/unit/source_upload_auth_test.go` break CI if this moves.

### Validation
- **`tenantID` and `projectID` MUST match DNS-1123** before flowing into `fmt.Sprintf` repo names. Single canonical regex: `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`. Sentinel: `source.ErrInvalidTenantOrProject` (`api/internal/source/gitea_store.go`).
- **`env` keys MUST match POSIX shape AND reject reserved keys**. Schema: `patternProperties ^[A-Z_][A-Z0-9_]*$` + `maxLength 4096` + `maxProperties 64`. Blocklist: `PATH`, `HOME`, `USER`, `SHELL`, `LD_PRELOAD`, `LD_LIBRARY_PATH` + prefixes `DYLD_*`, `KUBERNETES_*`, `LOOPZERO_*` (`api/internal/source/validator_env.go`).
- **services.json schema is `oneOf` discriminated by `version`**. Add a v3 only via a new branch — never break v1+v2.

### Wire
- **Multipart field order is part of the contract.** `services_json` first, `bundle` second. Enforced via `peekFirstMultipartFieldName` (`api/internal/routes/source.go`). Documented at `docs/api/source-upload-wire-protocol.md`.
- **`services_json` field is capped at 256 KB**; `bundle` at 500 MB. Constants: `maxServicesJSONSize`, `maxBundleSize`.

### Error discipline
- **`manifests.Render()` MUST return `(string, error)` — never panic**. All 5 production callers (`build_reconcile_complete.go`, `build_reconcile_retry.go`, `domain_provisioner.go` ×2, `services_input_mapping.go:RenderFromServicesManifest`) treat the error as a recoverable build failure.
- **`RenderOpts.ImageRefFor()` returns error when no ref and no default** unless `AllowMissingImage=true` (fixture-only escape hatch).
- **`runtimeToStrategyTable` is an immutable array** (`[...]runtimeToStrategyEntry{...}`). Never a `map` — Go maps are not concurrent-safe for write+read.

### CLI
- **`ApiError` preserves `cause` chain** via TC39 proposal-error-cause. `cli/src/adapters/source-upload.ts` threads it through all 3 catch branches with codes `UPLOAD_TIMEOUT` / `CONNECTION_REFUSED` / `UPLOAD_FAILED`.
- **`fs.promises.readFile` (async), never `readFileSync`** in the upload path — keeps the event loop responsive while reading 500 MB bundles.

## Reference table — when to consult what

| If you are editing… | Read first | Then read |
|---------------------|------------|-----------|
| `services.schema.json` | This doc + Plan v1.2 §Phase 2 | `loopzerokit/src/services/adapters-bridge/manifest.ts` (mirror types) |
| `loopzerokit build --target loopzero-cloud` emit | This doc + Plan v1.2 T2.3 | `services-manifest-v2.test.ts` (cross-product gate) |
| `routes/source.go` upload handler | This doc + wire protocol spec | Plan v1.2 T1.1 / T1.2 / T6.1 tests |
| `manifests/renderer.go` | This doc | Plan v1.2 T3.1 grep audit + T4.1 K8sDoc tests |
| `cli/src/adapters/source-upload.ts` | This doc + wire protocol spec | Plan v1.2 T5.1 / T5.2 tests |
| `cli/src/commands/deploy.ts` | This doc + Vercel-like UX plan | Plan v1.2 T5.1 cause-chain pattern |
| any new validator on the upload path | This doc + Plan v1.2 T1.3 EC-8 | warn-mode toggle pattern via env var |

## Cross-references

- **Plan v1.2 implementation report:** `loopzerokit-tools/.claude/knowledge-base/reviews/cutover-deep-review-hardening-implementation-2026-06-06.md`
- **Plan v1.2 plan:** `loopzerokit-tools/.claude/knowledge-base/plans/cutover-deep-review-hardening-plan.md`
- **Plan v1.2 edge cases:** `loopzerokit-tools/.claude/knowledge-base/reviews/cutover-deep-review-hardening-edge-cases-2026-06-05.md`
- **Wire protocol spec:** `loopzero-cloud/loopzero/docs/api/source-upload-wire-protocol.md`
- **T3.1 callers audit:** `loopzerokit-tools/.claude/knowledge-base/audits/t3-1-render-callers-2026-06-06.md`
- **Architecture baseline (pre-Plan v1.2):** `loopzerokit-tools/.claude/knowledge-base/architecture/cutover/`
- **Phase 8 dogfood substitute:** `loopzerokit-tools/.claude/knowledge-base/audits/phase-8-dogfood-substitute-2026-06-06.md`
- **Vercel-like UX follow-up plan:** `loopzerokit-tools/.claude/knowledge-base/plans/loopzero-cli-vercel-like-ux-plan.md`

## When to update this doc

- A new finding closes a hardening invariant → add to §Hardening invariants.
- A new schema version ships → update the services.json schema versions table.
- The architectural seam changes (e.g., two-CLI flow unifies) → update §Two-CLI install + the diagram.
- A new error code is registered on the wire → update §Typed error flow.

Keep the two mirror copies in sync (`loopzero-cloud/loopzero/docs/architecture/` and `loopzerokit-tools/loopzerokit/docs/architecture/`). When you edit one, copy the diff to the other in the same commit.

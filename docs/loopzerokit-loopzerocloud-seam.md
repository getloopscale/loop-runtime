# The LoopZeroKit ↔ LoopZeroCloud seam

**Mandatory pre-read.** Consult this before editing anything that touches
`api/internal/source/sourcehttp/source.go`, `api/internal/source/services.schema.json`,
`api/internal/source/validator_*.go`, `api/internal/manifests/renderer.go`, or
the `services_json` / multipart upload contract.

This document used to be mirrored on the LoopZeroKit side. **That mirror no longer exists
anywhere in the workspace**, so this copy is the only one. If you re-establish a mirror,
restore the "edit one, copy the diff in the same commit" rule with it.

## The two systems

| | LoopZeroKit | LoopZeroCloud |
|---|---|---|
| Repo | `loopzerokit-tools/loopzerokit/` | this one |
| Language | TypeScript | Go |
| Distribution | OSS (npm `loopzerokit`) | proprietary (Helm chart + `@loopzero/cli`) |
| Surface | framework CLI (`loopzerokit dev`, `loopzerokit build`) + dev server + templates | HTTP API (`/api/v1/...`) + control plane + deploy CLI (`loopzero`) |
| Knows about Kubernetes? | **NO** — platform-neutral by design (ADR-0015) | yes — emits Deployment, Service, IngressRoute, Rollout |
| Knows about the other? | **NO** — emits a neutral file on disk | **NO** — consumes a neutral file from upload |

The seam is **a single JSON file on disk**: `.loopzero/services.json`. It is the only contract
between the two systems, and neither side imports the other's code. That is what lets
someone adopt LoopZeroKit without adopting the platform.

## The flow

```
1. loopzerokit build --target loopzero-cloud
     loopzero.config.ts -> buildManifest(services, project) -> .loopzero/services.json (v2)

2. loopzero deploy   (the CLI in ../loopzero-cli)
     reads .loopzero/services.json
     POST /api/v1/source/upload   multipart/form-data, Authorization: Bearer <JWT>
       part 1: services_json      <- order is part of the contract
       part 2: bundle

3. api/internal/source/sourcehttp/source.go — SourceHandler.Upload
     readUploadForm(r) streams the body with r.MultipartReader():
     a. first part is not services_json     -> 400 MULTIPART_FIELD_ORDER
                                               (a loopzero_yaml first part -> 422 E-009)
     b. services_json > 256 KB              -> 413 SERVICES_JSON_TOO_LARGE
     c. source.ValidateServicesJSONNative   -> 422 VALIDATION_FAILED
        (all three happen BEFORE a single bundle byte is read)
     d. bundle part spooled to a temp file  -> 413 PAYLOAD_TOO_LARGE past 500 MB
     e. tenant/project not DNS-1123         -> 400 INVALID_TENANT_OR_PROJECT
     f. push to Gitea + object store        -> 500 SOURCE_UPLOAD_FAILED on failure
```

## services.json schema versions

| Version | Status | `project` field | Use |
|---|---|---|---|
| **v1** | deprecated (sunset loopzerokit 0.6.0) | absent | legacy emit; the platform falls back to `services-bundle` (preserves Gitea repo lineage) and logs `SERVICES_JSON_V1_DEPRECATED` |
| **v2** | current | required, DNS-1123-validated | drives Gitea repo + ArgoCD App naming explicitly |

The schema is `oneOf` discriminated by `version`. **Add a v3 only via a new branch — never
break v1+v2.**

## Hardening invariants

Any future change that breaks one of these is a regression.

### Auth

- **`POST /api/v1/source/upload` is inside the `r.Group(middleware.Auth)` closure**
  (`api/internal/server/router.go`). Defensive tests in
  `api/tests/unit/source_upload_auth_test.go` break CI if this moves.

### Validation

- **`tenantID` and `projectID` MUST match DNS-1123** before flowing into `fmt.Sprintf` repo
  names. Single canonical regex `^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`; sentinel
  `source.ErrInvalidTenantOrProject` (`api/internal/source/gitea_store.go`).
- **`env` keys MUST match POSIX shape AND reject reserved keys.** Schema:
  `patternProperties ^[A-Z_][A-Z0-9_]*$` + `maxLength 4096` + `maxProperties 64`. Blocklist:
  `PATH`, `HOME`, `USER`, `SHELL`, `LD_PRELOAD`, `LD_LIBRARY_PATH`, plus prefixes `DYLD_*`,
  `KUBERNETES_*`, `LOOPZERO_*` (`api/internal/source/validator_env.go`).

### Wire

- **Multipart field order is part of the contract** — `services_json` first, `bundle`
  second. Enforced by `readUploadForm` (`api/internal/source/sourcehttp/upload_form.go`),
  which streams the body part by part and refuses a first part that is not
  `services_json` before reading anything else. It replaced a `GetBody`-based peek that
  was unreachable for real traffic: `net/http` never sets `GetBody` on a server request,
  so the order went unenforced while a test that set `GetBody` by hand stayed green. The
  order test now goes through a real `httptest.Server`
  (`api/tests/unit/source_upload_field_limits_test.go`).
- **`services_json` is capped at 256 KB; `bundle` at 500 MB.** Constants
  `maxServicesJSONSize`, `maxBundleSize`.

### Error discipline

- **`manifests.Render()` MUST return `(string, error)` — never panic.** All production
  callers treat the error as a recoverable build failure.

### CLI (in `../loopzero-cli`)

- **`ApiError` preserves the `cause` chain**, threaded through all three catch branches with
  codes `UPLOAD_TIMEOUT` / `CONNECTION_REFUSED` / `UPLOAD_FAILED`.
- **`fs.promises.readFile` (async), never `readFileSync`** on the upload path — keeps the
  event loop responsive while reading a 500 MB bundle.

## When to update this document

- A new invariant closes -> add it to § Hardening invariants.
- A new schema version ships -> update the versions table.
- The seam itself changes -> update § The flow.
- A new error code is registered on the wire -> update
  [`api-contracts.md`](api-contracts.md).

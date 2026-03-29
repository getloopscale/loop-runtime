# loopzero-packs Integration Plan

> Replace hardcoded Dockerfile generation with loopzero-packs engine.
> MEET-2026-03-29-017: Approved with conditions.

---

## Problem

`api/internal/argo/scripts.go` generates Dockerfiles via inline bash templates with
hardcoded `CMD` values. Express apps with `src/index.js` crash (CMD assumes `index.js`).
Go apps assume binary name `app`. Python assumes `uvicorn main:app`. All wrong for
real projects.

## Solution

Integrate `loopzero-packs/core` as a container image in the Argo Workflow build pipeline.
The `generate-dockerfile` step runs `loopzeropacks-generate` binary instead of bash scripts.

---

## Phase 1 — Container Image (Day 1, ~3h)

### 1.1 Create loopzeropacks-generate binary
**File:** `loopzero-packs/cmd/loopzeropacks-generate/main.go`

```
loopzeropacks-generate \
  --source /workspace \
  --app-path apps/api \
  --app-name api \
  --output /workspace/Dockerfile.api
```

- Reads source from `--source` (PVC mount in workflow)
- Calls `core.GenerateBuildPlan()` + `dockerfile.Generate()`
- Writes Dockerfile to `--output`
- Logs generated Dockerfile to stdout (for Loki observability)
- Exits 0 on success, 1 on failure with actionable error to stderr
- If `/workspace/Dockerfile` already exists → skip (user-provided takes precedence)

### 1.2 Dockerfile for loopzeropacks-generate image
**File:** `loopzero-packs/Dockerfile.generate`

```dockerfile
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go build -o /loopzeropacks-generate ./cmd/loopzeropacks-generate

FROM alpine:3.20
COPY --from=build /loopzeropacks-generate /usr/local/bin/
ENTRYPOINT ["loopzeropacks-generate"]
```

### 1.3 Add to build-images.yml
Add job `build-loopzeropacks` that builds and pushes `loopzero-packs-runner:dev` to DO registry.

### Acceptance Criteria
- [ ] `loopzeropacks-generate --source examples/node-bun --output /tmp/Dockerfile` produces valid Dockerfile
- [ ] Dockerfile uses `npm start` (not `node index.js`) for Node.js apps
- [ ] User Dockerfile takes precedence (skip generation)
- [ ] Image builds and pushes in CI

---

## Phase 2 — Pipeline Integration (Day 1-2, ~3h)

### 2.1 Replace DockerfileStep in languages.go
Remove per-language `DockerfileStep()` from LanguageStrategy interface.
Add single `generateDockerfileStep()` in `workflow_builder.go`:

```go
func generateDockerfileStep(app services.AppSpec) *forge.Container {
    return buildContainer("generate-dockerfile-"+app.Name,
        "registry.digitalocean.com/loopzero-infra-registry/loopzero-packs-runner:dev",
        resourcesSmall(),
        fmt.Sprintf("loopzeropacks-generate --source /workspace --app-path %s --app-name %s --output /workspace/Dockerfile.%s",
            app.Path, app.Name, app.Name),
    )
}
```

### 2.2 Remove from scripts.go
Delete:
- `scriptDockerfileNode()` (~60 lines)
- `scriptDockerfileGo()` (~30 lines)
- `scriptDockerfilePython()` (~25 lines)

### 2.3 Update LanguageStrategy interface
Remove `DockerfileStep()` from interface. Keep: `ValidateStep`, `InstallStep`, `BuildStep`, `ScanStep`.

### Acceptance Criteria
- [ ] Argo Workflow uses `loopzero-packs-runner` image for generate-dockerfile step
- [ ] Express app with `src/index.js` gets correct CMD (`npm start`)
- [ ] Go app gets correct binary name from `go.mod` module
- [ ] Python app uses `scripts.start` or correct uvicorn command
- [ ] All 498 CLI tests pass
- [ ] E2E: create-loopzero express template deploys successfully

---

## Phase 3 — CLI Improvements (Day 2, ~2h)

### 3.1 Rename `build: buildpack` → `build: auto`
- CLI: `loopzero init` generates `build: auto` (not `buildpack`)
- API: Accept both `auto` and `buildpack` (backward compat)
- Log deprecation warning when `buildpack` is used

### 3.2 `loopzero validate --show-build` (DX condition)
Run loopzeropacks locally on the user's source and display the Dockerfile that would be generated.

```bash
$ loopzero validate --show-build

✓ loopzero.yaml valid (2 apps)

Generated Dockerfile for api:
  FROM node:20-alpine
  WORKDIR /app
  COPY package*.json ./
  RUN npm ci --omit=dev
  COPY . .
  EXPOSE 3000
  CMD ["npm", "start"]

Generated Dockerfile for web:
  (static delivery — no Dockerfile needed)
```

This requires `loopzeropacks/core` as a Go dependency in a small CLI helper binary, OR
the CLI shells out to `loopzeropacks-generate --dry-run`.

### 3.3 Review loopzero-packs error messages for internal terms
Scan all logger messages for Rule 3 violations.

### Acceptance Criteria
- [ ] `loopzero init` generates `build: auto`
- [ ] `loopzero validate --show-build` displays generated Dockerfile
- [ ] `build: buildpack` still works with deprecation warning
- [ ] No internal terms in CLI output

---

## Phase 4 — Validation & Cleanup (Day 2-3, ~2h)

### 4.1 E2E test with create-loopzero templates
Deploy each template against DO dev:
- `node-express` — API server
- `go-api` — Go server
- `monorepo-turbo` — API + Next.js (static)
- `python-fastapi` — Python API

### 4.2 Clean up
- Remove dead code from scripts.go (Dockerfile functions)
- Update CLAUDE.md with loopzero-packs reference
- Update CHANGELOG

### 4.3 Dogfood
Run `/dogfood full` with create-loopzero templates.

### Acceptance Criteria
- [ ] All 4 templates deploy successfully
- [ ] Prova de Vida passes for each
- [ ] Delete + Prova de Morte passes
- [ ] Zero S0/S1 achados

---

## Execution Order

```
Phase 1 (Container Image)  ████████████████  3h
Phase 2 (Pipeline)         ████████████████  3h  ← depends on Phase 1
Phase 3 (CLI)              ████████████      2h  ← parallel with Phase 2
Phase 4 (Validation)       ████████          2h  ← depends on Phase 2+3
                                             ────
                                             ~10h total (2 days)
```

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| loopzeropacks generates wrong Dockerfile for edge case | 108 tested examples; user can switch to `build: dockerfile` |
| Image pull adds latency to builds | Pre-pull on build nodes; image is small (~20MB alpine) |
| loopzero-packs dependency tree changes | Pin image by tag, not latest |
| Existing deploys get different Dockerfiles | Document as behavior change; Dockerfiles are better, not worse |

---

## Files Affected

| Phase | Files | Action |
|-------|-------|--------|
| 1 | `loopzero-packs/cmd/loopzeropacks-generate/main.go` | NEW |
| 1 | `loopzero-packs/Dockerfile.generate` | NEW |
| 1 | `.github/workflows/build-images.yml` | ADD job |
| 2 | `api/internal/argo/languages.go` | MODIFY (remove DockerfileStep) |
| 2 | `api/internal/argo/workflow_builder.go` | MODIFY (add universal step) |
| 2 | `api/internal/argo/scripts.go` | DELETE 3 functions |
| 2 | `api/internal/argo/constants.go` | ADD loopzeropacks image constant |
| 3 | `cli/src/adapters/app-unit.ts` | MODIFY (buildpack → auto) |
| 3 | `cli/src/commands/validate.ts` | MODIFY (add --show-build) |
| 4 | `CLAUDE.md`, `CHANGELOG.md` | UPDATE |

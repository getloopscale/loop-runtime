# Contributing to LoopZero

Thank you for your interest in contributing to LoopZero! This document provides guidelines and instructions for contributing.

## Code of Conduct

This project follows the [CNCF Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## How to Contribute

### Reporting Issues

- Use [GitHub Issues](https://github.com/loopzero/loopzero/issues) for bug reports and feature requests.
- For security vulnerabilities, see [SECURITY.md](SECURITY.md).
- Search existing issues before creating a new one.
- Include reproduction steps, expected behavior, and actual behavior for bugs.

### Development Setup

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev), [Helm](https://helm.sh) v3.14+, [kubectl](https://kubernetes.io/docs/tasks/tools/), [kubeconform](https://github.com/yannh/kubeconform), [golangci-lint](https://golangci-lint.run) v2.11+

```bash
# 1. Fork and clone the repository
git clone https://github.com/<your-username>/loopzero.git
cd loopzero

# 2. Bootstrap local environment (idempotent)
task setup

# 3. Start development loop with hot reload
tilt up

# 4. Run all tests
task test

# 5. Run linters
task lint
```

### Submitting Changes

1. **Read the spec first.** The authoritative specification is `docs/technical.md`. Every state machine, failure mode, and CLI output format is documented there.

2. **Create a branch** from `main`:
   ```bash
   git checkout -b feat/my-feature main
   ```
   Branch naming: `feat/`, `fix/`, `refactor/`, `docs/` prefixes.

3. **Write the test first.** No production code without a failing test. This is not a guideline — it is the development process:
   - Write the test describing expected behavior
   - Run it — confirm it fails (red)
   - Implement the minimum code to make it pass (green)
   - Refactor without changing behavior

4. **Run the full validation suite:**
   ```bash
   task test          # All tests pass
   task lint          # No lint errors
   task validate:manifests  # Manifests valid
   ```

5. **Sign your commits** using the Developer Certificate of Origin (DCO):
   ```bash
   git commit -s -m "feat: add support for X"
   ```
   Every commit must include a `Signed-off-by` line. This certifies that you wrote the code or have the right to submit it under the Apache 2.0 license.

6. **Open a Pull Request** against `main`. Include:
   - What changed and why
   - Link to relevant issue
   - Test plan (what tests were added/modified)

### Pull Request Checklist

Before submitting, verify:

- [ ] `task test` passes (all tests green)
- [ ] `task lint` passes (no warnings)
- [ ] `task validate:manifests` passes
- [ ] New code has SPDX license headers
- [ ] No internal terms in CLI output (`ArgoCD`, `Gitea`, `MinIO`, `CRD`, `Kaniko` — see Rule 3 in CLAUDE.md)
- [ ] Mutating API routes use `withEnvironmentLock`
- [ ] Mutating API routes handle `Idempotency-Key`
- [ ] CLI `--json` output matches spec section 0.3 schema
- [ ] Exit codes are 0/1/2/3/4/5 only
- [ ] State machine changes match spec section 5
- [ ] CHANGELOG.md updated under `[Unreleased]`

## Architecture

LoopZero is organized into four planes:

| Plane | Components | Technology |
|-------|-----------|------------|
| **Developer** | CLI, API | TypeScript (Commander), Go (chi + pgx) |
| **Control** | Operators, Gitea, PostgreSQL, ArgoCD | Go (kubebuilder + controller-runtime) |
| **Build** | Argo Workflows + Kaniko | Container image builds |
| **Workload** | User applications | Managed by ArgoCD |

### Repository Structure

| Directory | Owner | Language |
|-----------|-------|----------|
| `cli/` | CLI Engineer | TypeScript |
| `api/` | API Engineer | Go |
| `operators/` | Operator Engineer | Go |
| `pkg/` | Platform SDK Engineer | Go (zero external deps) |
| `infra/` | Infrastructure Engineer | Helm, Pulumi (TypeScript), k3d |

### Component-Specific Guidelines

**CLI (`cli/`):**
- DX quality bar is the Vercel CLI
- Every command needs `--json` flag, correct exit code, CI mode support
- No internal terms in any user-facing string

**API (`api/`):**
- All state mutations use `withEnvironmentLock` (pg_advisory_xact_lock)
- All mutating endpoints support `Idempotency-Key`
- Use `LoopZeroError` with typed error codes — never generic strings

**Operators (`operators/`):**
- Write Ginkgo spec before implementation
- Reconcile must be idempotent
- State transitions via state machine only (no direct status updates)

**Shared libraries (`pkg/`):**
- Zero external dependencies
- Changes here affect all components — extra care required

## Testing

We follow the test pyramid:

```
       /  E2E  \         <- Few: infra/tests/e2e/ (Ginkgo)
      /----------\
     / Integration \     <- Moderate: api/tests/integration/
    /--------------\
   /    Unit        \    <- Many: cli/tests/unit/ + *_test.go
  /------------------\
```

- **Unit tests** for all business logic — fast, deterministic, no I/O
- **Integration tests** for boundaries — database, external APIs
- **E2E tests** for critical user flows — deploy happy path, concurrent deploys

Naming: describe behavior, not methods. Good: `test_deploy_fails_when_environment_locked`. Bad: `test_deploy_1`.

Pattern: Arrange-Act-Assert (AAA) or Given-When-Then.

## Style Guide

### Go
- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `golangci-lint` (config in `.golangci.yml`)
- Errors: use `LoopZeroError` from `pkg/errors`
- Logging: use `pkg/logging` (structured JSON via slog)
- IDs: use `pkg/id` (crypto/rand, `b-` prefix for builds, `r-` for releases)

### TypeScript
- Strict mode enabled
- ESLint with `@typescript-eslint`
- Minimal dependencies (only `commander` and `js-yaml` in production)

### Commits
- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `refactor:`, `docs:`, `test:`, `chore:`
- Sign all commits with DCO (`git commit -s`)
- One logical change per commit

## Decision Records

Significant design decisions are documented as ADRs in `docs/adr/`. If your contribution involves a non-trivial architectural choice, please include an ADR.

## Getting Help

- Open a [GitHub Discussion](https://github.com/loopzero/loopzero/discussions) for questions
- Join our community channel (see project README)
- Tag `@loopzero-maintainers` on your PR if you need a review

## License

By contributing to LoopZero, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).

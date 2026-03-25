# Release Process

This document describes how LoopZero releases are created and managed.

## Versioning

LoopZero follows [Semantic Versioning 2.0.0](https://semver.org/):

- **MAJOR** (`X.0.0`) — Incompatible API changes, CLI breaking changes, `--json` schema changes
- **MINOR** (`0.X.0`) — New features, new CLI commands, new API endpoints
- **PATCH** (`0.0.X`) — Bug fixes, security patches, performance improvements

### Pre-1.0 Expectations

While LoopZero is pre-1.0 (`0.x.y`):
- Minor versions may include breaking changes (documented in CHANGELOG)
- The CLI stability contract (exit codes, `--json` shape) is honored regardless of version
- CRD API versions follow Kubernetes conventions (`v1alpha1` → `v1beta1` → `v1`)

## Release Cadence

| Type | Frequency | Trigger |
|------|-----------|---------|
| **Minor release** | Every 4-6 weeks | Feature sprint completion |
| **Patch release** | As needed | Bug fix or security patch |
| **Security release** | Within severity timeline | Vulnerability report (see SECURITY.md) |

## Release Artifacts

Each release produces:

| Artifact | Location | Format |
|----------|----------|--------|
| CLI binary | GitHub Releases | `loopzero-{os}-{arch}` (linux, darwin, windows × amd64, arm64) |
| Container images | Container Registry | `loopzero-api:{version}`, `loopzero-operator:{version}` |
| Helm charts | GitHub Releases | `loopzero-{chart}-{version}.tgz` |
| CRD manifests | GitHub Releases | `loopzero-crds-{version}.yaml` |
| CHANGELOG | GitHub Releases | Release notes extracted from CHANGELOG.md |

## Cutting a Release

### Prerequisites

- [ ] All tests pass on `main` (`task test`)
- [ ] All linters pass (`task lint`)
- [ ] All manifests valid (`task validate:manifests`)
- [ ] CHANGELOG.md `[Unreleased]` section has entries
- [ ] No critical/high security findings open

### Process

1. **Update CHANGELOG.md** — Move `[Unreleased]` entries to a new version section:
   ```markdown
   ## [0.2.0] - 2026-04-01
   ```

2. **Create a version tag:**
   ```bash
   git tag -s v0.2.0 -m "Release v0.2.0"
   git push origin v0.2.0
   ```

3. **CI builds release artifacts** — The tag push triggers:
   - CLI binary compilation (cross-platform)
   - Container image build with version tag
   - GitHub Release creation with artifacts and changelog

4. **Verify the release:**
   - [ ] GitHub Release page has all artifacts
   - [ ] Container images are pullable with version tag
   - [ ] CLI binary works: `loopzero --version` shows new version

5. **Announce** — Post in community channels.

### Hotfix Process

For critical bugs or security fixes on a released version:

1. Create a release branch: `release/v0.2.x` from the `v0.2.0` tag
2. Cherry-pick the fix
3. Tag: `v0.2.1`
4. Follow the standard release process

## Support Policy

| Version | Support Level |
|---------|-------------|
| Latest minor release | Full support (bugs + security + features) |
| Previous minor release | Security fixes only |
| Older versions | No support |

## Image Tagging Strategy

| Tag | Meaning | Mutable? |
|-----|---------|----------|
| `v0.2.0` | Specific release | No (immutable) |
| `v0.2` | Latest patch of minor | Yes |
| `v0` | Latest minor of major | Yes |
| `latest` | Latest release | Yes |
| `sha-abc1234` | Specific commit | No (immutable) |
| `dev` | Latest from `main` | Yes |

## Platform Support

| Platform | Architecture | Status |
|----------|-------------|--------|
| Linux | amd64 | Supported |
| Linux | arm64 | Supported |
| macOS | amd64 | Supported |
| macOS | arm64 (Apple Silicon) | Supported |
| Windows | amd64 | Best effort |

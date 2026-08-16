# Security Policy

## Reporting a Vulnerability

The LoopZero team takes security vulnerabilities seriously. We appreciate your efforts to responsibly disclose your findings.

**Do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of the following channels:

1. **GitHub Security Advisories** (preferred): [Report a vulnerability](https://github.com/getloopscale/loopzero/security/advisories/new)
2. **Email**: security@loopzero.example

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Affected versions
- Impact assessment (what an attacker could achieve)
- Any potential mitigations you've identified

### Response Timeline

| Action | Timeline |
|--------|----------|
| Acknowledgment of report | Within 48 hours |
| Initial assessment | Within 5 business days |
| Fix development | Depends on severity (see below) |
| Public disclosure | After fix is released |

### Severity-Based Response

| Severity | Fix Timeline | Action |
|----------|-------------|--------|
| **Critical** (RCE, auth bypass, data exfiltration) | 72 hours | Immediate patch release |
| **High** (privilege escalation, significant data exposure) | 7 days | Patch release |
| **Medium** (limited exposure, requires specific conditions) | 30 days | Next scheduled release |
| **Low** (minimal impact, defense-in-depth) | 90 days | Next scheduled release |

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Previous minor release | Security fixes only |
| Older versions | No |

## Security Measures

LoopZero implements the following security practices:

### Build and quality gates

GitHub Actions was retired as the pipeline in 2026-05-23. The scanners below run through
`task` — locally, on the operator machine, or from whichever CI a deployment wires up. They are
**not** enforced automatically on every push to this repository.

- **Trivy** — CVE scanning (CRITICAL + HIGH). `loopzero-ops quality trivy-scan`, wired into `task quality:gates`.
- **TruffleHog** — verified-secret detection. `loopzero-ops quality trufflehog-scan`, wired into `task quality:gates`.
- **govulncheck** — Go stdlib + module CVEs across `api`, `operators`, `pkg`. `task vuln-scan`; `task vuln-scan:strict` fails on MEDIUM+.
- **kubeconform --strict** — Kubernetes manifest schema validation. `task validate:manifests`.
- **cosign + syft** — image signing and CycloneDX SBOM on publish (`.github/workflows/build-publish.yml`).
- **108 pure-logic gates** in `cmd/loopzero-ops` — `task quality:gates`; after `task quality:build`, `/tmp/loopzero-ops quality --list` is the authoritative inventory, including which gates are wired and which are registered-but-unrun. A stray `./loopzero-ops` at the repo root is a gitignored local artifact and can be badly stale.

The CLI moved to the `loopzero-cli` repository at M2; its `npm audit` runs there.

### Runtime
- Non-root containers (UID 1000)
- Multi-stage Docker builds with minimal base images
- Per-tenant isolation via Kubernetes namespaces, RBAC, and NetworkPolicies
- Transaction-scoped advisory locks (`pg_advisory_xact_lock`) for concurrency control
- GitHub token validation with caching

### Secrets
- **OpenBao (Transit)** holds tenant secrets; values are stored encrypted (`vault:v1:` prefixed) and never persisted in plaintext.
- **External Secrets Operator** projects them into workload namespaces; the plaintext value exists only in the tenant's Kubernetes Secret.
- OpenBao runs in its **own `loopzero-secrets` cluster**, separate from mgmt — a deliberate blast-radius boundary. Installing it into mgmt is forbidden.
- Secrets are never exposed in CLI output, logs, or audit entries.
- Unseal is Shamir 5-share / 3-threshold. For persistent and production vaults the ceremony is witnessed and the shares are distributed across independent custody; the ephemeral dev tier is automated (see `docs/operations/openbao-init-runbook.md`).

### Dependencies
- All Go dependencies pinned via `go.sum`; the workspace is a multi-module `go.work`.
- All Helm chart versions pinned; container base images pinned by digest.
- Renovate (`.github/renovate.json`) manages upgrades with `pinDigests: true`.

## Disclosure Policy

- We follow [responsible disclosure](https://en.wikipedia.org/wiki/Responsible_disclosure) practices.
- We will credit reporters in the security advisory unless they prefer to remain anonymous.
- We ask that you give us reasonable time to address the issue before public disclosure.
- We will not take legal action against researchers who follow this policy.

## Security-Related Configuration

For production deployments, ensure:

1. **TLS everywhere**: All endpoints must use HTTPS (cert-manager + Let's Encrypt, or Cloudflare-managed TLS)
2. **Network policies**: deny-all baseline plus explicit allow rules (`infra/helm/network-policies/`)
3. **RBAC**: minimal permissions for service accounts; no unconditional cluster-wide `secrets`/`configmaps` grants (enforced by the `cluster-role-no-secrets-star` gate)
4. **Secrets**: OpenBao Transit + External Secrets Operator — see § Secrets. Storing secrets as plain environment variables is refused by the `no-env-vault-secrets` gate
5. **Image provenance**: images are cosign-signed on publish; admission runs sigstore policy-controller (deny-by-default, with a warn-mode policy during the keyed→keyless cutover)
6. **Pod hardening**: `runAsNonRoot`, `drop: ALL`, `readOnlyRootFilesystem`, PodSecurity `restricted` on tenant namespaces

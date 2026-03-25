# Security Policy

## Reporting a Vulnerability

The LoopZero team takes security vulnerabilities seriously. We appreciate your efforts to responsibly disclose your findings.

**Do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report them via one of the following channels:

1. **GitHub Security Advisories** (preferred): [Report a vulnerability](https://github.com/loopzero/loopzero/security/advisories/new)
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

### Build Pipeline
- **govulncheck** on all Go modules in every CI run
- **npm audit** with `--audit-level=high` on CLI
- **Trivy** filesystem, IaC, and Dockerfile scanning (CRITICAL + HIGH severity)
- **TruffleHog** secret detection in every commit
- **kubeconform --strict** on all Kubernetes manifests

### Runtime
- Non-root containers (UID 1000)
- Multi-stage Docker builds with minimal base images
- Per-tenant isolation via Kubernetes namespaces, RBAC, and NetworkPolicies
- Transaction-scoped advisory locks (`pg_advisory_xact_lock`) for concurrency control
- GitHub token validation with caching

### Dependencies
- All Go dependencies pinned via `go.sum`
- All npm dependencies locked via `package-lock.json`
- All Helm chart versions pinned
- Security overrides applied for known vulnerabilities (e.g., `tar` CVE)

## Disclosure Policy

- We follow [responsible disclosure](https://en.wikipedia.org/wiki/Responsible_disclosure) practices.
- We will credit reporters in the security advisory unless they prefer to remain anonymous.
- We ask that you give us reasonable time to address the issue before public disclosure.
- We will not take legal action against researchers who follow this policy.

## Security-Related Configuration

For production deployments, ensure:

1. **TLS everywhere**: All endpoints must use HTTPS (cert-manager + Let's Encrypt)
2. **Network policies**: Enable Kubernetes NetworkPolicies to restrict pod-to-pod traffic
3. **RBAC**: Use minimal permissions for service accounts
4. **Secrets**: Use Kubernetes Secrets (encrypted at rest) for sensitive configuration
5. **Image provenance**: Verify container image signatures (when available, Sprint 6+)

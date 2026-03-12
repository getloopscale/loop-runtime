# Sprint 6 — Security + Supply Chain

> **Weeks:** 19-21
> **System Design Reference:** §23 (Sprint 6)

---

## Objective

The platform is secure enough for external users. Supply chain integrity verified. Secrets encrypted at rest.

## Product Exit Gate

- Every build produces a signed image + SBOM.
- Secrets are encrypted via KMS.
- Ephemeral credentials expire correctly.

## CLI Commands Delivered

None — this sprint is a security layer.

## Tasks

### Supply chain

```
[ ] cosign image signing in build pipeline
[ ] syft SBOM generation
[ ] Provenance attestation
[ ] Image signature verification on promote
[ ] Git commit integrity (branch protection, bot user)
```

### Secrets security

```
[ ] AppSecretOperator with rotation tracking (workloadHash + hashMatch verification)
[ ] KMS envelope encryption for etcd
[ ] KEK rotation procedure
[ ] 3 consecutive hash verification failures → LoopZeroSecretSyncFailed alert
```

### Credential lifecycle

```
[ ] Ephemeral credential TTL verification
[ ] Gitea minimum version 1.20 documented as infrastructure requirement
[ ] WorkflowTemplate finalize step: explicit Gitea token revocation
[ ] Gitea orphan token sweeper (tokens without active workflow)
```

### API hardening

```
[ ] API rate limiting per tenant
[ ] RBAC: role-based access control (owner, member, deploy-only)
[ ] API key management (for CI/CD, separate from OAuth tokens)
[ ] Platform ResourceQuotas (loopzero-system namespace)
[ ] Gitea rate limiting per tenant
[ ] PostgreSQL connection pool limits
```

### Audit

```
[ ] Audit trail: PostgreSQL append-only + K8s audit log
[ ] Per-tenant cache isolation verification
```

### Tests

```
[ ] Test: build pod killed before finalize → sweeper revokes token within 1 hour
[ ] Test: build token not accessible to other tenants
[ ] Test: unsigned image rejected on promote
[ ] Test: rate limit triggers on burst traffic
```

## Key References in System Design

| Topic | Section |
|-------|---------|
| Security model | §16 |
| Management plane security | §16.1 |
| KMS envelope encryption | §16.1 |
| Break-glass procedure | §16.1 |
| Secret lifecycle | §16.2 |
| RBAC model | §16.3 |
| Supply chain security | §17 |
| Threat model | §17.1 |
| Ephemeral credentials per build | §17.2 |
| Artifact signing and verification | §17.3 |
| Cache isolation | §17.4 |
| Git commit integrity | §17.5 |
| AppSecretOperator hash verification | §7.3.2 |
| Control plane self-protection | §6.3 |
| Compensation: orphan token sweeper | §11.2 |

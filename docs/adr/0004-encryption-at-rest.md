# ADR 0004: Encryption at rest for Kubernetes secrets

## Status

Planned (not yet implemented for local development)

## Context

Kubernetes secrets are stored as base64-encoded values in etcd by default, which provides no encryption at rest. In the LoopZero platform, several credentials are stored as Kubernetes secrets: Gitea API tokens, ArgoCD repository credentials, database passwords, MinIO access keys, and user-defined application secrets managed via `loopzero secret set`.

For production deployments, base64-only storage is insufficient. Any actor with etcd access or etcd backup access can read all secrets in plaintext.

## Decision

### Production deployment

1. Enable `EncryptionConfiguration` on the Kubernetes API server with AES-CBC (minimum) or AES-GCM (preferred) provider, using a key managed outside the cluster (AWS KMS, GCP KMS, or HashiCorp Vault transit).
2. Adopt External Secrets Operator with a `SecretStore` backed by an external secret manager (AWS Secrets Manager, GCP Secret Manager, or HashiCorp Vault). Convert direct `Secret` references to `ExternalSecret` custom resources.
3. Rotate encryption keys on a regular schedule (at minimum quarterly).

### Local development (k3d)

Accept base64-only storage. k3d clusters are ephemeral, contain no real credentials, and are destroyed on `task dev:down`. The risk is acceptable for local development.

## Consequences

- **Production:** secrets are encrypted in etcd. Key rotation is operationally required.
- **Development:** no change. Acceptable risk for ephemeral local clusters.
- **Migration path:** install External Secrets Operator, create `SecretStore` resource, convert existing `Secret` references to `ExternalSecret` CRs. Application code does not change since it reads from the same Kubernetes secret names.
- **Dependency:** External Secrets Operator (Apache 2.0 license, CNCF project, active maintenance).

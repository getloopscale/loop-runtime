# Tenant secrets on OpenBao, on a dedicated cluster

Status: accepted, 2026-11-25, by the maintainer.

## Context

loopruntime-api encrypts every tenant secret (environment variables a customer sets for an
app) with OpenBao's transit engine, one key per tenant, so that deleting a tenant's key
makes its secrets unreadable. Without a transit client the API refuses every secret write
(`SECRETS_BACKEND_UNAVAILABLE`), which is what staging did until this decision
(getloopscale/loop-runtime#103).

The question was where the encryption keys live in LoopScale's production, sized for the
target of 50,000 customers with two to three apps each, about 125,000 apps. LoopScale will
also ship a self-hosted edition, so anything that only exists on AWS has to be replaceable.

## Options

| Option | Cost at 125k apps | Isolation | Lock-in |
|---|---|---|---|
| OpenBao on the core cluster | none extra | an admin of core, or anything that compromises core, reaches the keys | none |
| OpenBao on each cell | N vaults | cells run tenant code; a container escape lands next to the keys; a tenant that moves cells moves vaults | none |
| OpenBao on a dedicated `secrets` cluster | one small EKS cluster, hundreds of USD a month | separate API server, nodes and RBAC | the auto-unseal key only |
| AWS KMS, one key per tenant | about 50,000 USD a month (1 USD per key) | AWS IAM | full |
| AWS Secrets Manager, one secret per app secret | about 500,000 USD a month (0.40 USD per secret) | AWS IAM | full |

Prices are AWS list prices for us-east-1 as published in November 2026.

## Decision

1. Tenant secrets stay on OpenBao transit. Neither KMS nor Secrets Manager holds tenant
   secrets: the cost does not fit the target, and the self-hosted edition could not run
   either.
2. LoopScale's own installations (production and staging) run OpenBao on a dedicated
   `secrets` cluster, HA Raft (three voters in production, one in staging), HTTPS, reached
   from core by loopruntime-api only.
3. OpenBao unseals itself with the `awskms` seal on AWS. The seal is the one AWS-specific
   piece, and OpenBao's seal is pluggable: GCP KMS, Azure Key Vault, PKCS#11 or Shamir
   replace it with a configuration change.
4. A self-hosted installation chooses where OpenBao runs, the core cluster included. The
   API only needs `OPENBAO_VAULT_ADDR`, the AppRole and, optionally, the SPKI pin. Running
   OpenBao on core gives up the isolation of point 2, and the self-hosting guide has to say
   so.

## Consequences

- infra-live gains, per environment, the `compute/eks-secrets`,
  `security/kms-openbao-unseal` and `identity/secrets-openbao` units. The IRSA role grants
  `kms:Encrypt`, `kms:Decrypt` and `kms:DescribeKey` on the unseal key and nothing else.
- `secrets.helmfile.yaml.gotmpl` serves the `staging` and `prod` environments from one
  block. On staging, `infra/staging/openbao.sh` initialises the vault with recovery keys
  after every rebuild and wires loopruntime-api through a selector-less Service on core whose
  endpoints are the secrets cluster's nodes, so the certificate's
  `openbao.secrets-system.svc.cluster.local` name holds end to end.
- The `openbao-bootstrap-in-core-task` gate keeps refusing an OpenBao install on core in
  this repository's tasks, which are LoopScale's installations. A documented single-cluster
  mode for self-hosting, when it exists, is a separate change to that gate.

## Open risk

Nobody has measured how many transit operations per second one Raft cluster serves for
125,000 apps. A load test of the transit path on staging comes before production; until
then the three-voter size is an assumption.

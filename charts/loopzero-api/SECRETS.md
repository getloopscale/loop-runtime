# loopzero-api — Secret manifest (install-config-contract)

The `loopzero-api` chart consumes credentials **by reference**, never inline. Provide a K8s Secret and
point the chart at it with `--set secrets.existingSecret=<name>` (or let the chart create one from
`values.secrets.*` — but populate those via your store, never commit them).

How you populate it is your choice (zero backend lock-in): **ESO** (recommended — see
`examples/eso-{openbao,aws-sm,gcp-sm}.yaml`), CSI driver, sops, sealed-secrets, or `kubectl create secret`.

> Secrets are set **per installation, in your target's native store** (AWS Secrets Manager / GCP Secret
> Manager / OpenBao) — never committed to git or baked into the artifact.

This table is **set-equal** to the rendered Secret keys (hermetic Go guard `TestLoopZeroApiSecretManifest`).

Generate a random hex secret with `openssl rand -hex 32` where indicated.

| Key | Required (prod) | How to obtain | Description |
|---|---|---|---|
| `DATABASE_URL` | Y | postgres conn string | primary PG connection string |
| `LOOPZERO_WORKER_DATABASE_URL` | when-rls | postgres conn string | loopzero_worker RLS-role conn string |
| `LOOPZERO_API_DATABASE_URL` | N | postgres conn string | api-specific DB conn string |
| `GITEA_TOKEN` | Y | gitea API token | Gitea gitops push token |
| `ARGOCD_TOKEN` | Y | argocd account token | ArgoCD API token |
| `MINIO_ACCESS_KEY` | Y | access key | MinIO/S3 access key |
| `MINIO_SECRET_KEY` | Y | openssl rand -hex 20 | MinIO/S3 secret key |
| `LOOPZERO_ARGO_CALLBACK_SECRET` | Y | openssl rand -hex 32 | Argo callback HMAC secret |
| `LOOPZERO_BOOTSTRAP_SECRET` | Y | openssl rand -hex 32 | /internal/bootstrap auth secret |
| `LOOPZERO_SECRET_ENCRYPTION_KEY` | Y | openssl rand -hex 32 | AES-256 secret-at-rest key (hex) |
| `OPENBAO_VAULT_ROLE_ID` | when-transit | from OpenBao AppRole | OpenBao AppRole role_id |
| `OPENBAO_VAULT_SECRET_ID` | when-transit | from OpenBao AppRole | OpenBao AppRole secret_id |
| `CF_API_TOKEN` | Y | from Cloudflare | Cloudflare API token |
| `CORE_KUBECONFIG` | Y | kubeconfig YAML | mgmt cluster kubeconfig |
| `BUILD_KUBECONFIG` | Y | kubeconfig YAML | build cluster kubeconfig |
| `CELL_0_KUBECONFIG` | Y | kubeconfig YAML | runtime cluster kubeconfig |
| `STRIPE_SECRET_KEY` | Y | from Stripe | engine Stripe secret key |
| `GITHUB_APP_PRIVATE_KEY` | Y | GitHub App PEM | GitHub App PKCS1 RSA private key |
| `GITHUB_WEBHOOK_SECRET` | Y | openssl rand -hex 32 | GitHub webhook HMAC secret |
| `LOOPZERO_ADMIN_API_KEY` | when-admin | openssl rand -hex 32 | admin API key |
| `LOOPZERO_ADMIN_API_KEY_PREVIOUS` | N | openssl rand -hex 32 | admin API key (rotation) |
| `LOOPZERO_ADMIN_SERVICE_AUTH_SECRET` | when-admin | openssl rand -hex 32 | admin service-auth secret |
| `LOOPZERO_ADMIN_SERVICE_AUTH_SECRET_PREVIOUS` | N | openssl rand -hex 32 | admin service-auth (rotation) |
| `LOOPZERO_BROKER_TOKEN` | when-broker | openssl rand -hex 32 | ESO-sync broker token |
| `LOOPZERO_EDGE_ROUTE_TOKEN` | when-edge | openssl rand -hex 32 | bearer for /internal/edge/route (edge host → cell lookup) |
| `LOOPZERO_SERVICE_AUTH_SECRET` | Y | openssl rand -hex 32 | cloud->engine shared service-auth secret |
| `LOOPZERO_SERVICE_AUTH_SECRET_PREVIOUS` | N | openssl rand -hex 32 | service-auth secret (rotation) |
| `LOOPZERO_SERVICE_AUTH_NONCE_REDIS_PASSWORD` | when-nonce-redis | redis password | service-auth nonce Redis password |

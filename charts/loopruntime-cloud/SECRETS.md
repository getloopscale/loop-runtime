# loopruntime-cloud — Secret manifest (install-config-contract)

The `loopruntime-cloud` chart consumes its credentials **by reference**, never inline. You provide a
single Kubernetes Secret and point the chart at it with `--set existingSecret=<name>`. How that
Secret is populated is **your choice** — the secret backend is never locked in:

- **ESO** (recommended) — copy one of `examples/eso-{openbao,aws-sm,gcp-sm}.yaml`, set your
  backend paths, `kubectl apply`. The External Secrets Operator materializes the K8s Secret.
- **CSI driver / sops / sealed-secrets / `kubectl create secret`** — anything that produces a
  K8s Secret of the right name + keys works equally.

> Secrets are set **per installation, in your target's native store** (AWS → Secrets Manager,
> GCP → Secret Manager, on-prem/k3d → OpenBao). They are **never** committed to git or baked
> into the artifact.

## Required Secret keys

The Secret named by `existingSecret` MUST carry these keys (the cloud-API reads them as env
vars and fail-fasts at boot when a required one is missing in production):

| Key (env var) | Required | Meaning | How to generate / source |
|---|---|---|---|
| `STRIPE_API_KEY` | prod | Stripe secret key for billing API calls | from your Stripe dashboard (`sk_live_…`) |
| `STRIPE_WEBHOOK_SECRET` | prod | Stripe webhook signing secret (verifies events) | from your Stripe webhook endpoint config (`whsec_…`) |
| `SESSION_TOKEN_SECRET` | prod | HMAC key signing dashboard session tokens (≥32 chars) | `openssl rand -hex 32` |
| `LOOPRUNTIME_SERVICE_AUTH_SECRET` | prod | Shared secret for cloud→engine service-to-service auth (≥32 chars; MUST match the engine's value) | `openssl rand -hex 32` — generate ONCE, share with the engine install |
| `THEMORY_API_KEY` | when `THEMORY_BASE_URL` set | Master bearer the proxy uses against loopruntime-data | from your loopruntime-data deployment |
| `SESSION_TOKEN_SECRET_PREVIOUS` | optional | Previous HMAC key for zero-downtime rotation | the prior `SESSION_TOKEN_SECRET` during a rotation window |

Non-secret config (URLs, replica count, domain) is set in plain `values` — see
`values.example.yaml`. Admin URLs (`KRATOS_ADMIN_URL`, `HYDRA_ADMIN_URL`) are rendered by the
chart from `.Values.config.*` into a defense-in-depth Secret and do NOT need to be in
`existingSecret`.

## Generating the generated secrets

`SESSION_TOKEN_SECRET` and `LOOPRUNTIME_SERVICE_AUTH_SECRET` are values YOU generate (not issued by a
third party). Generate them once per installation and store them in your backend:

```bash
openssl rand -hex 32     # SESSION_TOKEN_SECRET
openssl rand -hex 32     # LOOPRUNTIME_SERVICE_AUTH_SECRET  (must equal the engine's value)
```

`LOOPRUNTIME_SERVICE_AUTH_SECRET` is the one secret shared across the cloud and engine installs — both
sides verify dashboard-proxied requests with it. Generate it once, store it in both installs'
secret stores.

## Quick start (kubectl, simplest backend)

```bash
kubectl create secret generic loopruntime-cloud-secrets \
  --from-literal=STRIPE_API_KEY="sk_live_…" \
  --from-literal=STRIPE_WEBHOOK_SECRET="whsec_…" \
  --from-literal=SESSION_TOKEN_SECRET="$(openssl rand -hex 32)" \
  --from-literal=LOOPRUNTIME_SERVICE_AUTH_SECRET="$(openssl rand -hex 32)" \
  --from-literal=THEMORY_API_KEY="…"

helm install loopruntime-cloud oci://ghcr.io/getloopscale/charts/loopruntime-cloud \
  -f values.example.yaml --set existingSecret=loopruntime-cloud-secrets
```

For production, prefer an ESO example (`examples/eso-*.yaml`) so secrets stay in your managed
store and rotate without `kubectl`.

## Rotation (day-2)

When you rotate a secret in your store, ESO re-syncs the K8s Secret — enable `--set reloader.enabled=true` so the pod restarts and reads the new value (the cloud-API reads config only at boot). See [README.md](README.md) § Day-2.

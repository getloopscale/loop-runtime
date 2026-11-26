# loop-runtime-api Helm chart

The loop-runtime engine API, published as an OCI chart at `oci://ghcr.io/getloopscale/charts/loop-runtime-api`.
This README is the install guide for a `helm pull` operator (install-config-contract).

## Install

```bash
helm install loop-runtime-api oci://ghcr.io/getloopscale/charts/loop-runtime-api \
  -f values-prod.yaml \
  --set secrets.existingSecret=loop-runtime-api-secrets
```

## Two planes: config vs secret

The chart wires its environment in **two distinct planes** — they never share a home:

| Plane | K8s object | Source | Documented in |
|---|---|---|---|
| **Config** (non-secret: URLs, ports, timeouts, flags) | **ConfigMap** (`envFrom configMapRef`) | `values.yaml config:` → layering | **[CONFIG.md](CONFIG.md)** |
| **Secret** (credentials, tokens, keys, connection strings) | **K8s Secret** (`envFrom secretRef`) | your target's native store | **[SECRETS.md](SECRETS.md)** |

Both manifests are **set-equal to what the chart actually renders** — enforced by hermetic Go
guards (`TestLoopRuntimeApiConfigManifest` / `TestLoopRuntimeApiSecretManifest`). A drifted manifest fails CI.

### Config layering (`-f values-<env>.yaml`)

The chart ships sane defaults in `values.yaml`. Compose per-environment config by layering an
overlay — the standard Helm convention. Start from
[`values.example.yaml`](values.example.yaml) (every instance-specific field marked
`!!changeme!!`), copy per environment, override only what differs. `values.schema.json` is
`additionalProperties: false` — a typo'd key **fails fast at `helm install`**.

The full config key list (default / required-prod / public-vs-internal) is in **[CONFIG.md](CONFIG.md)**.

## Secrets — by reference, never inline

Secrets are **never** set in committed values. Provide a Kubernetes Secret and point the chart at
it with `--set secrets.existingSecret=<name>`. Full key list + how to generate each:
**[SECRETS.md](SECRETS.md)**.

How you populate that Secret is your choice (zero secret-backend lock-in):

- **ESO** (recommended) — copy a per-backend example and `kubectl apply`:
  - [`examples/eso-openbao.yaml`](examples/eso-openbao.yaml) — OpenBao / HashiCorp Vault
  - [`examples/eso-aws-sm.yaml`](examples/eso-aws-sm.yaml) — AWS Secrets Manager (IRSA)
  - [`examples/eso-gcp-sm.yaml`](examples/eso-gcp-sm.yaml) — GCP Secret Manager (Workload Identity)
- **CSI driver / sops / sealed-secrets / `kubectl create secret`** — anything producing a K8s
  Secret of the right name + keys works.

Secrets are set **per installation, in your target's native store** — never committed to git.

## Day-2: rotation-aware restarts (opt-in Reloader)

The engine reads its config at boot. When you rotate a secret in your store, ESO re-syncs the K8s
Secret — but the running pod keeps the old value until restarted. Enable
[Stakater Reloader](https://github.com/stakater/Reloader):

```bash
helm install loop-runtime-api oci://… --set secrets.existingSecret=loop-runtime-api-secrets --set reloader.enabled=true
```

`reloader.enabled=true` stamps `reloader.stakater.com/auto: "true"` on the Deployment. With
`auto`, Reloader watches **ALL** referenced Secrets AND ConfigMaps (both planes) — so a config-only
change also triggers a rolling restart. The **Reloader controller is operator-installed**
(recommended, not bundled) — the annotation is inert without it, and `reloader.enabled` is **off by
default** (default render is byte-stable).

## What this chart deploys

The engine API Deployment + Service (+ ServiceAccount, NetworkPolicy, PDB, ServiceMonitor, ConfigMap,
Secret). Pin the image by digest in production rather than the floating tag.

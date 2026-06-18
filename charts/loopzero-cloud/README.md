# loopzero-cloud Helm chart

The LoopZeroCloud cloud-API + dashboard, published as an OCI chart at
`oci://ghcr.io/getloopscale/charts/loopzero-cloud`. This README is the install guide for a
`helm pull` operator (install-config-contract).

## Install

```bash
helm install loopzero-cloud oci://ghcr.io/getloopscale/charts/loopzero-cloud \
  -f values-prod.yaml \
  --set existingSecret=loopzero-cloud-secrets
```

### Config layering (`-f values-<env>.yaml`)

The chart ships sane defaults in `values.yaml`. Compose your per-environment config by
layering an overlay on top — the standard Helm convention:

```bash
helm install loopzero-cloud oci://… -f values.example.yaml          # generic starting point
helm install loopzero-cloud oci://… -f values-prod.yaml             # your prod overlay
helm install loopzero-cloud oci://… -f values-staging.yaml          # your staging overlay
```

Start from `values.example.yaml` (commented, every instance-specific field marked
`!!changeme!!`), copy it per environment, and override only what differs. The chart's
`values.schema.json` is `additionalProperties: false` — a typo'd or unknown key **fails
fast at `helm install`** instead of silently rendering a broken pod.

## Secrets — by reference, never inline

Secrets are **never** set in values. You provide a Kubernetes Secret and point the chart at
it with `--set existingSecret=<name>`. Full key list + how to generate each secret:
**[SECRETS.md](SECRETS.md)**.

How you populate that Secret is your choice (zero secret-backend lock-in):

- **ESO** (recommended) — copy a per-backend example and `kubectl apply`:
  - [`examples/eso-openbao.yaml`](examples/eso-openbao.yaml) — OpenBao / HashiCorp Vault
  - [`examples/eso-aws-sm.yaml`](examples/eso-aws-sm.yaml) — AWS Secrets Manager (IRSA)
  - [`examples/eso-gcp-sm.yaml`](examples/eso-gcp-sm.yaml) — GCP Secret Manager (Workload Identity)
- **CSI driver / sops / sealed-secrets / `kubectl create secret`** — anything that produces a
  K8s Secret of the right name + keys works.

Secrets are set **per installation, in your target's native store** — never committed to git.

## Day-2: rotation-aware restarts (opt-in Reloader)

The cloud-API reads its config at boot. When you rotate a secret in your store, ESO re-syncs
the K8s Secret — but the running pod keeps the old value until restarted. Enable
[Stakater Reloader](https://github.com/stakater/Reloader) integration so a rotation triggers
a rolling restart automatically:

```bash
helm install loopzero-cloud oci://… --set existingSecret=loopzero-cloud-secrets --set reloader.enabled=true
```

`reloader.enabled=true` stamps `reloader.stakater.com/auto: "true"` on the Deployment. The
**Reloader controller is operator-installed** (recommended, not bundled) — the annotation is
inert without it, and `reloader.enabled` is **off by default**.

## What this chart deploys

The cloud-API Deployment + Service (+ ServiceAccount, NetworkPolicy, PDB, ServiceMonitor).
Pin the image by digest in production (`image.digest`) rather than the floating tag.

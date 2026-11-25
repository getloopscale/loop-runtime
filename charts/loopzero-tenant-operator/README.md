# loopzero-tenant-operator Helm chart

loopzero tenant operator — install guide (install-config-contract Slice 4).

## Config + secrets (two planes)

Non-secret config → **ConfigMap** (`envFrom configMapRef`), documented in **[CONFIG.md](CONFIG.md)**, set per env via `-f values-<env>.yaml`. `values.schema.json` is `additionalProperties:false` (fail-fast).

Secrets → referenced **K8s Secret** (`--set secrets.existingSecret=<name>`), documented in **[SECRETS.md](SECRETS.md)** — never inline. Populate via ESO (`examples/eso-*.yaml`) / CSI / sops / kubectl, per-install in your native store.

## Day-2 (opt-in Reloader)

`--set reloader.enabled=true` stamps `reloader.stakater.com/auto: "true"` on the Deployment so a rotated Secret/ConfigMap triggers a rolling restart (operator-installed controller; off by default).

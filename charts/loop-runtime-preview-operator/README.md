# loop-runtime-preview-operator Helm chart

loop-runtime preview operator — install guide (install-config-contract Slice 4).

## Config (config-only)

Non-secret config → **ConfigMap** (`envFrom configMapRef`), documented in **[CONFIG.md](CONFIG.md)**, set per env via `-f values-<env>.yaml`. `values.schema.json` is `additionalProperties:false` (fail-fast).

This operator reads **no chart secret**. Its runtime kubeconfig is mounted as a volume from the platform Secret `loop-runtime-cluster-kubeconfigs` (not a chart knob).

## Day-2 (opt-in Reloader)

`--set reloader.enabled=true` stamps `reloader.stakater.com/auto: "true"` on the Deployment so a rotated ConfigMap triggers a rolling restart (operator-installed controller; off by default).

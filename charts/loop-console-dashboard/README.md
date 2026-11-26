# loop-console-dashboard

The LoopRuntime dashboard SPA (static, nginx-served). Install-config-contract surface: runtime
config only (no chart secret) + a fail-fast `values.schema.json`.

## Install

```bash
helm install loop-console-dashboard oci://ghcr.io/getloopscale/charts/loop-console-dashboard \
  --namespace <namespace> \
  -f values-<env>.yaml
```

## Runtime config (`__config.js`)

The SPA learns where the API/cloud endpoints live by reading
`/usr/share/nginx/html/__config.js` (a ConfigMap volume) at page load. The chart renders
that file from `.Values.config.*` at install time. See [`CONFIG.md`](CONFIG.md) for the
keys (`VITE_API_URL` / `VITE_CLOUD_URL` / `BASE_DOMAIN`). Changing a value requires a chart
upgrade (the ConfigMap is regenerated).

## Schema fail-fast

`values.schema.json` rejects unknown top-level keys (`additionalProperties:false`): a
typo'd Helm value fails `helm install` instead of being silently ignored.

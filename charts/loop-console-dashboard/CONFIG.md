# loop-console-dashboard — Config manifest (install-config-contract)

Runtime configuration the `loop-console-dashboard` chart renders into `__config.js` (a ConfigMap
mounted at `/usr/share/nginx/html/__config.js`, read by the SPA at page load — see
`templates/configmap.yaml`). The dashboard is a static SPA; it reads **no chart secret**
(config-only).

Keys are documented in UPPER_SNAKE env form (the canonical install surface); each maps to a
`.Values.config.<camelCase>` Helm value (the hermetic Go guard asserts the mapping via
`camelAPI`).

| Key | Default | Description |
|---|---|---|
| `VITE_API_URL` | (empty) | engine/API base URL injected into `__config.js` (`.Values.config.viteApiUrl`) |
| `VITE_AUTH_URL` | (empty) | Kratos base URL, `AUTH_URL` in `__config.js`; empty falls back to the API's (`.Values.config.viteAuthUrl`) |
| `VITE_ACCOUNTS_URL` | (empty) | base URL password recovery starts on, `ACCOUNTS_URL` in `__config.js` (`.Values.config.viteAccountsUrl`) |
| `VITE_OAUTH_URL` | (empty) | the OAuth2 server's public origin (Hydra), `OAUTH_URL` in `__config.js`; the CLI device flow redirects there (`.Values.config.viteOauthUrl`) |
| `CSP_CONNECT_ORIGINS` | `[]` | extra `connect-src` origins written to `csp-origins.conf`: the Kratos public host and the control plane (`.Values.config.cspConnectOrigins`) |
| `CSP_FORM_ORIGINS` | `[]` | extra `form-action` origins written to `csp-origins.conf`: the Kratos public host the forms post to (`.Values.config.cspFormOrigins`) |

## Runtime mechanism

These values are rendered at `helm install` time into the `__config.js` ConfigMap and
mounted into the nginx-served SPA. The browser reads `window.__LOOPSCALE_CONFIG__` at page load
to discover where the API, Kratos and Hydra live. Changing a value requires a chart upgrade
(the ConfigMap is regenerated); opt-in Reloader is not applicable to a static SPA bundle.

## Schema fail-fast

`values.schema.json` rejects unknown top-level keys (`additionalProperties:false`).

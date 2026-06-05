// All URLs relative — same-origin via Caddy reverse-proxy on app.loopzero.example.
//
// Caddy routes (infra/local/Caddyfile):
//   /sessions/*      → kratos (Host: auth.loopzero.example rewrite)
//   /self-service/*  → kratos (Host: auth.loopzero.example rewrite)
//   /v1/*            → vite proxy → cloud
//   /engine/*        → vite proxy → cloud → loopzero-api
//   /                → vite SPA
//
// SECURITY (2026-06-01 mandate): same-origin XHR sends cookies unconditionally,
// removing all SameSite=Lax cross-subdomain ambiguity. Kratos serve.public.base_url
// stays auth.loopzero.example so flow.ui.action URLs are absolute to the canonical
// host — login/register submits use top-level form POST (Lax cookies travel on
// top-level navigation regardless of cross-origin) and OIDC callbacks land on
// auth.loopzero.example/self-service/methods/oidc/callback/* matching the registered
// redirect URIs in GitHub + Google OAuth Clients.
//
// In prod, docker-entrypoint.sh regenerates this from env vars.
window.__LOOPZERO_CONFIG__ = {
  API_URL: "",
  ENGINE_URL: "/engine",
  AUTH_URL: "",
  ACCOUNTS_URL: ""
};

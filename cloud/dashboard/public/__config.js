// Dev: same-origin via vite proxy (see vite.config.ts server.proxy).
// In prod, docker-entrypoint.sh regenerates this from env vars.
window.__LOOPZERO_CONFIG__ = {
  API_URL: "",
  ENGINE_URL: "/engine",
  AUTH_URL: "",
  ACCOUNTS_URL: ""
};

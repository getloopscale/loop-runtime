// Runtime configuration — overwritten by container entrypoint.
// In development, these are empty (same-origin).
// In production, the entrypoint generates this file from env vars.
window.__LOOPZERO_CONFIG__ = {
  API_URL: "",
  ENGINE_URL: "",
  AUTH_URL: "",
  ACCOUNTS_URL: "",
};

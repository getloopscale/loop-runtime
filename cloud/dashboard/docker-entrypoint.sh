#!/bin/sh
# Generate runtime config from environment variables.
# This replaces build-time VITE_* injection, making the image
# environment-independent (same commit = same artifact).
cat > /usr/share/nginx/html/__config.js << JSEOF
window.__LOOPZERO_CONFIG__ = {
  API_URL: "${VITE_API_URL:-}",
  ENGINE_URL: "${VITE_ENGINE_URL:-}",
  AUTH_URL: "${VITE_AUTH_URL:-}",
  ACCOUNTS_URL: "${VITE_ACCOUNTS_URL:-}",
};
JSEOF

exec "$@"

#!/bin/sh
# Generate runtime config from environment variables.
# This replaces build-time VITE_* injection, making the image
# environment-independent (same commit = same artifact).

# Validate a runtime config URL before writing it into client-served JS.
# A value is either empty (same-origin default) or a clean http(s) URL with no
# characters that could terminate the JS string literal and inject code into
# every browser loading the dashboard (find_infra_005). Fail closed on bad input.
validate_url() {
  name="$1"
  value="$2"
  [ -z "$value" ] && return 0
  case "$value" in
    http://*|https://*) ;;
    *)
      echo "FATAL: $name must be an http(s) URL (got: $value)" >&2
      exit 1
      ;;
  esac
  # Reject anything that could break out of the JS double-quoted string or inject.
  case "$value" in
    *'"'* | *"'"* | *'`'* | *';'* | *'<'* | *'>'* | *' '* | *'\'* | *'$'* | *'{'* | *'}'* | *'('* | *')'*)
      echo "FATAL: $name contains illegal characters (got: $value)" >&2
      exit 1
      ;;
  esac
}

validate_url VITE_API_URL "${VITE_API_URL:-}"
validate_url VITE_ENGINE_URL "${VITE_ENGINE_URL:-}"
validate_url VITE_AUTH_URL "${VITE_AUTH_URL:-}"
validate_url VITE_ACCOUNTS_URL "${VITE_ACCOUNTS_URL:-}"

cat > /usr/share/nginx/html/__config.js << JSEOF
window.__LOOPZERO_CONFIG__ = {
  API_URL: "${VITE_API_URL:-}",
  ENGINE_URL: "${VITE_ENGINE_URL:-}",
  AUTH_URL: "${VITE_AUTH_URL:-}",
  ACCOUNTS_URL: "${VITE_ACCOUNTS_URL:-}",
};
JSEOF

exec "$@"

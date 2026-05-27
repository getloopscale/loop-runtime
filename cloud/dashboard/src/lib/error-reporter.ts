// T0.1 Error Reporter — receives errors from RouteBoundary / SheetBoundary
// and dispatches to /v1/telemetry endpoint. Rate-limited to prevent error
// storms that would flood the telemetry endpoint (EC-13 risk acknowledged).
//
// Invariant: this function MUST NEVER throw. A throw here would cascade
// the boundary into infinite loop (boundary catches, calls onError, which
// throws, which triggers boundary again).

interface ErrorInfo {
  componentStack?: string | null;
  routePath?: string;
}

const MAX_ERRORS_PER_SESSION = 10;
let errorsThisSession = 0;

export function resetErrorReporter(): void {
  errorsThisSession = 0;
}

export function reportError(error: Error, info: ErrorInfo): void {
  try {
    if (errorsThisSession >= MAX_ERRORS_PER_SESSION) return;
    errorsThisSession++;

    const payload = {
      type: 'react-error' as const,
      message: error.message,
      stack: error.stack ?? null,
      componentStack: info.componentStack ?? null,
      routePath: info.routePath ?? null,
      timestamp: Date.now(),
      userAgent: typeof navigator !== 'undefined' ? navigator.userAgent : null,
    };

    if (import.meta.env.DEV) {
      // eslint-disable-next-line no-console
      console.error('[error-reporter]', payload);
      return;
    }

    // Prod: use sendBeacon when available so dispatch survives page unload.
    // Fall back to fetch with keepalive. Both MUST be wrapped in try/catch
    // since they can throw (e.g., CSP block, malformed URL).
    try {
      const body = JSON.stringify(payload);
      if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
        navigator.sendBeacon('/v1/telemetry/errors', body);
      } else if (typeof fetch !== 'undefined') {
        fetch('/v1/telemetry/errors', {
          method: 'POST',
          body,
          headers: { 'Content-Type': 'application/json' },
          keepalive: true,
        }).catch(() => {});
      }
    } catch {
      // Transport failed — never propagate.
    }
  } catch {
    // Any unexpected error in the reporter itself — swallow. The boundary
    // already showed fallback to the user; failing to report telemetry is
    // a non-fatal observability gap, not a user-facing concern.
  }
}

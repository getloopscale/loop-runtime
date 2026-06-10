import { useEffect, useMemo, useRef, useState } from 'react';
import { CheckCircle2 } from 'lucide-react';
import { Card } from '@loopzerokit/ui';

// Plan 2026-06-05-dashboard-journey-j05-device-success T1.2 D4:
// Telemetry endpoint path — reuses the canonical /v1/telemetry/events handler
// (cloud/internal/routes/telemetry.go) instead of a dedicated /v1/telemetry/event
// route. Backend recognizes `event=device_success_viewed` payload and increments
// `loopzero_device_flow_browser_success_total`. Path exported so tests can assert.
export const BEACON_PATH = '/v1/telemetry/events';

const AUTO_CLOSE_MS = 5000;
const BLOCKED_CHECK_DELAY_MS = 100;
const COUNTDOWN_TICK_MS = 1000;

// NOTE: page MUST NOT read or display query params — user_code/device_code are
// post-flow PII that should never appear here. The page is a pure confirmation
// surface (EC-13 documented). Backend layer is the source of truth for the
// device-code/user-code lifecycle.
export function DeviceSuccessPage() {
  const [blocked, setBlocked] = useState(false);
  const [countdown, setCountdown] = useState(Math.ceil(AUTO_CLOSE_MS / 1000));

  const reducedMotion = useMemo(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return false;
    }
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  }, []);

  // EC-1/EC-6: persistent refs so cleanup cancels EVERY queued callback even when
  // the user clicks "Back to dashboard" before the 5s window elapses. Without this
  // a setCountdown call would land on an unmounted component and React 18 would
  // warn (dev) or leak the closure (prod).
  const countdownIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const closeTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const blockedCheckRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;

    // Telemetry beacon — fire-and-forget. Graceful-degrade if sendBeacon missing.
    try {
      const beacon = navigator.sendBeacon?.bind(navigator);
      if (beacon) {
        const payload = JSON.stringify({
          event: 'device_success_viewed',
          timestamp: Date.now(),
          origin: window.location.origin,
        });
        beacon(BEACON_PATH, payload);
      }
    } catch {
      // sendBeacon never throws per spec; defensive only.
    }

    if (!reducedMotion) {
      countdownIntervalRef.current = setInterval(() => {
        if (!mountedRef.current) return;
        setCountdown((value) => (value > 0 ? value - 1 : 0));
      }, COUNTDOWN_TICK_MS);
    }

    closeTimeoutRef.current = setTimeout(() => {
      if (!mountedRef.current) return;
      try {
        window.close();
      } catch {
        // window.close never throws; defensive only.
      }
      blockedCheckRef.current = setTimeout(() => {
        if (!mountedRef.current) return;
        if (!window.closed) {
          setBlocked(true);
        }
      }, BLOCKED_CHECK_DELAY_MS);
    }, AUTO_CLOSE_MS);

    return () => {
      mountedRef.current = false;
      if (countdownIntervalRef.current) clearInterval(countdownIntervalRef.current);
      if (closeTimeoutRef.current) clearTimeout(closeTimeoutRef.current);
      if (blockedCheckRef.current) clearTimeout(blockedCheckRef.current);
    };
  }, [reducedMotion]);

  return (
    <div className="min-h-screen bg-background flex items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <Card.Body className="p-8 text-center space-y-4">
          <CheckCircle2
            className="size-12 text-success mx-auto"
            aria-hidden="true"
          />
          <div className="space-y-1">
            <h1 className="font-display text-title-sm font-semibold text-foreground">
              CLI authorized
            </h1>
            <p className="text-body-sm text-muted-foreground">
              You can close this tab and return to your terminal.
            </p>
          </div>

          {!reducedMotion && !blocked && (
            <p
              data-testid="device-success-countdown"
              className="text-body-xs text-muted-foreground"
              aria-live="polite"
            >
              Closing in {countdown}s
            </p>
          )}

          {blocked && (
            <p
              data-testid="device-success-fallback"
              className="text-body-xs text-muted-foreground"
              aria-live="polite"
            >
              You can close this tab now.
            </p>
          )}

          <a
            href="/"
            data-testid="device-success-back-link"
            className="inline-block text-body-sm font-medium text-primary hover:underline"
          >
            Back to dashboard
          </a>
        </Card.Body>
      </Card>
    </div>
  );
}

import { Button } from '@loopzero/ui';
import { sanitizeReturnTo } from '@/lib/auth/return-to';

/**
 * NetworkRetryButton — CTA to retry the login flow after a network /
 * service-degraded failure. Plan v1.1 Phase 2 Task T2.3.
 *
 * EC-3 absorption (OWASP A01): the `returnTo` prop is sanitized via
 * `sanitizeReturnTo()` before being forwarded to the caller. The caller
 * NEVER sees the raw query param — defense-in-depth even if the caller
 * forgets to sanitize at its own boundary.
 */
export interface NetworkRetryButtonProps {
  onRetry: (sanitizedReturnTo: string) => void;
  returnTo: string | null;
  pending?: boolean;
}

export function NetworkRetryButton({ onRetry, returnTo, pending }: NetworkRetryButtonProps) {
  const handleClick = () => {
    onRetry(sanitizeReturnTo(returnTo));
  };
  return (
    <Button variant="secondary" size="sm" onClick={handleClick} disabled={pending}>
      Try again
    </Button>
  );
}

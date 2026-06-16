import { Alert, Button } from '@loopzero/ui';
import type { KratosErrorMapped } from '@/lib/auth/error-mapper';

/**
 * LoginErrorBanner — surfaces the product-language tuple from
 * `mapKratosError()` to the login UI. Plan v1.1 Phase 2 Task T2.1.
 *
 * Per ADR D6 of plan 2026-06-16-m0-j01-login-polish (component extraction
 * mandatory to keep login.tsx under 500 LOC budget): this component owns
 * the error-display concern; login.tsx orchestrates state but renders the
 * banner here.
 *
 * Uses @loopzero/ui <Alert intent="destructive"> for the canonical
 * destructive-state visual + role="alert" semantics (WCAG ARIA — assistive
 * tech announces immediately).
 */
export interface LoginErrorBannerProps {
  error: KratosErrorMapped;
  onRetry?: () => void;
}

export function LoginErrorBanner({ error, onRetry }: LoginErrorBannerProps) {
  return (
    <Alert
      intent="destructive"
      title={error.message}
      description={error.suggestion}
      action={
        error.retryable && onRetry ? (
          <Button variant="secondary" size="sm" onClick={onRetry}>
            Try again
          </Button>
        ) : undefined
      }
    />
  );
}

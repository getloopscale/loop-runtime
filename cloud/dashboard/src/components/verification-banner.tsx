import { Alert, Button } from '@loopzero/ui';
import { getVerificationURL } from '@/lib/auth';

/**
 * Non-dismissible banner shown when user's email is not verified.
 * Uses @loopzero/ui <Alert intent="warning"> primitive (Brief #3, shipped
 * 0.9.0-next.0). 27 LOC custom → 11 LOC composition per D-6 v1.2 ADR.
 *
 * Links to Kratos self-service verification flow.
 */
export function VerificationBanner() {
  return (
    <Alert
      intent="warning"
      title="Verify your email"
      description="Check your inbox for a verification code. You can browse, but you can't deploy until you verify."
      action={
        <Button variant="secondary" size="sm" asChild>
          <a href={getVerificationURL()}>Verify now</a>
        </Button>
      }
    />
  );
}

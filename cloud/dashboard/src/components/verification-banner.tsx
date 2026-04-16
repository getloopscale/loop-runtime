import { AlertTriangle, ArrowRight } from 'lucide-react';
import { getVerificationURL } from '@/lib/auth';

/**
 * Non-dismissible banner shown when user's email is not verified.
 * Links to Kratos self-service verification flow.
 */
export function VerificationBanner() {
  return (
    <div className="mb-6 flex items-center justify-between rounded-lg border border-warning/20 bg-warning/5 px-4 py-3">
      <div className="flex items-center gap-3">
        <AlertTriangle className="h-4 w-4 text-warning flex-shrink-0" />
        <div>
          <p className="text-sm font-medium text-warning">Verify your email</p>
          <p className="text-xs text-warning/60">Check your inbox for a verification code. Some features are disabled until verified.</p>
        </div>
      </div>
      <a
        href={getVerificationURL()}
        className="flex items-center gap-1.5 rounded-md bg-warning/10 px-3 py-1.5 text-xs font-medium text-warning hover:bg-warning/20 transition-colors flex-shrink-0"
      >
        Verify now
        <ArrowRight className="h-3 w-3" />
      </a>
    </div>
  );
}

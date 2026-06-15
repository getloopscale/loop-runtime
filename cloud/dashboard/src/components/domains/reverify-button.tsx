import { useRef } from 'react';
import { Button } from '@loopzero/ui';
import { RefreshCw } from 'lucide-react';
import { useVerifyDomain } from '@/hooks/mutations/use-domains';
import type { DomainStatus } from '@/lib/api/types';

/**
 * J-11 Domains plan v1.1 T2.3 — ReverifyButton.
 *
 * Renders ONLY for domains in `failed` or `verified` (i.e., Degraded-equivalent)
 * status per ADR D3 — clicking forces backend POST /verify which re-checks
 * CF zone + cert-manager Certificate.
 *
 * Reuses the canonical `useVerifyDomain` mutation already shipped at
 * `cloud/dashboard/src/hooks/mutations/use-domains.ts`. No new hook needed
 * (per ADR D6 EC-2 recon).
 *
 * EC-4 absorption (ADR D7): client-side debounce via `mutation.isPending`
 * gating — the button is disabled while a reverify is in flight, so rapid
 * double-clicks fire the mutation exactly once. Server-side rate limit
 * (1/30s via existing rateLimitMiddlewares per router.go:486) catches any
 * bypass attempts.
 */
export interface ReverifyButtonProps {
  /** Canonical domain name (registered TLD + subdomain). */
  domainName: string;
  /** Current domain status — drives render-or-hide per ADR D3. */
  status: DomainStatus;
}

/**
 * Status values that warrant a Reverify CTA. Active = healthy (no need);
 * removing = mid-delete (no point); pending_verification / provisioning =
 * the polling hook handles the wait state (no manual nudge needed).
 *
 * `failed` is the canonical "user must intervene" state — show Reverify.
 * `verified` here approximates the "Degraded" plan-v1 wording: CF zone
 * verified but Certificate didn't issue cleanly. Re-running /verify is
 * the canonical user-side action.
 */
const REVERIFY_ELIGIBLE_STATUSES: ReadonlySet<DomainStatus> = new Set<DomainStatus>([
  'failed',
  'verified',
]);

export function ReverifyButton({ domainName, status }: ReverifyButtonProps) {
  const verify = useVerifyDomain();
  // Ref-based guard for synchronous double-click protection. `isPending`
  // from useMutation is async (React state update); a user clicking the
  // button 3 times within one event-loop tick would all see isPending=false
  // until the next render. The ref blocks the 2nd+ click synchronously
  // (EC-4 absorption).
  const inFlightRef = useRef(false);
  if (!REVERIFY_ELIGIBLE_STATUSES.has(status)) {
    return null;
  }
  const isPending = verify.isPending;
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      onClick={() => {
        if (inFlightRef.current || isPending) return; // EC-4 debounce
        inFlightRef.current = true;
        verify.mutate(domainName, {
          onSettled: () => {
            inFlightRef.current = false;
          },
        });
      }}
      disabled={isPending}
      aria-label={`Re-verify ${domainName}`}
    >
      <RefreshCw
        className={`size-4 ${isPending ? 'animate-spin' : ''}`}
        aria-hidden="true"
      />
      <span>{isPending ? 'Re-verifying…' : 'Re-verify'}</span>
    </Button>
  );
}

/** Exported for hermetic tests so the eligible status set is auditable. */
export const __test_REVERIFY_ELIGIBLE_STATUSES = REVERIFY_ELIGIBLE_STATUSES;

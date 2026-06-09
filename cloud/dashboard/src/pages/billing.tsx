import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowUpRight, Brain, Loader2, Sparkles } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  PageShell,
  PlanBadge,
  UsageMeter,
  type PlanTier,
  type UsageMetric as UsageMeterMetric,
} from '@loopzerokit/ui';
import { ContentGrid } from '@/components/layout';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useCloudApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';
import { formatDate, formatNumber } from '@/lib/format';

interface UsageMetric {
  used: number;
  limit: number | null;
}

interface BillingResponse {
  plan: string;
  team: string;
  usage: Record<string, UsageMetric>;
  period_start?: string;
  period_end?: string;
  subscription_id?: string;
}

/**
 * §3.4: Post-checkout activation polling.
 *
 * Detects activation via `subscription_id` transition rather than `plan` label
 * change. Plan-label comparison fails when baseline equals target (e.g. user
 * already on "Pro" upgrades again after a cancellation): the new
 * subscription_id is unique per Stripe checkout session, so any signup or
 * re-purchase produces a diff regardless of the resulting plan name.
 */
function useCheckoutActivation(initialBilling: BillingResponse | null | undefined) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [activating, setActivating] = useState(false);
  const [activationResult, setActivationResult] = useState<'success' | 'timeout' | null>(null);
  const [activatedBilling, setActivatedBilling] = useState<BillingResponse | null>(null);

  useEffect(() => {
    if (searchParams.get('success') !== 'true') return;
    if (initialBilling === undefined) return;

    setActivating(true);
    let attempts = 0;
    const maxAttempts = 15;
    const baselineSubscriptionID = initialBilling?.subscription_id ?? '';
    const baselinePlan = initialBilling?.plan ?? '';

    const poll = setInterval(async () => {
      attempts++;
      try {
        const result = await cloudFetch<BillingResponse>('/v1/billing');
        const currentSubscriptionID = result?.subscription_id ?? '';
        const subscriptionChanged =
          currentSubscriptionID !== '' && currentSubscriptionID !== baselineSubscriptionID;
        const planChanged = result?.plan && result.plan !== baselinePlan;
        if (result && (subscriptionChanged || planChanged)) {
          clearInterval(poll);
          setActivating(false);
          setActivatedBilling(result);
          setActivationResult('success');
          setSearchParams({}, { replace: true });
        }
      } catch {
        /* continue polling */
      }
      if (attempts >= maxAttempts) {
        clearInterval(poll);
        setActivating(false);
        setActivationResult('timeout');
        setSearchParams({}, { replace: true });
      }
    }, 2000);

    return () => clearInterval(poll);
  }, [searchParams, setSearchParams, initialBilling]);

  return { activating, activationResult, setActivationResult, activatedBilling };
}

const PLAN_TIER_MAP: Record<string, PlanTier> = {
  starter: 'free',
  free: 'free',
  hobby: 'hobby',
  pro: 'pro',
  team: 'team',
  enterprise: 'enterprise',
};

/**
 * BillingPage redesign per plan v1.2 T-E.3 + EC-4 + EC-5.
 * EC-5: Invoice Table OMITTED — no /v1/billing/invoices endpoint exists
 * in cloud BillingHandler. Defer to follow-up plan when endpoint shipped.
 * EC-4: Plan/period metrics from /v1/billing; usage zeroed via placeholder
 * if backend response lacks usage (mockup spec allows).
 */
export function BillingPage() {
  useRequireAuth();
  const { setPageTitle } = usePageMeta();

  const { data: initialBilling, loading } = useCloudApi<BillingResponse>('/v1/billing');
  const {
    activating,
    activationResult,
    setActivationResult,
    activatedBilling,
  } = useCheckoutActivation(initialBilling);

  const billing = activatedBilling ?? initialBilling;

  const [actionLoading, setActionLoading] = useState(false);
  const [actionError, setActionError] = useState('');

  const openPortal = async () => {
    setActionLoading(true);
    setActionError('');
    try {
      const portal = await cloudFetch<{ url: string }>('/v1/billing/portal');
      if (portal?.url) {
        window.open(portal.url, '_blank');
      } else {
        setActionError('Failed to open billing portal. Please try again.');
      }
    } catch {
      setActionError('Failed to open billing portal. Please try again.');
    } finally {
      setActionLoading(false);
    }
  };

  const upgradePlan = async (plan: string) => {
    setActionLoading(true);
    setActionError('');
    try {
      const result = await cloudFetch<{ url: string }>('/v1/billing/checkout', {
        method: 'POST',
        body: JSON.stringify({ plan }),
      });
      if (result?.url) {
        window.location.href = result.url;
      } else {
        setActionError('Failed to start checkout. Please try again.');
      }
    } catch {
      setActionError('Failed to start checkout. Please try again.');
    } finally {
      setActionLoading(false);
    }
  };

  const usageMetrics: UsageMeterMetric[] = billing?.usage
    ? Object.entries(billing.usage).map(([key, data]) => ({
        label: key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
        value: data.used,
        max: data.limit ?? Number.MAX_SAFE_INTEGER, // EC-5: unlimited
        unit: key.includes('minute') ? 'min' : key.includes('mb') ? 'MB' : '',
        // T-C.2 P-1/P-2: use formatNumber for consistent thousand-separators +
        // compact-millions on Edge Requests / large counters. ADR-D1.bis for null limit.
        formatter: (v) => {
          if (data.limit === null) return `${formatNumber(v, 'grouped')} / Unlimited`;
          const useMillions = data.limit >= 1_000_000;
          return useMillions
            ? `${formatNumber(v, 'grouped')} / ${formatNumber(data.limit, 'compact')}`
            : `${formatNumber(v, 'grouped')} / ${formatNumber(data.limit, 'grouped')}`;
        },
      }))
    : [];

  // Normalize plan to lowercase: backend returns display-cased values via
  // PlanDisplayName ("Pro" / "Team") while PLAN_TIER_MAP keys are slug-cased.
  // Without toLowerCase, lookup("Pro") returned undefined, fell through to
  // 'free', and a Pro user saw the Starter UI (Free badge + Pro upgrade CTA).
  const planTier: PlanTier =
    PLAN_TIER_MAP[(billing?.plan ?? 'starter').toLowerCase()] ?? 'free';
  const planLabel = billing?.plan
    ? billing.plan.charAt(0).toUpperCase() + billing.plan.slice(1)
    : 'Starter';

  return (
    <PageShell title="Billing" onTitleChange={setPageTitle} loading={loading}>
      {/* Activation state alerts (§3.4 preserved) */}
      {activating && (
        <Alert
          intent="info"
          title="Activating your subscription..."
          description="This usually takes a few seconds."
          className="mb-4"
        />
      )}
      {activationResult === 'success' && (
        <Alert
          intent="success"
          title={`Subscription activated! Welcome to ${billing?.plan ?? 'your plan'}.`}
          onDismiss={() => setActivationResult(null)}
          className="mb-4"
        />
      )}
      {activationResult === 'timeout' && (
        <Alert
          intent="warning"
          title="Taking longer than expected."
          description="Your subscription is being processed. Refresh the page in a moment, or contact support if it doesn't update within a few minutes."
          className="mb-4"
        />
      )}
      {actionError && (
        <Alert intent="destructive" title={actionError} className="mb-4" />
      )}

      <ContentGrid columns="2-asymmetric" gap={6}>
        {/* Left: Plan summary */}
        <Card>
          <Card.Body className="p-6">
            <div className="flex items-start justify-between mb-4">
              <div className="space-y-2">
                <p className="text-label-caps text-muted-foreground uppercase tracking-wider">
                  Current Plan
                </p>
                <div className="flex items-center gap-3">
                  <h2 className="font-display text-display-md text-foreground">
                    {planLabel}
                  </h2>
                  <PlanBadge plan={planTier} />
                </div>
                {billing?.team && (
                  <p className="text-body-sm text-muted-foreground">{billing.team}</p>
                )}
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={openPortal}
                disabled={actionLoading}
                className="gap-1.5"
              >
                {actionLoading ? (
                  <Loader2 className="size-3.5 animate-spin" aria-hidden="true" />
                ) : (
                  <>
                    Manage Plan
                    <ArrowUpRight className="size-3.5" aria-hidden="true" />
                  </>
                )}
              </Button>
            </div>
            {/* T-DR-1.3 v1.2 (closes P2-1): quota summary inline. Backend
                /v1/billing.payment_method.last4 + period_end RESCOPED per
                Phase 0 T-DR-0.1 verdict. Quota lines come from billing.usage. */}
            {billing?.usage && Object.keys(billing.usage).length > 0 && (
              <div className="border-t border-border pt-4 mt-2 space-y-1.5">
                <p className="text-label-caps text-muted-foreground uppercase tracking-wider mb-2">
                  Includes
                </p>
                {Object.entries(billing.usage).slice(0, 4).map(([key, data]) => (
                  <div key={key} className="flex justify-between text-body-sm">
                    <span className="text-muted-foreground">{key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())}</span>
                    <span className="font-mono text-foreground tabular-nums">
                      {data.limit === null ? 'Unlimited' : formatNumber(data.limit, 'grouped')}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </Card.Body>
        </Card>

        {/* Right: Upgrade / Contact CTA — varies by current plan.
            free (starter):       Upgrade to Pro via Stripe checkout
            pro:                  Contact sales for Team / Enterprise (Team is
                                  Early Access, no public signup; Enterprise is
                                  Contact-only — see CLAUDE.md § Pricing)
            team / enterprise:    no card (already on a top tier)
        */}
        {planTier === 'free' && (
          <Card
            className="cursor-pointer hover:border-primary/30 transition-colors"
            onClick={() => upgradePlan('pro')}
          >
            <Card.Body className="p-6">
              <Sparkles className="size-5 text-primary mb-3" aria-hidden="true" />
              <p className="text-body-sm font-semibold text-foreground">
                Upgrade to Pro — $29/mo
              </p>
              <p className="text-label text-muted-foreground mt-1">
                10 projects, 3 environments, 1000 builds/month
              </p>
            </Card.Body>
          </Card>
        )}
        {planTier === 'pro' && (
          <Card
            className="cursor-pointer hover:border-primary/30 transition-colors"
            onClick={() => upgradePlan('team')}
          >
            <Card.Body className="p-6">
              <Sparkles className="size-5 text-primary mb-3" aria-hidden="true" />
              <p className="text-body-sm font-semibold text-foreground">
                Upgrade to Team — $99/mo
              </p>
              <p className="text-label text-muted-foreground mt-1">
                A whole team or department
              </p>
              <ul className="text-label text-muted-foreground mt-3 space-y-1.5">
                <li>Unlimited projects · unlimited environments · metered builds/mo (no agent quota)</li>
                <li>Per-role permissions: Admin · Deploy · View</li>
                <li>Compliance report per build (SBOM)</li>
                <li>Priority support</li>
              </ul>
            </Card.Body>
          </Card>
        )}
      </ContentGrid>

      {/* Usage */}
      {usageMetrics.length > 0 && (
        <section className="mt-6">
          <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
            Usage This Period
          </p>
          <UsageMeter
            title={
              billing?.period_start && billing?.period_end
                ? `${formatDate(billing.period_start, 'date')} – ${formatDate(billing.period_end, 'date')}`
                : 'Current period'
            }
            metrics={usageMetrics}
          />
        </section>
      )}

      {/* Plan 4 of dashboard-memory-section-roadmap.md, T4.2 — Memory promotion.
          ADR D1: rendered always; graceful empty state when no memory activity.
          NOT a regression to existing billing flow — purely additive. */}
      <MemoryUsageCard />
    </PageShell>
  );
}

interface MemoryMetricsResponse {
  total_memories: number;
  retrieval_api_usage: number;
  add_events: number;
  entity_count: number;
}

/**
 * MemoryUsageCard — Plan 4 cross-cut from dashboard-memory-section-roadmap.md.
 *
 * Reads `/v1/memory/metrics` (Plan 1 endpoint). On error OR all-zero, renders
 * a "no activity yet" state with a Get-started link. On populated, renders 2
 * figures (Total memories + Retrieval API calls) + a link to /memory.
 *
 * D1 invariant: this card is the OPT-IN signal — the gracefulness of the empty
 * state is what avoids surfacing Memory to customers who don't use it.
 */
function MemoryUsageCard() {
  const { data, error } = useCloudApi<MemoryMetricsResponse>('/v1/memory/metrics');
  const empty = error || !data || (data.total_memories === 0 && data.retrieval_api_usage === 0);

  return (
    <section className="mt-6" data-testid="billing-memory-card">
      <p className="text-label-caps text-muted-foreground uppercase mb-3 tracking-wider">
        Memory
      </p>
      <Card>
        <Card.Body className="p-6">
          <div className="flex items-start justify-between gap-4">
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Brain className="size-5 text-primary" aria-hidden="true" />
                <h3 className="text-body font-semibold text-foreground">
                  {empty ? 'No memory activity yet' : 'Memory usage'}
                </h3>
              </div>
              {empty ? (
                <p className="text-body-sm text-muted-foreground max-w-md">
                  Once your agent starts remembering, you&apos;ll see counts here.
                  Use the SDK to record your first memory.
                </p>
              ) : (
                <div className="flex gap-6 pt-2">
                  <div>
                    <p className="text-label-caps text-muted-foreground uppercase tracking-wider">
                      Total memories
                    </p>
                    <p className="font-display text-display-md tabular-nums">{data!.total_memories}</p>
                  </div>
                  <div>
                    <p className="text-label-caps text-muted-foreground uppercase tracking-wider">
                      Retrieval calls
                    </p>
                    <p className="font-display text-display-md tabular-nums">{data!.retrieval_api_usage}</p>
                  </div>
                </div>
              )}
            </div>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => (window.location.href = '/memory')}
              className="gap-1.5 shrink-0"
            >
              {empty ? 'Get started' : 'Open Memory'}
              <ArrowUpRight className="size-3.5" aria-hidden="true" />
            </Button>
          </div>
        </Card.Body>
      </Card>
    </section>
  );
}

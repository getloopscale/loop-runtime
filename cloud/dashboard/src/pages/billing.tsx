import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ArrowUpRight, Loader2, Sparkles } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  PlanBadge,
  UsageMeter,
  type PlanTier,
  type UsageMetric as UsageMeterMetric,
} from '@loopzero/ui';
import { ContentGrid } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
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
}

/** §3.4: Post-checkout activation polling. Preserves prior behavior. */
function useCheckoutActivation(currentPlan: string | undefined) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [activating, setActivating] = useState(false);
  const [activationResult, setActivationResult] = useState<'success' | 'timeout' | null>(null);
  const [activatedBilling, setActivatedBilling] = useState<BillingResponse | null>(null);

  useEffect(() => {
    if (searchParams.get('success') !== 'true') return;
    if (currentPlan === undefined) return;

    setActivating(true);
    let attempts = 0;
    const maxAttempts = 15;
    const baselinePlan = currentPlan;

    const poll = setInterval(async () => {
      attempts++;
      try {
        const result = await cloudFetch<BillingResponse>('/v1/billing');
        if (result && result.plan !== baselinePlan) {
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
  }, [searchParams, setSearchParams, currentPlan]);

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
  useSetPageTitle('Billing');

  const { data: initialBilling, loading } = useCloudApi<BillingResponse>('/v1/billing');
  const {
    activating,
    activationResult,
    setActivationResult,
    activatedBilling,
  } = useCheckoutActivation(initialBilling?.plan);

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

  const planTier: PlanTier = PLAN_TIER_MAP[billing?.plan ?? 'starter'] ?? 'free';
  const planLabel = billing?.plan
    ? billing.plan.charAt(0).toUpperCase() + billing.plan.slice(1)
    : 'Starter';

  if (loading) {
    return (
      <Card>
        <Card.Body className="py-16 text-center">
          <Loader2
            className="mx-auto size-6 text-muted-foreground animate-spin"
            aria-label="Loading billing"
          />
        </Card.Body>
      </Card>
    );
  }

  return (
    <>
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
                  <h2 className="font-display text-display-md font-bold text-foreground">
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
          </Card.Body>
        </Card>

        {/* Right: Upgrade CTA (only show if not on top tier) */}
        {planTier !== 'enterprise' && planTier !== 'team' && (
          <Card
            className="cursor-pointer hover:border-primary/30 transition-colors"
            onClick={() => upgradePlan('pro')}
          >
            <Card.Body className="p-6">
              <Sparkles
                className="size-5 text-primary mb-3"
                aria-hidden="true"
              />
              <p className="text-body-sm font-semibold text-foreground">
                Upgrade to Pro — $19/mo
              </p>
              <p className="text-label text-muted-foreground mt-1">
                10 projects, 3 environments, 1000 builds/month
              </p>
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
    </>
  );
}

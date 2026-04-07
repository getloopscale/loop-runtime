import { useState, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import { ArrowUpRight, Sparkles, Loader2, CheckCircle2, Clock } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';
import { useCloudApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';

interface UsageMetric {
  used: number;
  limit: number | null; // §1.5: null = unlimited
}

interface BillingResponse {
  plan: string;
  team: string;
  usage: Record<string, UsageMetric>;
  period_start?: string;
  period_end?: string;
}

// §3.4: Post-checkout activation polling.
// Returns updated billing data when activation succeeds, so the page renders fresh state.
// Compares against the actual pre-checkout plan (captured on first poll), not a hardcoded value.
function useCheckoutActivation(currentPlan: string | undefined) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [activating, setActivating] = useState(false);
  const [activationResult, setActivationResult] = useState<'success' | 'timeout' | null>(null);
  const [activatedBilling, setActivatedBilling] = useState<BillingResponse | null>(null);

  useEffect(() => {
    if (searchParams.get('success') !== 'true') return;

    setActivating(true);
    let attempts = 0;
    const maxAttempts = 15; // 30 seconds at 2s intervals
    // Use the page's current plan as baseline (not hardcoded 'Starter').
    // This handles Pro→Team upgrades and revisits correctly.
    const baselinePlan = currentPlan || '';

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
      } catch { /* continue polling */ }

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

export function BillingPage() {
  useRequireAuth();
  const { data: initialBilling, loading } = useCloudApi<BillingResponse>('/v1/billing');
  const { activating, activationResult, setActivationResult, activatedBilling } = useCheckoutActivation(initialBilling?.plan);

  // Use activated billing data if available (fresher than initial fetch)
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

  const usage = billing ? Object.entries(billing.usage).map(([metric, data]) => ({
    metric: metric.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase()),
    used: data.used,
    limit: data.limit,
    unit: metric.includes('minute') ? 'min' : metric.includes('mb') ? 'MB' : '',
  })) : [];

  if (loading) {
    return <PageWrapper title="Billing"><Card><CardContent className="py-16 text-center"><Loader2 className="mx-auto h-6 w-6 text-white/20 animate-spin" /></CardContent></Card></PageWrapper>;
  }

  return (
    <PageWrapper title="Billing" description="Plan, usage, and payment.">
      {/* §3.4: Post-checkout activation states */}
      {activating && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="mb-4 rounded-lg bg-accent/10 border border-accent/20 p-4 flex items-center gap-3">
          <Loader2 className="h-5 w-5 text-accent animate-spin flex-shrink-0" />
          <div>
            <p className="text-sm font-medium text-white">Activating your subscription...</p>
            <p className="text-xs text-white/40 mt-0.5">This usually takes a few seconds.</p>
          </div>
        </motion.div>
      )}
      {activationResult === 'success' && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="mb-4 rounded-lg bg-green-500/10 border border-green-500/20 p-4 flex items-center gap-3">
          <CheckCircle2 className="h-5 w-5 text-green-400 flex-shrink-0" />
          <div>
            <p className="text-sm font-medium text-white">Subscription activated! Welcome to {billing?.plan}.</p>
          </div>
          <button onClick={() => setActivationResult(null)} className="ml-auto text-white/30 hover:text-white/60 text-xs">Dismiss</button>
        </motion.div>
      )}
      {activationResult === 'timeout' && (
        <motion.div initial={{ opacity: 0, y: -10 }} animate={{ opacity: 1, y: 0 }}
          className="mb-4 rounded-lg bg-yellow-500/10 border border-yellow-500/20 p-4 flex items-center gap-3">
          <Clock className="h-5 w-5 text-yellow-400 flex-shrink-0" />
          <div>
            <p className="text-sm font-medium text-white">Taking longer than expected.</p>
            <p className="text-xs text-white/40 mt-0.5">Your subscription is being processed. Refresh the page in a moment, or contact support if it doesn't update within a few minutes.</p>
          </div>
        </motion.div>
      )}

      {actionError && (
        <div className="mb-4 rounded-lg bg-danger/10 border border-danger/20 p-3 text-sm text-danger">
          {actionError}
        </div>
      )}
      <div className="grid grid-cols-3 gap-4 mb-6">
        {/* Current Plan */}
        <motion.div
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          className="col-span-2"
        >
          <Card className="relative overflow-hidden">
            <div className="absolute top-0 right-0 w-64 h-64 bg-accent/[0.03] rounded-full -translate-y-32 translate-x-32 blur-3xl" />
            <CardContent className="p-6 relative">
              <div className="flex items-start justify-between">
                <div>
                  <p className="text-sm text-white/40 mb-1">Current Plan</p>
                  <h2 className="font-display text-3xl font-bold text-white tracking-tight">{billing?.plan || 'Starter'}</h2>
                  <p className="text-sm text-white/30 mt-1">{billing?.team || ''}</p>
                </div>
                <Button variant="secondary" size="sm" className="gap-1.5" onClick={openPortal} disabled={actionLoading}>
                  {actionLoading ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <>Manage Plan <ArrowUpRight className="h-3.5 w-3.5" /></>}
                </Button>
              </div>
            </CardContent>
          </Card>
        </motion.div>

        {/* Upgrade CTA */}
        <motion.div
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.1 }}
        >
          <Card className="h-full border-accent/10 hover:border-accent/20 cursor-pointer group" onClick={() => upgradePlan('pro')}>
            <CardContent className="p-6 flex flex-col justify-center h-full">
              <Sparkles className="h-5 w-5 text-accent/60 mb-3 group-hover:text-accent transition-colors" />
              <p className="text-sm font-semibold text-white/70 group-hover:text-white transition-colors">Upgrade to Pro — $19/mo</p>
              <p className="text-xs text-white/30 mt-1">10 projects, 3 environments, 1000 builds/month</p>
            </CardContent>
          </Card>
        </motion.div>
      </div>

      {/* Usage */}
      <motion.div
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ delay: 0.2 }}
      >
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Usage This Period</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-5">
              {usage.map((item, i) => {
                const isUnlimited = item.limit === null;
                const pct = isUnlimited ? 0 : Math.min((item.used / (item.limit ?? 1)) * 100, 100);
                const isHigh = !isUnlimited && pct > 80;
                return (
                  <motion.div
                    key={item.metric}
                    initial={{ opacity: 0, x: -10 }}
                    animate={{ opacity: 1, x: 0 }}
                    transition={{ delay: 0.3 + i * 0.06 }}
                  >
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-sm text-white/60">{item.metric}</span>
                      <span className="font-mono text-sm text-white/80">
                        {item.used}{item.unit} <span className="text-white/20">/ {isUnlimited ? '∞' : `${item.limit}${item.unit}`}</span>
                      </span>
                    </div>
                    {!isUnlimited && (
                      <div className="h-1.5 rounded-full bg-surface-4 overflow-hidden">
                        <motion.div
                          className={`h-full rounded-full ${isHigh ? 'bg-warning' : 'bg-accent/60'}`}
                          initial={{ width: 0 }}
                          animate={{ width: `${pct}%` }}
                          transition={{ delay: 0.5 + i * 0.1, duration: 0.8, ease: [0.25, 0.46, 0.45, 0.94] }}
                        />
                      </div>
                    )}
                  </motion.div>
                );
              })}
            </div>
          </CardContent>
        </Card>
      </motion.div>
    </PageWrapper>
  );
}

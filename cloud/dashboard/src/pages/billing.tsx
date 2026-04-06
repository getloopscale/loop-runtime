import { motion } from 'framer-motion';
import { ArrowUpRight, Sparkles, Loader2 } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';
import { useCloudApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';

interface UsageMetric {
  used: number;
  limit: number;
}

interface BillingResponse {
  plan: string;
  team: string;
  usage: Record<string, UsageMetric>;
  period_start?: string;
  period_end?: string;
}

export function BillingPage() {
  useRequireAuth();
  const { data: billing, loading } = useCloudApi<BillingResponse>('/v1/billing');

  const openPortal = async () => {
    const portal = await cloudFetch<{ url: string }>('/v1/billing/portal');
    if (portal?.url) window.open(portal.url, '_blank');
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
                <Button variant="secondary" size="sm" className="gap-1.5" onClick={openPortal}>
                  Manage Plan
                  <ArrowUpRight className="h-3.5 w-3.5" />
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
          <Card className="h-full border-accent/10 hover:border-accent/20 cursor-pointer group">
            <CardContent className="p-6 flex flex-col justify-center h-full">
              <Sparkles className="h-5 w-5 text-accent/60 mb-3 group-hover:text-accent transition-colors" />
              <p className="text-sm font-semibold text-white/70 group-hover:text-white transition-colors">Upgrade to Team</p>
              <p className="text-xs text-white/30 mt-1">Preview environments, more seats, priority builds</p>
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
                const pct = Math.min((item.used / item.limit) * 100, 100);
                const isHigh = pct > 80;
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
                        {item.used}{item.unit} <span className="text-white/20">/ {item.limit >= 999999 ? '∞' : `${item.limit}${item.unit}`}</span>
                      </span>
                    </div>
                    <div className="h-1.5 rounded-full bg-surface-4 overflow-hidden">
                      <motion.div
                        className={`h-full rounded-full ${isHigh ? 'bg-warning' : 'bg-accent/60'}`}
                        initial={{ width: 0 }}
                        animate={{ width: `${pct}%` }}
                        transition={{ delay: 0.5 + i * 0.1, duration: 0.8, ease: [0.25, 0.46, 0.45, 0.94] }}
                      />
                    </div>
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

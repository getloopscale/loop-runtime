import { motion } from 'framer-motion';
import { ArrowUpRight, Box, Clock, Zap } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { PageWrapper } from '@/components/page-wrapper';

const stats = [
  { label: 'Projects', value: '3', icon: Box, change: '+1 this week' },
  { label: 'Deploys', value: '47', icon: Zap, change: '12 today' },
  { label: 'Avg Build', value: '24s', icon: Clock, change: '-3s vs last week' },
];

const recentDeploys = [
  { project: 'acme-api', env: 'production', status: 'live', time: '2m ago', duration: '24s' },
  { project: 'acme-web', env: 'production', status: 'live', time: '5m ago', duration: '18s' },
  { project: 'acme-worker', env: 'staging', status: 'live', time: '1h ago', duration: '12s' },
  { project: 'acme-api', env: 'staging', status: 'failed', time: '2h ago', duration: '31s' },
];

const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.08 } },
};

const item = {
  hidden: { opacity: 0, y: 10 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: [0.25, 0.46, 0.45, 0.94] } },
};

export function OverviewPage() {
  return (
    <PageWrapper title="Overview" description="Your deployment activity at a glance.">
      {/* Stats */}
      <motion.div variants={container} initial="hidden" animate="show" className="grid grid-cols-3 gap-4 mb-8">
        {stats.map((stat) => (
          <motion.div key={stat.label} variants={item}>
            <Card className="group cursor-default">
              <CardContent className="p-5">
                <div className="flex items-center justify-between mb-3">
                  <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-white/[0.04] border border-white/[0.06] group-hover:border-accent/20 group-hover:bg-accent/5 transition-all duration-300">
                    <stat.icon className="h-4 w-4 text-white/30 group-hover:text-accent transition-colors duration-300" />
                  </div>
                  <ArrowUpRight className="h-3.5 w-3.5 text-white/10 group-hover:text-white/30 transition-colors" />
                </div>
                <div className="font-display text-3xl font-bold tracking-tight text-white">{stat.value}</div>
                <div className="mt-1 flex items-center gap-2">
                  <span className="text-sm text-white/40">{stat.label}</span>
                  <span className="text-xs text-accent/60">{stat.change}</span>
                </div>
              </CardContent>
            </Card>
          </motion.div>
        ))}
      </motion.div>

      {/* Recent Deploys */}
      <motion.div initial={{ opacity: 0, y: 10 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.3, duration: 0.4 }}>
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Recent Deploys</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-0 -mt-2">
              {recentDeploys.map((deploy, i) => (
                <motion.div
                  key={i}
                  initial={{ opacity: 0, x: -10 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ delay: 0.4 + i * 0.06 }}
                  className="group flex items-center justify-between py-3 border-b border-white/[0.04] last:border-0 hover:bg-white/[0.02] -mx-6 px-6 transition-colors cursor-pointer"
                >
                  <div className="flex items-center gap-4">
                    <div className={`h-2 w-2 rounded-full ${deploy.status === 'live' ? 'bg-accent animate-pulse-subtle' : 'bg-danger'}`} />
                    <div>
                      <span className="font-mono text-sm text-white/80">{deploy.project}</span>
                      <span className="mx-2 text-white/10">/</span>
                      <span className="text-sm text-white/40">{deploy.env}</span>
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <Badge variant={deploy.status === 'live' ? 'success' : 'danger'}>
                      {deploy.status}
                    </Badge>
                    <span className="font-mono text-xs text-white/20 w-12 text-right">{deploy.duration}</span>
                    <span className="text-xs text-white/20 w-16 text-right">{deploy.time}</span>
                  </div>
                </motion.div>
              ))}
            </div>
          </CardContent>
        </Card>
      </motion.div>
    </PageWrapper>
  );
}

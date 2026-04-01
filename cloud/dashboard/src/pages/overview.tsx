import { motion } from 'framer-motion';
import { ArrowUpRight, Box, Clock, Zap, Loader2 } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { PageWrapper } from '@/components/page-wrapper';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';

interface ProjectsResponse { projects: { name: string }[] }
interface BuildsResponse { builds: { project: string; environment: string; status: string; created_at: string; duration_seconds?: number }[] }

const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.08 } },
};

const item = {
  hidden: { opacity: 0, y: 10 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: [0.25, 0.46, 0.45, 0.94] } },
};

export function OverviewPage() {
  useRequireAuth();
  const { data: projectsData, loading: loadingProjects } = useEngineApi<ProjectsResponse>('/api/v1/projects');
  const { data: buildsData, loading: loadingBuilds } = useEngineApi<BuildsResponse>('/api/v1/builds');

  const projectCount = projectsData?.projects?.length ?? 0;
  const builds = buildsData?.builds ?? [];
  const totalBuilds = builds.length;
  const avgDuration = builds.length > 0
    ? Math.round(builds.reduce((sum, b) => sum + (b.duration_seconds || 0), 0) / builds.length)
    : 0;

  const stats = [
    { label: 'Projects', value: String(projectCount), icon: Box, change: '' },
    { label: 'Deploys', value: String(totalBuilds), icon: Zap, change: '' },
    { label: 'Avg Build', value: avgDuration > 0 ? `${avgDuration}s` : '-', icon: Clock, change: '' },
  ];

  const recentDeploys = builds.slice(0, 5).map(b => ({
    project: b.project,
    env: b.environment,
    status: b.status === 'SUCCEEDED' ? 'live' : b.status === 'FAILED' ? 'failed' : 'building',
    time: timeAgo(b.created_at),
    duration: b.duration_seconds ? `${b.duration_seconds}s` : '-',
  }));

  const loading = loadingProjects || loadingBuilds;
  if (loading) {
    return <PageWrapper title="Overview"><Card><CardContent className="py-16 text-center"><Loader2 className="mx-auto h-6 w-6 text-white/20 animate-spin" /></CardContent></Card></PageWrapper>;
  }

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

function timeAgo(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

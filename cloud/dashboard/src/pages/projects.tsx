import { motion } from 'framer-motion';
import { FolderKanban, Globe, Clock, ArrowRight } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { PageWrapper } from '@/components/page-wrapper';

const projects = [
  {
    name: 'acme-saas',
    apps: [
      { name: 'api', type: 'server', framework: 'express', status: 'live' },
      { name: 'web', type: 'frontend', framework: 'vite', status: 'live' },
      { name: 'worker', type: 'worker', framework: 'node', status: 'live' },
    ],
    lastDeploy: '2m ago',
    env: 'production',
  },
  {
    name: 'internal-tools',
    apps: [
      { name: 'admin', type: 'server', framework: 'go', status: 'live' },
    ],
    lastDeploy: '3h ago',
    env: 'staging',
  },
];

const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.1 } },
};

const item = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: [0.25, 0.46, 0.45, 0.94] } },
};

export function ProjectsPage() {
  return (
    <PageWrapper title="Projects" description="All projects in your team.">
      {projects.length === 0 ? (
        <Card>
          <CardContent className="py-16 text-center">
            <FolderKanban className="mx-auto h-10 w-10 text-white/10 mb-4" />
            <p className="text-white/40 mb-2">No projects yet</p>
            <p className="text-sm text-white/20">
              Run <code className="font-mono bg-surface-3 px-2 py-0.5 rounded text-accent/70">loopzero init</code> to create your first project
            </p>
          </CardContent>
        </Card>
      ) : (
        <motion.div variants={container} initial="hidden" animate="show" className="space-y-4">
          {projects.map((project) => (
            <motion.div key={project.name} variants={item}>
              <Card className="group cursor-pointer hover:border-accent/20">
                <CardContent className="p-0">
                  <div className="flex items-center justify-between p-5 pb-4">
                    <div className="flex items-center gap-3">
                      <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/5 border border-accent/10">
                        <FolderKanban className="h-4 w-4 text-accent/60" />
                      </div>
                      <div>
                        <h3 className="font-mono text-sm font-semibold text-white">{project.name}</h3>
                        <div className="flex items-center gap-2 mt-0.5">
                          <Globe className="h-3 w-3 text-white/20" />
                          <span className="text-xs text-white/30">{project.env}</span>
                          <span className="text-white/10">·</span>
                          <Clock className="h-3 w-3 text-white/20" />
                          <span className="text-xs text-white/30">{project.lastDeploy}</span>
                        </div>
                      </div>
                    </div>
                    <ArrowRight className="h-4 w-4 text-white/10 group-hover:text-white/30 group-hover:translate-x-0.5 transition-all" />
                  </div>

                  <div className="border-t border-white/[0.04] px-5 py-3 flex gap-2">
                    {project.apps.map((app) => (
                      <div key={app.name} className="flex items-center gap-2 rounded-md bg-surface-3/50 px-2.5 py-1.5">
                        <div className={`h-1.5 w-1.5 rounded-full ${app.status === 'live' ? 'bg-accent' : 'bg-danger'}`} />
                        <span className="font-mono text-xs text-white/50">{app.name}</span>
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">{app.framework}</Badge>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            </motion.div>
          ))}
        </motion.div>
      )}
    </PageWrapper>
  );
}

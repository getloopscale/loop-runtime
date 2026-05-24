import { motion } from 'framer-motion';
import { FolderKanban, Globe, Clock, ArrowRight, Loader2 } from 'lucide-react';
import { Card } from '@loopzero/ui';
import { Badge } from '@loopzero/ui';
import { PageWrapper } from '@/components/page-wrapper';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';

interface Project {
  name: string;
  apps?: { name: string; type: string; framework: string; status: string }[];
  last_deploy?: string;
  environment?: string;
  created_at?: string;
}

interface ProjectsResponse {
  projects: Project[];
}

const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.1 } },
};

const item = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: [0.25, 0.46, 0.45, 0.94] } },
};

export function ProjectsPage() {
  useRequireAuth();
  const { data, loading } = useEngineApi<ProjectsResponse>('/api/v1/projects');
  const projects = data?.projects || [];

  return (
    <PageWrapper title="Projects" description="All projects in your team.">
      {loading ? (
        <Card><Card.Body className="py-16 text-center"><Loader2 className="mx-auto h-6 w-6 text-white/20 animate-spin" /></Card.Body></Card>
      ) : projects.length === 0 ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <FolderKanban className="mx-auto h-10 w-10 text-white/10 mb-4" />
            <p className="text-white/40 mb-2">No projects yet</p>
            <p className="text-sm text-white/20">
              Run <code className="font-mono bg-surface-3 px-2 py-0.5 rounded-sm text-accent/70">loopzero init</code> to create your first project
            </p>
          </Card.Body>
        </Card>
      ) : (
        <motion.div variants={container} initial="hidden" animate="show" className="space-y-4">
          {projects.map((project) => (
            <motion.div key={project.name} variants={item}>
              <Card className="group cursor-pointer hover:border-accent/20">
                <Card.Body className="p-0">
                  <div className="flex items-center justify-between p-5 pb-4">
                    <div className="flex items-center gap-3">
                      <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/5 border border-accent/10">
                        <FolderKanban className="h-4 w-4 text-accent/60" />
                      </div>
                      <div>
                        <h3 className="font-mono text-sm font-semibold text-white">{project.name}</h3>
                        <div className="flex items-center gap-2 mt-0.5">
                          <Globe className="h-3 w-3 text-white/20" />
                          <span className="text-xs text-white/30">{project.environment ?? 'production'}</span>
                          <span className="text-white/10">·</span>
                          <Clock className="h-3 w-3 text-white/20" />
                          <span className="text-xs text-white/30">{project.last_deploy ?? '—'}</span>
                        </div>
                      </div>
                    </div>
                    <ArrowRight className="h-4 w-4 text-white/10 group-hover:text-white/30 group-hover:translate-x-0.5 transition-all" />
                  </div>

                  <div className="border-t border-white/4 px-5 py-3 flex gap-2">
                    {(project.apps ?? []).map((app) => (
                      <div key={app.name} className="flex items-center gap-2 rounded-md bg-surface-3/50 px-2.5 py-1.5">
                        <div className={`h-1.5 w-1.5 rounded-full ${app.status === 'live' ? 'bg-accent' : 'bg-danger'}`} />
                        <span className="font-mono text-xs text-white/50">{app.name}</span>
                        <Badge variant="outline" className="text-[10px] px-1.5 py-0">{app.framework}</Badge>
                      </div>
                    ))}
                  </div>
                </Card.Body>
              </Card>
            </motion.div>
          ))}
        </motion.div>
      )}
    </PageWrapper>
  );
}

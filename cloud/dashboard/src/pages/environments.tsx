import { motion } from 'framer-motion';
import { Globe, Shield, Lock } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { PageWrapper } from '@/components/page-wrapper';

const environments = [
  { name: 'production', status: 'active', projects: 2, protected: true, domain: '*.loopzero.example' },
  { name: 'staging', status: 'active', projects: 2, protected: false, domain: '*.staging.loopzero.example' },
];

export function EnvironmentsPage() {
  return (
    <PageWrapper title="Environments" description="Isolated deployment targets.">
      <div className="space-y-3">
        {environments.map((env, i) => (
          <motion.div
            key={env.name}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: i * 0.08, duration: 0.35 }}
          >
            <Card className="group">
              <CardContent className="p-5 flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-surface-3 border border-white/[0.06]">
                    <Globe className="h-4.5 w-4.5 text-white/30" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="font-mono text-sm font-semibold text-white">{env.name}</h3>
                      {env.protected && (
                        <Lock className="h-3 w-3 text-warning/60" />
                      )}
                    </div>
                    <p className="text-xs text-white/30 mt-0.5 font-mono">{env.domain}</p>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  <span className="text-xs text-white/30">{env.projects} projects</span>
                  <Badge variant="success">
                    <span className="h-1.5 w-1.5 rounded-full bg-accent" />
                    {env.status}
                  </Badge>
                  {env.protected && (
                    <Badge variant="warning">
                      <Shield className="h-3 w-3" />
                      protected
                    </Badge>
                  )}
                </div>
              </CardContent>
            </Card>
          </motion.div>
        ))}
      </div>
    </PageWrapper>
  );
}

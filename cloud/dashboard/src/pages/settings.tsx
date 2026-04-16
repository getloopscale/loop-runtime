import { motion } from 'framer-motion';
import { AlertTriangle, Terminal } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';

export function SettingsPage() {
  return (
    <PageWrapper title="Settings" description="Team configuration and danger zone.">
      <div className="space-y-6">
        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
          <Card>
            <CardHeader>
              <CardTitle className="text-base">CLI-First</CardTitle>
              <CardDescription>Most settings are managed via the CLI for maximum control.</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="rounded-lg bg-surface-0 border border-white/[0.06] p-4 font-mono text-sm">
                <div className="flex items-center gap-2 text-white/30 mb-3">
                  <Terminal className="h-3.5 w-3.5" />
                  <span className="text-xs">terminal</span>
                </div>
                <div className="space-y-1 text-white/50">
                  <p><span className="text-accent/70">$</span> loopzero whoami</p>
                  <p><span className="text-accent/70">$</span> loopzero team list</p>
                  <p><span className="text-accent/70">$</span> loopzero secret set KEY=value</p>
                  <p><span className="text-accent/70">$</span> loopzero env create staging</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.15 }}>
          <Card className="border-danger/10">
            <CardHeader>
              <CardTitle className="text-base text-danger flex items-center gap-2">
                <AlertTriangle className="h-4 w-4" />
                Danger Zone
              </CardTitle>
              <CardDescription>Irreversible actions. Proceed with extreme caution.</CardDescription>
            </CardHeader>
            <CardContent>
              <div className="flex items-center justify-between p-4 rounded-lg border border-danger/10 bg-danger/[0.03]">
                <div>
                  <p className="text-sm font-medium text-white/70">Delete Team</p>
                  <p className="text-xs text-white/30 mt-0.5">Permanently delete this team and all its projects</p>
                </div>
                <Button variant="danger" size="sm">Delete Team</Button>
              </div>
            </CardContent>
          </Card>
        </motion.div>
      </div>
    </PageWrapper>
  );
}

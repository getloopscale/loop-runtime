import { motion } from 'framer-motion';
import { AlertTriangle, Terminal } from 'lucide-react';
import { Card } from '@loopzero/ui';
import { Button } from '@loopzero/ui';
import { PageWrapper } from '@/components/page-wrapper';

export function SettingsPage() {
  return (
    <PageWrapper title="Settings" description="Team configuration and danger zone.">
      <div className="space-y-6">
        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }}>
          <Card>
            <Card.Header>
              <Card.Title className="text-base">CLI-First</Card.Title>
              <Card.Description>Most settings are managed via the CLI for maximum control.</Card.Description>
            </Card.Header>
            <Card.Body>
              <div className="rounded-lg bg-surface-0 border border-white/6 p-4 font-mono text-sm">
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
            </Card.Body>
          </Card>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 8 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.15 }}>
          <Card className="border-danger/10">
            <Card.Header>
              <Card.Title className="text-base text-danger flex items-center gap-2">
                <AlertTriangle className="h-4 w-4" />
                Danger Zone
              </Card.Title>
              <Card.Description>Irreversible actions. Proceed with extreme caution.</Card.Description>
            </Card.Header>
            <Card.Body>
              <div className="flex items-center justify-between p-4 rounded-lg border border-danger/10 bg-danger/3">
                <div>
                  <p className="text-sm font-medium text-white/70">Delete Team</p>
                  <p className="text-xs text-white/30 mt-0.5">Permanently delete this team and all its projects</p>
                </div>
                <Button variant="destructive" size="sm">Delete Team</Button>
              </div>
            </Card.Body>
          </Card>
        </motion.div>
      </div>
    </PageWrapper>
  );
}

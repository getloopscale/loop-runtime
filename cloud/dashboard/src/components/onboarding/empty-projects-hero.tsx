import { Rocket } from 'lucide-react';
import { Button, Card, CodeBlock, CopyButton } from '@loopzerokit/ui';

/**
 * EmptyProjectsHero (Sprint 3 T3.3) — Vercel-style empty-state-as-onboarding.
 *
 * Per ADR D2: empty Projects page IS the first-time UX, não modal jail. User
 * vê 3 starter templates + CLI command + "Create from dashboard" CTA.
 *
 * Anti-pattern avoided: "Sign up for tour" / "Watch demo" buttons. User wants
 * to ship; show how.
 */

interface Template {
  name: string;
  description: string;
  cliCommand: string;
}

const TEMPLATES: Template[] = [
  { name: 'Next.js', description: 'React framework with SSR', cliCommand: 'loopzero init nextjs-starter' },
  { name: 'Express', description: 'Node.js API server', cliCommand: 'loopzero init express-starter' },
  { name: 'Go', description: 'Lightweight HTTP service', cliCommand: 'loopzero init go-starter' },
];

interface EmptyProjectsHeroProps {
  onCreateFromDashboard: () => void;
}

export function EmptyProjectsHero({ onCreateFromDashboard }: EmptyProjectsHeroProps) {
  return (
    <Card>
      <Card.Body className="p-8 text-center space-y-6 max-w-2xl mx-auto">
        <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 border border-primary/20 mx-auto">
          <Rocket className="size-6 text-primary" aria-hidden="true" />
        </div>

        <div className="space-y-1.5">
          <h2 className="font-display text-title-sm font-semibold text-foreground">
            Deploy your first project
          </h2>
          <p className="text-body-sm text-muted-foreground">
            LoopZeroCloud goes from idea to live URL in about 4 minutes. Pick a starter or use your own repo:
          </p>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          {TEMPLATES.map((tpl) => (
            <div
              key={tpl.name}
              className="rounded-md border border-border/40 p-4 text-left bg-muted/10 hover:bg-muted/20 transition-colors"
            >
              <p className="text-body-sm font-medium text-foreground">{tpl.name}</p>
              <p className="text-label text-muted-foreground mt-0.5">{tpl.description}</p>
              <div className="mt-3 flex items-center gap-1 bg-background rounded px-2 py-1 font-mono text-[11px]">
                <span className="text-muted-foreground">$</span>
                <span className="flex-1 truncate text-foreground">{tpl.cliCommand}</span>
                <CopyButton value={tpl.cliCommand} label="Copy" />
              </div>
            </div>
          ))}
        </div>

        <div className="space-y-3">
          <p className="text-label uppercase text-muted-foreground">Or from your own repo</p>
          <CodeBlock
            caption=""
            code="loopzero init"
            terminal
            copyable
          />
        </div>

        <div className="border-t border-border/40 pt-4">
          <Button variant="secondary" size="md" onClick={onCreateFromDashboard}>
            Create project from dashboard
          </Button>
        </div>
      </Card.Body>
    </Card>
  );
}

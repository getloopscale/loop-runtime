import { type ComponentType, type SVGProps } from 'react';
import { Card, CodeBlock, EmptyState, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';

/** Matches @loopzero/ui `IconComponent` type (re-declared to avoid coupling
 * to the library's non-exported alias). Compatible with all lucide-react icons. */
type IconComponent = ComponentType<SVGProps<SVGSVGElement> & { className?: string }>;

interface SubPagePlaceholderProps {
  /** Page title (also drives PageShell heading + document title hook). */
  title: string;
  /** Short description of what this section will do once implemented. */
  description: string;
  /** Icon shown in the EmptyState card. */
  icon: IconComponent;
  /** Optional CLI command(s) the user can run today to perform this action
   * from the terminal — kept truthful (no fake UI). */
  cliCommand?: string;
}

/**
 * SubPagePlaceholder — reusable shell for submenu routes that exist as
 * navigation targets (Vercel drill-down pattern) but whose full implementation
 * has not landed yet.
 *
 * Honest pattern: show the section title + a clear "in progress" empty state +
 * the CLI command that achieves the same outcome today. Avoids both
 * "page renders blank" (broken UX) and "page lies about feature being ready"
 * (worse UX). Aligns with CLAUDE.md Rule 3 (extreme honesty) + Rule 9
 * (CLI is the primary interface).
 */
export function SubPagePlaceholder({
  title,
  description,
  icon,
  cliCommand,
}: SubPagePlaceholderProps) {
  useRequireAuth();
  useSetPageTitle(title);

  return (
    <PageShell title={title} description={description}>
      <Card>
        <Card.Body className="py-12">
          <EmptyState
            icon={icon}
            title={`${title} is coming soon`}
            description="This screen is part of the navigation but the full UI is still being built. Use the CLI below to do this today."
          />
          {cliCommand && (
            <div className="mt-6 max-w-xl mx-auto">
              <CodeBlock code={cliCommand} terminal copyable />
            </div>
          )}
        </Card.Body>
      </Card>
    </PageShell>
  );
}

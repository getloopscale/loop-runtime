import type { ReactNode } from 'react';
import { useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import { Loader2 } from 'lucide-react';
import { useRequireAuth } from '@/hooks/use-auth';
import { isEmailVerified } from '@/lib/auth';
import { PageMetaProvider } from '@/hooks/use-page-meta';
import { useCommandPalette } from '@/hooks/use-command-palette';
import { useProjects } from '@/hooks/use-projects';
import { VerificationBanner } from '@/components/verification-banner';
import { CommandPalette } from '@/components/command/command-palette';
import { buildCommands, sortByRecentDeploy } from '@/components/command/commands';
import { OnboardingPanel } from '@/components/onboarding/onboarding-panel';
import { AppSidebar } from './app-sidebar';
import { TopBar } from './top-bar';

interface DashboardLayoutProps {
  children: ReactNode;
}

/**
 * DashboardLayout — shell for all protected routes. Wraps children with
 * <PageMetaProvider> so pages can call useSetPageTitle() to control TopBar.
 *
 * Sidebar width 220px + main content with TopBar h-14 + p-6.
 *
 * Auth pages (Login/Register/Verification/Recovery/DeviceSuccess) do NOT
 * use this layout — they render standalone via the public Routes block in
 * app.tsx (per D5 of plan v1.2).
 */
export function DashboardLayout({ children }: DashboardLayoutProps) {
  const session = useRequireAuth();
  const navigate = useNavigate();
  const { open: paletteOpen, setOpen: setPaletteOpen } = useCommandPalette();
  const { projects } = useProjects();

  // EC-12: project quick-jumps sorted by last_deploy DESC, tiebreaker created_at
  const commands = useMemo(
    () => buildCommands({ navigate, recentProjects: sortByRecentDeploy(projects) }),
    [navigate, projects],
  );

  if (session === null) {
    return (
      <div className="bg-background min-h-screen text-foreground flex items-center justify-center">
        <Loader2 className="h-6 w-6 text-primary animate-spin" aria-label="Loading" />
      </div>
    );
  }

  const verified = isEmailVerified(session);

  return (
    <PageMetaProvider>
      <a
        href="#main-content"
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:px-3 focus:py-2 focus:rounded-md focus:bg-primary focus:text-primary-foreground focus:text-body-sm focus:font-medium"
      >
        Skip to main content
      </a>
      <div className="bg-background min-h-screen text-foreground flex">
        <div className="border-r border-border/60 shrink-0">
          <AppSidebar session={session} />
        </div>
        <div className="flex-1 flex flex-col min-w-0">
          <TopBar onCommandPaletteOpen={() => setPaletteOpen(true)} />
          <main
            id="main-content"
            className="flex-1 px-6 py-6 mx-auto w-full max-w-screen-2xl"
          >
            {!verified && (
              <div className="mb-6">
                <VerificationBanner />
              </div>
            )}
            {children}
          </main>
        </div>
      </div>
      <CommandPalette
        open={paletteOpen}
        onOpenChange={setPaletteOpen}
        commands={commands}
      />
      {/* Sprint 3 T3.2 v2: Floating onboarding panel (Linear/Vercel 2026
          pattern). Bottom-right fixed, never competes with sidebar nav.
          Collapses to badge with progress ring. Replaces cramped sidebar
          widget (220px largura era ilegível). */}
      <OnboardingPanel />
    </PageMetaProvider>
  );
}

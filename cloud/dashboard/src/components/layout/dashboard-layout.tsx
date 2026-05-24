import type { ReactNode } from 'react';
import { Loader2 } from 'lucide-react';
import { useRequireAuth } from '@/hooks/use-auth';
import { isEmailVerified } from '@/lib/auth';
import { PageMetaProvider } from '@/hooks/use-page-meta';
import { VerificationBanner } from '@/components/verification-banner';
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
      <div className="bg-background min-h-screen text-foreground flex">
        <AppSidebar session={session} />
        <div className="flex-1 flex flex-col min-w-0">
          <TopBar />
          <main className="flex-1 p-6">
            {!verified && (
              <div className="mb-6">
                <VerificationBanner />
              </div>
            )}
            {children}
          </main>
        </div>
      </div>
    </PageMetaProvider>
  );
}

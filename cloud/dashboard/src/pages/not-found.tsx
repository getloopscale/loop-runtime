import { Link } from 'react-router-dom';
import { SearchX } from 'lucide-react';
import { Button, EmptyState, PageShell } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';

/**
 * NotFoundPage — catch-all `*` route page.
 *
 * Per plan v1.2 T0.3: explicit 404 instead of catch-all DashboardLayout
 * with no inner Route match (which previously rendered empty content area).
 *
 * Closes a corollary of SG-06: any unknown URL inside the dashboard shell
 * now lands here, not on a silently blank page.
 */
export function NotFoundPage() {
  useSetPageTitle('Page not found');

  return (
    <PageShell title="Page not found">
      <EmptyState
        icon={SearchX}
        title="That page doesn't exist"
        description="Check the URL or head back to your dashboard."
        action={
          <Button asChild>
            <Link to="/">Go home</Link>
          </Button>
        }
      />
    </PageShell>
  );
}

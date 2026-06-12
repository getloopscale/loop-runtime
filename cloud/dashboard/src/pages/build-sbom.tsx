import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { SBOMTab } from './deployment-tabs/sbom-tab';

/**
 * BuildSBOMPage — backward-compat wrapper.
 *
 * Plan v1.0 T1.4 originally introduced this page as a standalone route.
 * Plan J-17 T5.5 moved the content to SBOMTab inside the layout. This
 * wrapper exists so smoke tests (pages.smoke.test.ts) still pass and
 * any external bookmarks to /projects/:name/deployments/:id/sbom keep
 * working at the lazy-import level.
 *
 * The actual route in app.tsx delegates to DeploymentDetailLayout +
 * SBOMTab — this page itself is unrouted.
 */
export function BuildSBOMPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · ${id.slice(0, 7)} · SBOM`);

  return (
    <PageShell title={`SBOM · ${id.slice(0, 7)}`}>
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}/deployments/${id}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to deployment</span>
          </Link>
        </Button>
      </div>
      <SBOMTab />
    </PageShell>
  );
}

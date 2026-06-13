import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { AttestationTab } from './deployment-tabs/attestation-tab';

/**
 * BuildAttestationPage — backward-compat wrapper.
 *
 * Plan v1.0 T1.5 originally introduced this page as a standalone route.
 * Plan J-17 T5.6 moved the content to AttestationTab inside the layout.
 * Wrapper exists so pages.smoke.test.ts assertions continue to pass.
 */
export function BuildAttestationPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · ${id.slice(0, 7)} · Attestation`);

  return (
    <PageShell title={`Attestation · ${id.slice(0, 7)}`}>
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
      <AttestationTab />
    </PageShell>
  );
}

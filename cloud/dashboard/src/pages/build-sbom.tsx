import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Download, FileText } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  EmptyState,
  PageShell,
  Table,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { CardSkeleton } from '@/components/data/card-skeleton';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { engineFetch } from '@/lib/api';
import type { BuildSBOM } from '@/lib/api/types';

/**
 * BuildSBOMPage — Plan v1.0 T1.4.
 *
 * Fetches CycloneDX SBOM JSON from /api/v1/builds/{id}/sbom and renders
 * summary card + components table + download .json button.
 */
export function BuildSBOMPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · ${id.slice(0, 7)} · SBOM`);
  const [sbom, setSbom] = useState<BuildSBOM | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    engineFetch<BuildSBOM>(`/api/v1/builds/${encodeURIComponent(id)}/sbom`)
      .then((data) => {
        if (cancelled) return;
        setSbom(data);
        setLoading(false);
      })
      .catch(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const handleDownload = () => {
    if (!sbom) return;
    const blob = new Blob([JSON.stringify(sbom, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `sbom-${id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (loading) {
    return (
      <PageShell
        title={`SBOM · ${id.slice(0, 7)}`}
        loading
        loadingNode={
          <div className="flex flex-col gap-6">
            <CardSkeleton />
            <Card><TableSkeleton rows={8} columns={4} /></Card>
          </div>
        }
      />
    );
  }

  if (!sbom) {
    return (
      <PageShell title="SBOM not available">
        <EmptyState
          icon={FileText}
          title="No SBOM for this build"
          description="The Software Bill of Materials is generated as part of the post-build step. This build either failed before that step ran, or the build pre-dates the SBOM pipeline (rolled out 2026-04). Trigger a new build to get an SBOM."
          action={
            <Button asChild variant="secondary">
              <Link
                to={`/projects/${encodeURIComponent(name)}/deployments/${id}`}
                className="inline-flex items-center gap-1"
              >
                <ArrowLeft className="size-4" aria-hidden="true" />
                Back to deployment
              </Link>
            </Button>
          }
        />
      </PageShell>
    );
  }

  return (
    <PageShell title={`SBOM · ${id.slice(0, 7)}`} description={`${sbom.bomFormat} ${sbom.specVersion}`}>
      <div className="mb-4 flex items-center justify-between gap-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}/deployments/${id}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to deployment</span>
          </Link>
        </Button>
        <Button onClick={handleDownload} size="sm">
          <Download className="size-4" aria-hidden="true" />
          Download .json
        </Button>
      </div>

      <Card className="mb-4">
        <Card.Body className="grid grid-cols-1 gap-4 sm:grid-cols-3 text-body-sm">
          <div>
            <p className="text-label uppercase text-muted-foreground">Format</p>
            <p className="mt-1 font-medium text-foreground">{sbom.bomFormat}</p>
          </div>
          <div>
            <p className="text-label uppercase text-muted-foreground">Spec version</p>
            <p className="mt-1 font-medium text-foreground">{sbom.specVersion}</p>
          </div>
          <div>
            <p className="text-label uppercase text-muted-foreground">Components</p>
            <p className="mt-1 font-medium text-foreground tabular-nums">
              {sbom.components.length}
            </p>
          </div>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Component</Table.HeaderCell>
                <Table.HeaderCell>Version</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">Type</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">PURL</Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {sbom.components.map((c) => (
                <Table.Row key={c['bom-ref'] ?? `${c.name}@${c.version}`}>
                  <Table.Cell>
                    <span className="font-medium text-body-sm text-foreground">
                      {c.name}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="font-mono text-label text-muted-foreground">
                      {c.version}
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <Badge variant="outline" className="text-label uppercase">
                      {c.type}
                    </Badge>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <span className="font-mono text-label text-muted-foreground">
                      {c.purl ?? '—'}
                    </span>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>
    </PageShell>
  );
}

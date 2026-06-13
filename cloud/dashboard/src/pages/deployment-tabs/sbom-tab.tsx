import { useEffect, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Download, FileText, Loader2 } from 'lucide-react';
import { Badge, Button, Card, EmptyState } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import type { BuildSBOM } from '@/lib/api/types';

/**
 * SBOMTab — Plan J-17 T5.5.
 *
 * LIVE bug 2026-06-06: backend returns SBOM METADATA per app (format,
 * component count, generated_at, presigned MinIO download_url) — NOT
 * the inline CycloneDX JSON. The full SBOM lives at download_url for
 * users to grab via curl/sbom tools (grype, syft, dependency-track).
 *
 * UI shows: one card per app with format + component count + generated
 * time + a "Download" button that opens the presigned URL.
 */
export function SBOMTab() {
  const { id = '' } = useParams<{ name: string; id: string }>();
  const [sbom, setSbom] = useState<BuildSBOM | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

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
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Failed to load SBOM');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="size-6 animate-spin text-muted-foreground" aria-label="Loading SBOM" />
      </div>
    );
  }

  if (error || !sbom || !sbom.apps || sbom.apps.length === 0) {
    return (
      <EmptyState
        icon={FileText}
        title="No SBOM for this build"
        description={
          error
            ? `Could not load SBOM: ${error}`
            : 'SBOMs are generated for succeeded builds. This build may have failed before the SBOM step.'
        }
      />
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <Card.Body>
          <p className="text-body-sm text-muted-foreground">
            Software Bill of Materials for each app in this build. Download the
            CycloneDX JSON for use with grype, dependency-track, or other supply
            chain tools.
          </p>
        </Card.Body>
      </Card>

      {sbom.apps.map((app) => (
        <Card key={app.app_name}>
          <Card.Header>
            <div className="flex items-center justify-between gap-4 flex-wrap">
              <div>
                <Card.Title>{app.app_name}</Card.Title>
                <Card.Description>
                  {app.sbom_format ? (
                    <Badge variant="outline" className="text-label uppercase">
                      {app.sbom_format}
                    </Badge>
                  ) : (
                    'Format unknown'
                  )}
                </Card.Description>
              </div>
              {app.download_url && (
                <Button asChild size="sm">
                  <a href={app.download_url} target="_blank" rel="noopener noreferrer">
                    <Download className="size-4" aria-hidden="true" />
                    Download
                  </a>
                </Button>
              )}
            </div>
          </Card.Header>
          <Card.Body className="grid grid-cols-1 gap-4 sm:grid-cols-2 text-body-sm">
            <div>
              <p className="text-label uppercase text-muted-foreground">Components</p>
              <p className="mt-1 font-medium text-foreground tabular-nums">
                {app.component_count ?? '—'}
              </p>
            </div>
            <div>
              <p className="text-label uppercase text-muted-foreground">Generated</p>
              <p
                className="mt-1 font-medium text-foreground tabular-nums"
                title={app.generated_at}
              >
                {app.generated_at ? formatDate(app.generated_at, 'relative') : '—'}
              </p>
            </div>
          </Card.Body>
        </Card>
      ))}
    </div>
  );
}

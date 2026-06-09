import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Download, Loader2, ShieldCheck } from 'lucide-react';
import { Button, Card, CodeBlock, EmptyState, PageShell } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';
import type { BuildAttestation } from '@/lib/api/types';

/**
 * BuildAttestationPage — Plan v1.0 T1.5.
 *
 * Fetches cosign attestation envelope from /api/v1/builds/{id}/attestation.
 * Shows decoded payload (in-toto JSON) + signature summary + download.
 */
export function BuildAttestationPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · ${id.slice(0, 7)} · Attestation`);
  const [att, setAtt] = useState<BuildAttestation | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) {
      setLoading(false);
      return;
    }
    let cancelled = false;
    engineFetch<BuildAttestation>(
      `/api/v1/builds/${encodeURIComponent(id)}/attestation`,
    )
      .then((data) => {
        if (cancelled) return;
        setAtt(data);
        setLoading(false);
      })
      .catch(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const decodedPayload = (() => {
    if (!att) return '';
    try {
      const json = atob(att.payload);
      return JSON.stringify(JSON.parse(json), null, 2);
    } catch {
      return att.payload;
    }
  })();

  const handleDownload = () => {
    if (!att) return;
    const blob = new Blob([JSON.stringify(att, null, 2)], {
      type: 'application/json',
    });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `attestation-${id}.json`;
    a.click();
    URL.revokeObjectURL(url);
  };

  if (loading) {
    return (
      <PageShell title="Attestation">
        <div className="flex items-center justify-center py-12">
          <Loader2
            className="size-6 animate-spin text-muted-foreground"
            aria-label="Loading attestation"
          />
        </div>
      </PageShell>
    );
  }

  if (!att) {
    return (
      <PageShell title="Attestation not available">
        <EmptyState
          icon={ShieldCheck}
          title="No attestation for this build"
          description="Cosign signs succeeded builds. Failed or cancelled builds don't have an attestation."
        />
      </PageShell>
    );
  }

  return (
    <PageShell title={`Attestation · ${id.slice(0, 7)}`} description={att.payloadType}>
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

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[1fr_2fr]">
        <Card>
          <Card.Header>
            <Card.Title>Signatures</Card.Title>
          </Card.Header>
          <Card.Body>
            <ul className="space-y-3">
              {att.signatures.map((s) => (
                <li key={s.keyid} className="text-body-sm">
                  <p className="text-label uppercase text-muted-foreground">
                    Key id
                  </p>
                  <p className="mt-1 font-mono text-foreground break-all">{s.keyid}</p>
                  <p className="mt-3 text-label uppercase text-muted-foreground">
                    Signature
                  </p>
                  <p className="mt-1 font-mono text-label text-muted-foreground break-all">
                    {s.sig.slice(0, 32)}…
                  </p>
                </li>
              ))}
            </ul>
          </Card.Body>
        </Card>

        <Card>
          <Card.Header>
            <Card.Title>Payload (in-toto)</Card.Title>
            <Card.Description>Decoded predicate JSON.</Card.Description>
          </Card.Header>
          <Card.Body>
            <CodeBlock code={decodedPayload} copyable />
          </Card.Body>
        </Card>
      </div>
    </PageShell>
  );
}

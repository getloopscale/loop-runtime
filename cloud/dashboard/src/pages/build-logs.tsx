import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, Card, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useLogStream } from '@/hooks/use-log-stream';
import { LogViewer } from '@/components/logs/log-viewer';

/**
 * BuildLogsPage — Plan v1.0 T1.3.
 *
 * Streams build logs via SSE. Path = /engine/api/v1/builds/{id}/logs/stream,
 * which mocks/sse.ts shim handles in dev. The LogViewer composite renders
 * + provides filter/search/pause/clear toolbar.
 */
export function BuildLogsPage() {
  useRequireAuth();
  const { name = '', id = '' } = useParams<{ name: string; id: string }>();
  useSetPageTitle(`${name} · ${id.slice(0, 7)} · Logs`);
  const [paused, setPaused] = useState(false);

  const streamPath = id
    ? `/engine/api/v1/builds/${encodeURIComponent(id)}/logs/stream`
    : null;
  const { lines, connected, error, clear } = useLogStream(streamPath, { paused });

  return (
    <PageShell title={`Build logs · ${id.slice(0, 7)}`}>
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

      <Card className="overflow-hidden">
        <div className="h-[640px]">
          <LogViewer
            lines={lines}
            connected={connected}
            error={error}
            paused={paused}
            onTogglePause={() => setPaused((p) => !p)}
            onClear={clear}
          />
        </div>
      </Card>
    </PageShell>
  );
}

import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, Card, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useLogStream } from '@/hooks/use-log-stream';
import { LogViewer } from '@/components/logs/log-viewer';

/**
 * RuntimeLogsPage — Plan v1.0 T2.1.
 *
 * Streams runtime logs from /api/v1/projects/{name}/logs/stream via SSE.
 * Reuses LogViewer composite + useLogStream hook from Phase 1 — only the
 * path differs from build logs. Mock emitter (handlers.ts) keeps emitting
 * lines indefinitely (vs build logs which finish after 10 lines).
 */
export function RuntimeLogsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Runtime logs`);
  const [paused, setPaused] = useState(false);

  const streamPath = name
    ? `/engine/api/v1/projects/${encodeURIComponent(name)}/logs/stream`
    : null;
  const { lines, connected, error, clear } = useLogStream(streamPath, { paused });

  return (
    <PageShell
      title="Runtime logs"
      description={`Live log stream for ${name}.`}
    >
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
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

import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft } from 'lucide-react';
import { Button, Card, PageShell } from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useLogStream } from '@/hooks/use-log-stream';
import { LogViewer } from '@/components/logs/log-viewer';
import { TimeRangePicker } from '@/components/logs/time-range-picker';

/**
 * RuntimeLogsPage — Plan v1.0 (T2.1) + J-19 T1.1 (TimeRangePicker wire).
 *
 * Streams runtime logs from /api/v1/projects/{name}/logs/stream?since={range}
 * via SSE. Reuses LogViewer composite + useLogStream hook. The `?since=` query
 * param consumes LoopZero's Beta Logs Gate Criterion 7 backend contract (canonical
 * across CLI + dashboard); changing the time range disconnects + reconnects
 * the stream (useEffect[path] in use-log-stream.ts clears buffer per design).
 */
export function RuntimeLogsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Runtime logs`);
  const [paused, setPaused] = useState(false);
  const [selectedRange, setSelectedRange] = useState('1h');

  // 2026-06-13 LIVE J-19 walkthrough fix — backend StreamRuntimeLogs requires
  // `env` query parameter (returns 400 MISSING_FIELD otherwise). Default to
  // `production` to match Beta Logs Gate canonical env. Future: derive from
  // current environment context (when env-switcher ships v0.2).
  const streamPath = name
    ? `/engine/api/v1/projects/${encodeURIComponent(name)}/logs/stream?env=production&since=${selectedRange}`
    : null;
  const { lines, connected, error, droppedCount, clear } = useLogStream(streamPath, { paused });

  return (
    <PageShell
      title="Runtime logs"
      description={`Live log stream for ${name}.`}
    >
      <div className="mb-4 flex items-center justify-between gap-2">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
        <TimeRangePicker value={selectedRange} onChange={setSelectedRange} />
      </div>

      <Card className="overflow-hidden">
        <div className="h-[640px]">
          <LogViewer
            lines={lines}
            connected={connected}
            error={error}
            paused={paused}
            droppedCount={droppedCount}
            onTogglePause={() => setPaused((p) => !p)}
            onClear={clear}
          />
        </div>
      </Card>
    </PageShell>
  );
}

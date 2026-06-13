import { useEffect, useMemo, useState } from 'react';
import { useParams } from 'react-router-dom';
import { Card } from '@loopzero/ui';
import { useLogStream } from '@/hooks/use-log-stream';
import { useDeployment } from '@/hooks/use-deployment';
import { LogViewer } from '@/components/logs/log-viewer';
import { engineFetch } from '@/lib/api';
import { normalizeBuildStatus } from '@/lib/api/types';
import type { LogLine, LogLevel } from '@/lib/api/types';

/**
 * LogsTab — Plan J-17 T5.4 + LIVE bug fix 2026-06-06.
 *
 * Two-layer log loading (mirrors Vercel/Railway design):
 *
 *  1. **Historical via REST** — on mount, GET /logs returns every log
 *     line Loki has retained for this build (7 day retention via S3
 *     chunks in MinIO). Works for SUCCEEDED, FAILED, CANCELLED, and
 *     RUNNING builds alike. The user always sees what was already
 *     captured, even after the workflow Pod has been GC'd.
 *
 *  2. **Live SSE only when in-flight** — if the build status is
 *     non-terminal (QUEUED / RUNNING / CANCELLING / RELEASING) we ALSO
 *     open the SSE stream so new lines appended after the historical
 *     snapshot tick in real time. Terminal builds skip the SSE entirely
 *     (no more lines will arrive; opening would just hang) avoiding the
 *     "stuck on empty viewer" UX that pre-fix LogsTab suffered from.
 *
 * Backend LogEntry shape ({step, stream, content, time}) is converted to
 * the frontend's canonical LogLine shape ({ts, level, msg, source}) so
 * the LogViewer renders consistently regardless of source.
 */

interface BackendLogEntry {
  step?: string;
  stream?: string;
  content?: string;
  // LIVE bug 2026-06-06: backend emits `timestamp` (verified via curl);
  // `time` accepted as fallback for forward-compat.
  timestamp?: string;
  time?: string;
}

interface BackendLogsResponse {
  build_id: string;
  status: string;
  entries: BackendLogEntry[];
  total_lines: number;
}

/**
 * Convert backend LogEntry → frontend LogLine.
 *
 * Plan J-17 Layer 4 (2026-06-06): pipeline lines that go through the
 * loopzero marker wrapper come prefixed with
 *   `::loopzero:: phase=X step=Y level=Z msg="..."`
 * Strip the marker and surface only the human message + structured
 * fields. The phase is used as source so the LogViewer can color-code
 * by phase group (clone / install / build / sign / deliver).
 *
 * Lines without the marker still render via the legacy fallback —
 * useful for debug mode (raw container output).
 */
const LOOPZERO_MARKER_RE =
  /^::loopzero:: phase=(?<phase>\w+) step=(?<step>[\w-]+) level=(?<level>\w+) msg=(?<msg>.+?)\s*$/;

function toLogLine(e: BackendLogEntry): LogLine {
  const content = e.content ?? '';
  const marker = LOOPZERO_MARKER_RE.exec(content);
  if (marker?.groups) {
    const msg = unquote(marker.groups.msg);
    return {
      ts: e.timestamp ?? e.time ?? new Date().toISOString(),
      level: coerceLevel(marker.groups.level),
      msg,
      source: humanizePhase(marker.groups.phase),
    };
  }
  return {
    ts: e.timestamp ?? e.time ?? new Date().toISOString(),
    level: extractLevel(content),
    msg: content,
    source: e.step || e.stream,
  };
}

function unquote(s: string): string {
  const t = s.trim();
  if (t.startsWith('"') && t.endsWith('"') && t.length >= 2) {
    // Strip outer quotes AND undo the wrapper's sed escape (Layer 5):
    // build pipeline replays failure tail lines with `"` → `\"` and
    // `\` → `\\` so the marker regex survives. Reverse those here so
    // the user sees the real npm/cosign/buildkit error verbatim.
    return t.slice(1, -1).replace(/\\"/g, '"').replace(/\\\\/g, '\\');
  }
  return t;
}

function coerceLevel(raw: string): LogLevel {
  const l = raw.toLowerCase();
  if (l === 'warning') return 'warn';
  if (l === 'debug' || l === 'info' || l === 'warn' || l === 'error' || l === 'fatal') {
    return l;
  }
  return 'info';
}

// humanizePhase converts the canonical phase group ("clone" / "install"
// / "build" / "sign" / "deliver" / "other") into a capitalized display
// suitable for the LogViewer source column.
function humanizePhase(phase: string): string {
  if (!phase) return '';
  return phase.charAt(0).toUpperCase() + phase.slice(1);
}

function extractLevel(content: string): LogLevel {
  const m = content.match(/level=(debug|info|warn|warning|error|fatal)/i);
  if (!m) return 'info';
  const lvl = m[1].toLowerCase();
  if (lvl === 'warning') return 'warn';
  return lvl as LogLevel;
}

const TERMINAL_STATUSES = new Set(['succeeded', 'failed', 'cancelled', 'cancelling']);

export function LogsTab() {
  const { id = '' } = useParams<{ name: string; id: string }>();
  const [paused, setPaused] = useState(false);
  const [historical, setHistorical] = useState<LogLine[]>([]);
  const [historicalLoading, setHistoricalLoading] = useState(true);
  const [historicalError, setHistoricalError] = useState<string | null>(null);
  // J-17 pattern #5 canonical (lessons-learned 2026-06-06):
  // REST + SSE coexistem; Refresh manual button obrigatório.
  // `refreshing` é distinto de `historicalLoading` — o primeiro é
  // user-driven (botão Refresh), o segundo é initial-load lifecycle.
  const [refreshing, setRefreshing] = useState(false);

  // Get build status to decide whether to open SSE.
  const { build } = useDeployment(id);
  const status = build ? normalizeBuildStatus(build.status) : 'queued';
  const isTerminal = TERMINAL_STATUSES.has(status);

  // Historical fetch on mount + on id change + on terminal transition.
  //
  // LIVE bug 2026-06-06: when sponsor opens Logs tab during RUNNING, Loki
  // has only the lines flushed up to that instant. After the build finishes
  // and the workflow Pod is reaped, the remaining chunks land in Loki —
  // but the LogsTab kept the partial snapshot forever because deps only
  // included [id]. Adding `isTerminal` re-fetches once at the transition,
  // pulling the complete log set in. Idempotent (same data on re-fetch
  // of an already terminal build).
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    setHistoricalLoading(true);
    setHistoricalError(null);
    engineFetch<BackendLogsResponse>(`/api/v1/builds/${encodeURIComponent(id)}/logs`)
      .then((data) => {
        if (cancelled) return;
        if (!data) {
          setHistorical([]);
        } else {
          setHistorical((data.entries ?? []).map(toLogLine));
        }
        setHistoricalLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setHistoricalError(
          err instanceof Error ? err.message : 'Failed to load historical logs',
        );
        setHistoricalLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [id, isTerminal]);

  // SSE live only for non-terminal builds. When terminal, pass null
  // path so useLogStream is a no-op.
  const streamPath =
    id && !isTerminal
      ? `/engine/api/v1/builds/${encodeURIComponent(id)}/logs/stream`
      : null;
  const stream = useLogStream(streamPath, { paused });

  // Combine historical (always) + live (only if streaming). Historical
  // first so the viewer scrolls chronologically. Live lines append on
  // top of historical.
  const lines = useMemo<LogLine[]>(() => {
    return [...historical, ...stream.lines];
  }, [historical, stream.lines]);

  // Connected state: when terminal, "connected" effectively means
  // historical loaded successfully. When live, defer to the SSE state.
  const connected = isTerminal ? !historicalLoading && !historicalError : stream.connected;
  const error = Boolean(historicalError) || stream.error;

  const handleClear = () => {
    setHistorical([]);
    stream.clear();
  };

  // Manual re-fetch of historical chunks via REST. Distinct from
  // useEffect initial-load: user-driven, no cancellation flag needed
  // (the user invokes it intentionally), guarded against click-storms
  // via `refreshing` state. Preserves the live SSE stream — only
  // re-snapshots the historical buffer (useful after backfill OR
  // after the workflow Pod was reaped and remaining Loki chunks
  // arrive late).
  const handleRefresh = async () => {
    if (!id || refreshing) return;
    setRefreshing(true);
    setHistoricalError(null);
    try {
      const data = await engineFetch<BackendLogsResponse>(
        `/api/v1/builds/${encodeURIComponent(id)}/logs`,
      );
      setHistorical(data ? (data.entries ?? []).map(toLogLine) : []);
    } catch (err: unknown) {
      setHistoricalError(
        err instanceof Error ? err.message : 'Failed to refresh logs',
      );
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <Card className="overflow-hidden">
      <div className="h-[640px]">
        <LogViewer
          lines={lines}
          connected={connected}
          error={error}
          paused={paused}
          onTogglePause={() => setPaused((p) => !p)}
          onClear={handleClear}
          onRefresh={handleRefresh}
          refreshing={refreshing}
        />
      </div>
    </Card>
  );
}

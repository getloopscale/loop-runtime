import { useEffect, useRef, useState } from 'react';
import { Copy } from 'lucide-react';
import { Badge, Button } from '@loopzerokit/ui';
import type { LogLine, LogLevel } from '@/lib/api/types';
import { parseAnsi } from '@/lib/ansi';
import { copyToClipboard } from '@/lib/clipboard';
import { formatLocalTime } from '@/lib/format-ts';
import { formatLineForExport } from '@/lib/log-download';
import { DroppedLinesIndicator } from './dropped-lines-indicator';
import { LogViewerToolbar } from './log-viewer-toolbar';

interface LogViewerProps {
  lines: LogLine[];
  connected: boolean;
  error: boolean;
  paused: boolean;
  /** J-19 T1.3 (R3) — number of older lines silently dropped from the
   *  FIFO buffer (cap 5000). When > 0, the viewer surfaces an explicit
   *  indicator at the top of the buffer. Optional for backward compat
   *  with build-logs.tsx (existing caller without droppedCount). */
  droppedCount?: number;
  onTogglePause: () => void;
  onClear: () => void;
}

const LEVEL_COLOR: Record<LogLevel, string> = {
  debug: 'text-muted-foreground',
  info: 'text-foreground',
  warn: 'text-warning',
  error: 'text-destructive',
  fatal: 'text-destructive',
};

const LEVEL_BADGE: Record<
  LogLevel,
  'default' | 'destructive' | 'success' | 'warning' | 'primary' | 'accent' | 'outline'
> = {
  debug: 'outline',
  info: 'default',
  warn: 'warning',
  error: 'destructive',
  fatal: 'destructive',
};

/**
 * LogViewer — Plan v1.0 T1.3 composite.
 *
 * Renders a streaming log buffer. Filter by severity, search by substring,
 * auto-scroll to bottom (disable on user scroll-up — re-enable when user
 * scrolls back to bottom). Pause/resume + clear controls in toolbar.
 *
 * Designed to render thousands of lines; uses CSS overflow-y-auto rather
 * than virtual scrolling (acceptable up to ~5000 lines; for larger streams
 * we'd need react-virtual — deferred).
 */
export function LogViewer({
  lines,
  connected,
  error,
  paused,
  droppedCount = 0,
  onTogglePause,
  onClear,
}: LogViewerProps) {
  const [search, setSearch] = useState('');
  const [levelFilter, setLevelFilter] = useState<'all' | LogLevel>('all');
  // J-19 T1.4 (R4) — empty-state 3s delay gate. The empty-state message
  // ("No log lines yet…") should only appear AFTER 3s of empty-connected
  // state — typical SSE handshake completes in 100-500ms, so showing the
  // message immediately makes it flash briefly during normal startup.
  // EC-7 covered: timer resets on reconnect (connected:false → true).
  const [showEmptyState, setShowEmptyState] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(true);

  // Auto-scroll on new lines — but only when user hasn't scrolled up
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !autoScrollRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [lines]);

  // 3s delay before showing "No log lines yet" — only when SSE connected
  // AND buffer empty. Any first line arrival OR disconnect cancels.
  useEffect(() => {
    if (connected && lines.length === 0) {
      const t = setTimeout(() => setShowEmptyState(true), 3000);
      return () => clearTimeout(t);
    }
    setShowEmptyState(false);
  }, [connected, lines.length]);

  // J-19 T2.1 (R5) — per-line permalink (#L<index>). On mount, parse the
  // hash; if `#L<n>` matches a valid line index, scroll to it. Q4
  // resolved (plan v1.1): index-based for v0.1; correlation_id-based
  // deferred to v0.2 (R12). If FIFO drops happen between click and
  // reload, scroll jumps to the wrong line — droppedCount indicator
  // (R3) makes this discoverable; user re-clicks.
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const m = window.location.hash.match(/^#L(\d+)$/);
    if (!m) return;
    const idx = Number.parseInt(m[1], 10);
    if (!Number.isFinite(idx) || idx < 0 || idx >= lines.length) return;
    const el = scrollRef.current?.querySelector<HTMLElement>(`[data-line-index="${idx}"]`);
    el?.scrollIntoView({ block: 'center' });
    // Disable auto-scroll so the targeted line stays visible
    autoScrollRef.current = false;
    // Run once after mount; intentional empty deps + lines.length probe
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleScroll = () => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50;
    autoScrollRef.current = atBottom;
  };

  const filtered = lines.filter((l) => {
    if (levelFilter !== 'all' && l.level !== levelFilter) return false;
    if (search && !l.msg.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  });

  return (
    <div className="flex h-full flex-col">
      <LogViewerToolbar
        lines={lines}
        connected={connected}
        error={error}
        paused={paused}
        search={search}
        levelFilter={levelFilter}
        onSearchChange={setSearch}
        onLevelFilterChange={setLevelFilter}
        onTogglePause={onTogglePause}
        onClear={onClear}
      />

      {/* J-19 T1.3 (R3) — extracted to DroppedLinesIndicator at T2.x batch */}
      <DroppedLinesIndicator count={droppedCount} />

      {/* Log lines */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto bg-surface-1 font-mono text-label"
      >
        {filtered.length === 0 ? (
          // T0.2 D4 — empty state com what/why/action context
          // J-19 T1.4 (R4) — "No log lines yet" branch gated by showEmptyState
          // (3s delay). Filter-mismatch branch is immediate — user just
          // applied a filter intentionally; no delay needed.
          <div className="px-4 py-12 text-center text-muted-foreground">
            {lines.length === 0
              ? showEmptyState
                ? 'No log lines yet. Logs stream here as your app runs — trigger a request or wait for the next event.'
                : null
              : 'No lines match your filter. Clear search or change severity to see more lines.'}
          </div>
        ) : (
          filtered.map((line, i) => (
            <div
              key={`${line.ts}-${i}`}
              data-line-index={i}
              className="group flex items-start gap-3 border-b border-border/30 px-4 py-1.5 hover:bg-surface-2/40"
              onClick={() => {
                if (typeof window !== 'undefined') {
                  window.location.hash = `#L${i}`;
                }
              }}
            >
              {/* J-19 T2.5 (R9) — browser-local time + UTC tooltip */}
              <time
                className="shrink-0 tabular-nums text-muted-foreground"
                dateTime={formatLocalTime(line.ts).iso}
                title={formatLocalTime(line.ts).iso}
              >
                {formatLocalTime(line.ts).local}
              </time>
              <Badge
                variant={LEVEL_BADGE[line.level]}
                className="shrink-0 text-label uppercase"
              >
                {line.level}
              </Badge>
              {line.source && (
                <Badge variant="outline" className="shrink-0 text-label">
                  {line.source}
                </Badge>
              )}
              <span className={`flex-1 break-all ${LEVEL_COLOR[line.level]}`}>
                {parseAnsi(line.msg)}
              </span>
              {/* J-19 T2.2 (R6) — copy-to-clipboard on hover */}
              <Button
                variant="ghost"
                size="sm"
                aria-label={`Copy line ${i + 1}`}
                className="shrink-0 opacity-0 group-hover:opacity-100"
                onClick={(e) => {
                  e.stopPropagation(); // don't trigger row's permalink handler
                  void copyToClipboard(formatLineForExport(line));
                }}
              >
                <Copy className="size-3" aria-hidden="true" />
              </Button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

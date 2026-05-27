import { useEffect, useRef, useState } from 'react';
import { Pause, Play, Trash2 } from 'lucide-react';
import { Badge, Button, Input } from '@loopzero/ui';
import type { LogLine, LogLevel } from '@/lib/api/types';

interface LogViewerProps {
  lines: LogLine[];
  connected: boolean;
  error: boolean;
  paused: boolean;
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

const LEVELS: { value: 'all' | LogLevel; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
];

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
  onTogglePause,
  onClear,
}: LogViewerProps) {
  const [search, setSearch] = useState('');
  const [levelFilter, setLevelFilter] = useState<'all' | LogLevel>('all');
  const scrollRef = useRef<HTMLDivElement>(null);
  const autoScrollRef = useRef(true);

  // Auto-scroll on new lines — but only when user hasn't scrolled up
  useEffect(() => {
    const el = scrollRef.current;
    if (!el || !autoScrollRef.current) return;
    el.scrollTop = el.scrollHeight;
  }, [lines]);

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
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-border/60 px-4 py-2">
        <Input
          type="search"
          placeholder="Search logs..."
          aria-label="Search log lines"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-8 w-48"
        />
        <div className="flex items-center gap-1" role="group" aria-label="Filter by level">
          {LEVELS.map((l) => (
            <Button
              key={l.value}
              size="sm"
              variant={levelFilter === l.value ? 'primary' : 'ghost'}
              onClick={() => setLevelFilter(l.value)}
            >
              {l.label}
            </Button>
          ))}
        </div>
        <div className="ml-auto flex items-center gap-2">
          <Badge
            variant={connected ? 'success' : error ? 'destructive' : 'outline'}
            className="text-label uppercase"
          >
            {error ? 'Disconnected' : connected ? 'Live' : 'Connecting…'}
          </Badge>
          <Button
            variant="ghost"
            size="sm"
            onClick={onTogglePause}
            aria-label={paused ? 'Resume log stream' : 'Pause log stream'}
          >
            {paused ? (
              <Play className="size-4" aria-hidden="true" />
            ) : (
              <Pause className="size-4" aria-hidden="true" />
            )}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClear}
            aria-label="Clear log buffer"
          >
            <Trash2 className="size-4" aria-hidden="true" />
          </Button>
        </div>
      </div>

      {/* Log lines */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto bg-surface-1 font-mono text-label"
      >
        {filtered.length === 0 ? (
          <div className="px-4 py-12 text-center text-muted-foreground">
            {lines.length === 0
              ? 'Waiting for log lines…'
              : 'No lines match your filter.'}
          </div>
        ) : (
          filtered.map((line, i) => (
            <div
              key={`${line.ts}-${i}`}
              className="flex items-start gap-3 border-b border-border/30 px-4 py-1.5"
            >
              <span className="shrink-0 tabular-nums text-muted-foreground">
                {line.ts.slice(11, 19)}
              </span>
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
                {line.msg}
              </span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

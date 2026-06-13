import { Download, Pause, Play, Trash2 } from 'lucide-react';
import { Badge, Button, Input } from '@loopzero/ui';
import type { LogLine, LogLevel } from '@/lib/api/types';
import { triggerLogDownload } from '@/lib/log-download';

const LEVELS: { value: 'all' | LogLevel; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'info', label: 'Info' },
  { value: 'warn', label: 'Warn' },
  { value: 'error', label: 'Error' },
];

interface LogViewerToolbarProps {
  lines: LogLine[];
  connected: boolean;
  error: boolean;
  paused: boolean;
  search: string;
  levelFilter: 'all' | LogLevel;
  onSearchChange: (next: string) => void;
  onLevelFilterChange: (next: 'all' | LogLevel) => void;
  onTogglePause: () => void;
  onClear: () => void;
}

/**
 * LogViewerToolbar — J-19 T2.x extraction.
 *
 * Extracted from LogViewer at T2.3 (Download button addition) to keep
 * log-viewer.tsx under 250 LOC budget (Global DoD + EC-9 halt-loop
 * checkpoint). Hosts: search input, severity filter buttons, connection
 * badge, pause/resume, download (T2.3), clear.
 */
export function LogViewerToolbar({
  lines,
  connected,
  error,
  paused,
  search,
  levelFilter,
  onSearchChange,
  onLevelFilterChange,
  onTogglePause,
  onClear,
}: LogViewerToolbarProps) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-border/60 px-4 py-2">
      <Input
        type="search"
        placeholder="Search logs..."
        aria-label="Search log lines"
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        className="h-8 w-48"
      />
      <div className="flex items-center gap-1" role="group" aria-label="Filter by level">
        {LEVELS.map((l) => (
          <Button
            key={l.value}
            size="sm"
            variant={levelFilter === l.value ? 'primary' : 'ghost'}
            onClick={() => onLevelFilterChange(l.value)}
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
          onClick={() => triggerLogDownload(lines)}
          aria-label="Download log buffer as .log file"
          disabled={lines.length === 0}
        >
          <Download className="size-4" aria-hidden="true" />
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
  );
}

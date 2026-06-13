import { useEffect, useState } from 'react';

/**
 * BuildStepTimer — Plan J-17 T3.3.
 *
 * Displays the duration between startedAt and endedAt as a formatted
 * string ("47s", "1m 23s", "2h 15m"). When endedAt is not provided AND
 * not paused, ticks every second so the user sees live progress.
 */
export interface BuildStepTimerProps {
  startedAt: string;
  endedAt?: string;
  /** Pause the live tick (e.g., parent step is no longer running). */
  paused?: boolean;
  className?: string;
}

export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  if (m < 60) return s === 0 ? `${m}m` : `${m}m ${s}s`;
  const h = Math.floor(m / 60);
  const rem = m % 60;
  return rem === 0 ? `${h}h` : `${h}h ${rem}m`;
}

export function BuildStepTimer({
  startedAt,
  endedAt,
  paused = false,
  className,
}: BuildStepTimerProps) {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (endedAt || paused) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [endedAt, paused]);

  const start = new Date(startedAt).getTime();
  const end = endedAt ? new Date(endedAt).getTime() : now;
  const seconds = (end - start) / 1000;

  return (
    <span className={className} aria-label={`Step duration ${formatDuration(seconds)}`}>
      {formatDuration(seconds)}
    </span>
  );
}

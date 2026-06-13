import { Check, CircleDashed, Loader2, X } from 'lucide-react';
import type { BuildStep } from '@/lib/api/sse-events';
import { BuildStepTimer } from './build-step-timer';

/**
 * BuildStepCard — Plan J-17 T3.2.
 *
 * One step in the deploy timeline. Renders a small status icon, the
 * step display name (truncated for very long names — EC-14), and the
 * BuildStepTimer for live duration. Failure state surfaces the error
 * class via aria-label so screen readers describe the error.
 */
export interface BuildStepCardProps {
  step: BuildStep;
  /** When true, this is the head of the timeline — adds pulse animation. */
  active?: boolean;
  className?: string;
}

function displayLabel(stepName: string): string {
  // Strip noisy suffixes that the backend may not have already stripped
  // and replace dashes with spaces for prose-style rendering.
  const stripped = stepName.replace(/[-_]/g, ' ');
  return stripped.charAt(0).toUpperCase() + stripped.slice(1);
}

export function BuildStepCard({ step, active = false, className }: BuildStepCardProps) {
  const label = displayLabel(step.step_name);
  const isRunning = step.status === 'running';
  const isFailed = step.status === 'failed';
  const isSucceeded = step.status === 'succeeded';

  const Icon = isFailed ? X : isSucceeded ? Check : isRunning ? Loader2 : CircleDashed;
  const tone = isFailed
    ? 'text-destructive'
    : isSucceeded
      ? 'text-success'
      : isRunning
        ? 'text-primary'
        : 'text-muted-foreground';

  return (
    <div
      className={[
        'flex items-center gap-3 rounded-md border border-border/60 px-3 py-2',
        active && isRunning ? 'animate-pulse' : '',
        className ?? '',
      ]
        .filter(Boolean)
        .join(' ')}
      aria-label={isFailed ? `${label} failed: ${step.error_class ?? 'unknown'}` : label}
    >
      <Icon
        className={`size-4 shrink-0 ${tone} ${isRunning ? 'animate-spin' : ''}`}
        aria-hidden="true"
      />
      <div className="min-w-0 flex-1">
        <p
          className="truncate text-body-sm font-medium"
          title={step.step_name}
        >
          {label}
        </p>
        {(step.started_at || step.duration_seconds !== undefined) && (
          <p className="text-label text-muted-foreground">
            <BuildStepTimer
              startedAt={step.started_at}
              endedAt={step.ended_at}
              paused={!isRunning}
            />
          </p>
        )}
      </div>
    </div>
  );
}

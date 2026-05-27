import { Check, CircleDashed, Loader2, X } from 'lucide-react';
import type { BuildStatus } from '@/lib/api/types';

/**
 * BuildTimeline — Plan v1.0 T1.2 component.
 *
 * Renders the canonical build phases as an ordered vertical list. The
 * `current` status drives which phase is active + how earlier phases render
 * (succeeded). Designed for the DeploymentDetailPage right column.
 *
 * Phase order matches the LoopZero build pipeline (queued → running → released).
 * If backend ever splits "running" into sub-phases (clone, install, build,
 * push, sign), this composite stays unchanged — it visualizes the high-level
 * lifecycle, not internal sub-steps.
 */
export interface BuildTimelinePhase {
  id: string;
  label: string;
  description?: string;
}

const PHASES: BuildTimelinePhase[] = [
  { id: 'queued', label: 'Queued', description: 'Build accepted, waiting for runner.' },
  { id: 'running', label: 'Building', description: 'Cloning, installing, compiling.' },
  { id: 'pushing', label: 'Pushing image', description: 'Uploading container to registry.' },
  { id: 'signing', label: 'Signing', description: 'Cosign attestation + SBOM upload.' },
  { id: 'releasing', label: 'Releasing', description: 'Updating manifests via GitOps.' },
  { id: 'succeeded', label: 'Live', description: 'Image deployed to environment.' },
];

type PhaseState = 'pending' | 'active' | 'done' | 'failed' | 'cancelled';

function phaseState(
  phaseIndex: number,
  status: BuildStatus,
): PhaseState {
  // Map terminal statuses to a single visual sequence
  if (status === 'failed') {
    if (phaseIndex < 2) return 'done'; // queued + running may have completed
    if (phaseIndex === 2) return 'failed';
    return 'pending';
  }
  if (status === 'cancelled') {
    return phaseIndex === 0 ? 'cancelled' : 'pending';
  }
  if (status === 'queued') {
    return phaseIndex === 0 ? 'active' : 'pending';
  }
  if (status === 'running') {
    if (phaseIndex < 1) return 'done';
    if (phaseIndex === 1) return 'active';
    return 'pending';
  }
  if (status === 'cancelling') {
    if (phaseIndex < 1) return 'done';
    return 'pending';
  }
  // succeeded: all phases done
  return 'done';
}

interface BuildTimelineProps {
  status: BuildStatus;
}

export function BuildTimeline({ status }: BuildTimelineProps) {
  return (
    <ol className="space-y-3" aria-label="Build pipeline phases">
      {PHASES.map((phase, i) => {
        const state = phaseState(i, status);
        return (
          <li key={phase.id} className="flex items-start gap-3">
            <span
              className="mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full border border-border"
              aria-hidden="true"
            >
              {state === 'done' && (
                <Check className="size-3 text-success" />
              )}
              {state === 'active' && (
                <Loader2 className="size-3 animate-spin text-primary" />
              )}
              {state === 'failed' && (
                <X className="size-3 text-destructive" />
              )}
              {(state === 'pending' || state === 'cancelled') && (
                <CircleDashed className="size-3 text-muted-foreground" />
              )}
            </span>
            <div className="min-w-0 flex-1">
              <p
                className={
                  state === 'done'
                    ? 'text-body-sm font-medium text-foreground'
                    : state === 'active'
                      ? 'text-body-sm font-semibold text-primary'
                      : state === 'failed'
                        ? 'text-body-sm font-semibold text-destructive'
                        : 'text-body-sm font-medium text-muted-foreground'
                }
              >
                {phase.label}
              </p>
              {phase.description && (
                <p className="text-label text-muted-foreground">
                  {phase.description}
                </p>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

export const BUILD_TIMELINE_PHASES = PHASES;

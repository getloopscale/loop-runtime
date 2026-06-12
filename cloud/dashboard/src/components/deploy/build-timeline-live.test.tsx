import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import { BuildTimelineLive } from './build-timeline-live';
import type { BuildStep } from '@/lib/api/sse-events';
import type { UseDeploymentStreamResult } from '@/hooks/use-deployment-stream';

vi.mock('@/hooks/use-deployment-stream', () => ({
  useDeploymentStream: vi.fn(),
}));

import { useDeploymentStream } from '@/hooks/use-deployment-stream';

const mockedStream = vi.mocked(useDeploymentStream);

function streamResult(overrides: Partial<UseDeploymentStreamResult> = {}): UseDeploymentStreamResult {
  return {
    steps: [],
    currentStep: null,
    totalDurationSeconds: null,
    status: 'queued',
    error: null,
    connected: true,
    lastEventID: null,
    ...overrides,
  };
}

function renderTimeline(buildId = 'b-1', projectName = 'teste') {
  return render(
    <MemoryRouter>
      <BuildTimelineLive buildId={buildId} projectName={projectName} />
    </MemoryRouter>,
  );
}

describe('BuildTimelineLive', () => {
  beforeEach(() => {
    mockedStream.mockReset();
  });

  it('test_renders_skeleton_when_no_events', () => {
    mockedStream.mockReturnValue(streamResult({ status: 'queued' }));
    renderTimeline();

    expect(screen.getByLabelText(/waiting for build to start/i)).toBeInTheDocument();
    expect(screen.getByText(/waiting for build to start/i)).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('test_renders_step_card_per_event', () => {
    const steps: BuildStep[] = [
      {
        step_name: 'clone-repo',
        phase_group: 'clone',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:00Z',
        ended_at: '2026-06-06T00:00:05Z',
        duration_seconds: 5,
      },
      {
        step_name: 'install-deps',
        phase_group: 'install',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:05Z',
        ended_at: '2026-06-06T00:00:30Z',
        duration_seconds: 25,
      },
    ];
    mockedStream.mockReturnValue(
      streamResult({ steps, status: 'running', currentStep: steps[1] }),
    );
    renderTimeline();

    expect(screen.getByText(/clone repo/i)).toBeInTheDocument();
    expect(screen.getByText(/install deps/i)).toBeInTheDocument();
  });

  it('test_current_step_has_live_indicator', () => {
    const running: BuildStep = {
      step_name: 'build-image',
      phase_group: 'build',
      status: 'running',
      started_at: '2026-06-06T00:00:00Z',
    };
    mockedStream.mockReturnValue(
      streamResult({
        steps: [running],
        status: 'running',
        currentStep: running,
      }),
    );
    renderTimeline();

    const card = screen.getByLabelText(/build image/i);
    expect(card.className).toContain('animate-pulse');
  });

  it('test_failed_step_shows_error', () => {
    const failed: BuildStep = {
      step_name: 'build-image',
      phase_group: 'build',
      status: 'failed',
      started_at: '2026-06-06T00:00:00Z',
      ended_at: '2026-06-06T00:01:00Z',
      duration_seconds: 60,
      error_class: 'OOM',
      error_message: 'Container exited 137',
      suggestion: 'Increase plan resources',
    };
    mockedStream.mockReturnValue(
      streamResult({
        steps: [failed],
        status: 'failed',
        currentStep: failed,
        error: {
          step_name: failed.step_name,
          error_class: 'OOM',
          error_message: failed.error_message ?? '',
          suggestion: failed.suggestion ?? '',
        },
      }),
    );
    renderTimeline();

    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText(/out of memory/i)).toBeInTheDocument();
    expect(screen.getByText(/increase plan resources/i)).toBeInTheDocument();
    expect(screen.queryByText(/deploy complete/i)).not.toBeInTheDocument();
  });

  it('test_all_succeeded_shows_celebration', () => {
    const steps: BuildStep[] = [
      {
        step_name: 'clone-repo',
        phase_group: 'clone',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:00Z',
        ended_at: '2026-06-06T00:00:05Z',
        duration_seconds: 5,
      },
    ];
    mockedStream.mockReturnValue(
      streamResult({
        steps,
        status: 'succeeded',
        currentStep: steps[0],
        totalDurationSeconds: 5,
      }),
    );
    renderTimeline();

    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByText(/deploy complete/i)).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('test_groups_steps_by_phase', () => {
    const steps: BuildStep[] = [
      {
        step_name: 'clone-repo',
        phase_group: 'clone',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:00Z',
        ended_at: '2026-06-06T00:00:05Z',
        duration_seconds: 5,
      },
      {
        step_name: 'install-deps',
        phase_group: 'install',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:05Z',
        ended_at: '2026-06-06T00:00:30Z',
        duration_seconds: 25,
      },
      {
        step_name: 'build-image',
        phase_group: 'build',
        status: 'succeeded',
        started_at: '2026-06-06T00:00:30Z',
        ended_at: '2026-06-06T00:01:30Z',
        duration_seconds: 60,
      },
      {
        step_name: 'sign-image',
        phase_group: 'sign',
        status: 'succeeded',
        started_at: '2026-06-06T00:01:30Z',
        ended_at: '2026-06-06T00:01:35Z',
        duration_seconds: 5,
      },
      {
        step_name: 'push-manifest',
        phase_group: 'deliver',
        status: 'succeeded',
        started_at: '2026-06-06T00:01:35Z',
        ended_at: '2026-06-06T00:01:40Z',
        duration_seconds: 5,
      },
    ];
    mockedStream.mockReturnValue(
      streamResult({
        steps,
        status: 'succeeded',
        currentStep: steps[steps.length - 1],
        totalDurationSeconds: 100,
      }),
    );
    renderTimeline();

    // Each phase has its own labelled <section>.
    expect(screen.getByLabelText(/clone phase/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/install phase/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/build phase/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/sign phase/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/deliver phase/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/other phase/i)).not.toBeInTheDocument();
  });

  it('test_unknown_step_renders_in_other_group_EC11', () => {
    // EC-11: backend can introduce a novel step before UI is updated;
    // PhaseGroupForStep returns "other" as fallback. Front MUST NOT crash
    // and MUST render the step under the Other phase group.
    const steps: BuildStep[] = [
      {
        step_name: 'novel-step',
        phase_group: 'other',
        status: 'running',
        started_at: '2026-06-06T00:00:00Z',
      },
    ];
    mockedStream.mockReturnValue(
      streamResult({
        steps,
        status: 'running',
        currentStep: steps[0],
      }),
    );
    renderTimeline();

    expect(screen.getByLabelText(/other phase/i)).toBeInTheDocument();
    expect(screen.getByText(/novel step/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/clone phase/i)).not.toBeInTheDocument();
  });
});

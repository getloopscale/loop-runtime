import { render, screen } from '@testing-library/react';
import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest';
import { BuildStepCard } from './build-step-card';
import type { BuildStep } from '@/lib/api/sse-events';

const baseStep: BuildStep = {
  step_name: 'clone-repo',
  phase_group: 'clone',
  status: 'succeeded',
  started_at: '2026-06-06T00:00:00Z',
  ended_at: '2026-06-06T00:00:05Z',
  duration_seconds: 5,
};

describe('BuildStepCard', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-06-06T00:00:30Z'));
  });
  afterEach(() => vi.useRealTimers());

  it('test_renders_step_name', () => {
    render(<BuildStepCard step={baseStep} />);
    expect(screen.getByText(/clone repo/i)).toBeInTheDocument();
  });

  it('test_renders_duration_when_succeeded', () => {
    render(<BuildStepCard step={baseStep} />);
    expect(screen.getByText('5s')).toBeInTheDocument();
  });

  it('test_aria_label_includes_failed_error_class', () => {
    render(
      <BuildStepCard
        step={{
          ...baseStep,
          status: 'failed',
          error_class: 'OOM',
        }}
      />,
    );
    const card = screen.getByLabelText(/clone repo failed: oom/i);
    expect(card).toBeInTheDocument();
  });

  it('test_long_step_name_truncates_with_title_attr_EC14', () => {
    const longName = 'validate-workspace-user-dockerfile-with-monorepo-package';
    render(
      <BuildStepCard
        step={{
          ...baseStep,
          step_name: longName,
        }}
      />,
    );
    const label = screen.getByTitle(longName);
    expect(label.className).toMatch(/truncate/);
  });

  it('test_active_running_step_pulses', () => {
    render(
      <BuildStepCard
        step={{ ...baseStep, status: 'running', ended_at: undefined }}
        active
      />,
    );
    const card = screen.getByLabelText(/clone repo/i);
    expect(card.className).toContain('animate-pulse');
  });
});

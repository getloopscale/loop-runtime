import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BuildStepTimer, formatDuration } from './build-step-timer';

describe('formatDuration', () => {
  it('format_47s', () => {
    expect(formatDuration(47)).toBe('47s');
  });
  it('format_1m_23s', () => {
    expect(formatDuration(83)).toBe('1m 23s');
  });
  it('format_2h_15m', () => {
    expect(formatDuration(2 * 3600 + 15 * 60)).toBe('2h 15m');
  });
  it('format_round_minute_omits_seconds', () => {
    expect(formatDuration(120)).toBe('2m');
  });
  it('format_negative_returns_dash', () => {
    expect(formatDuration(-5)).toBe('—');
  });
});

describe('BuildStepTimer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-06-06T00:00:30Z'));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('test_static_duration_when_ended', () => {
    const { getByLabelText } = render(
      <BuildStepTimer
        startedAt="2026-06-06T00:00:00Z"
        endedAt="2026-06-06T00:00:47Z"
      />,
    );
    expect(getByLabelText(/47s/i)).toBeInTheDocument();
  });

  it('test_initial_running_state_computes_duration', () => {
    const { container } = render(
      <BuildStepTimer startedAt="2026-06-06T00:00:00Z" />,
    );
    // 30s elapsed at system time 00:00:30 (set in beforeEach).
    expect(container.textContent).toBe('30s');
  });

  it('test_paused_does_not_tick', () => {
    const { container } = render(
      <BuildStepTimer startedAt="2026-06-06T00:00:00Z" paused />,
    );
    const before = container.textContent;
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(container.textContent).toBe(before);
  });
});

import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  ensureSSEToken: vi.fn(async () => 'tok'),
  clearToken: vi.fn(),
  hasValidToken: vi.fn(() => true),
  engineFetch: vi.fn(),
  cloudFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({
  API_URL: '',
}));

import { useDeploymentStream } from './use-deployment-stream';

class MockEventSource {
  static instances: MockEventSource[] = [];
  url: string;
  listeners: Record<string, Array<(ev: unknown) => void>> = {};
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    MockEventSource.instances.push(this);
    queueMicrotask(() => this.onopen?.({}));
  }
  addEventListener(type: string, l: (ev: unknown) => void) {
    (this.listeners[type] ??= []).push(l);
  }
  removeEventListener() {}
  emit(type: string, data: unknown, lastEventId = '') {
    const ev = { data: JSON.stringify(data), lastEventId };
    (this.listeners[type] ?? []).forEach((l) => l(ev));
  }
  close() {
    this.closed = true;
  }
}

beforeEach(() => {
  MockEventSource.instances = [];
  // @ts-expect-error global stub
  globalThis.EventSource = MockEventSource;
  vi.clearAllMocks();
});

afterEach(() => {
  // @ts-expect-error restore
  delete globalThis.EventSource;
});

const startedPayload = (stepName: string, startedAt: string) => ({
  step_name: stepName,
  started_at: startedAt,
  phase_group: 'clone',
});

const succeededPayload = (
  stepName: string,
  startedAt: string,
  endedAt: string,
  duration: number,
) => ({
  step_name: stepName,
  started_at: startedAt,
  ended_at: endedAt,
  duration_seconds: duration,
  phase_group: 'clone',
});

const failedPayload = (stepName: string) => ({
  step_name: stepName,
  started_at: '2026-06-06T00:00:00Z',
  ended_at: '2026-06-06T00:00:05Z',
  duration_seconds: 5,
  phase_group: 'build',
  error_class: 'OOM',
  error_message: 'Container OOMKilled',
  suggestion: 'Increase plan resources',
});

describe('useDeploymentStream', () => {
  it('test_initial_state_no_events_returns_queued', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    expect(result.current.status).toBe('queued');
    expect(result.current.steps).toEqual([]);
    expect(result.current.currentStep).toBeNull();
  });

  it('test_one_step_started_returns_running_with_currentStep', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    act(() => {
      MockEventSource.instances[0].emit(
        'build.step.started',
        startedPayload('clone-repo', '2026-06-06T00:00:00Z'),
        '1',
      );
    });
    await waitFor(() => expect(result.current.steps.length).toBe(1));
    expect(result.current.status).toBe('running');
    expect(result.current.currentStep?.step_name).toBe('clone-repo');
  });

  it('test_all_steps_succeeded_returns_succeeded', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit('build.step.started', startedPayload('clone-repo', '2026-06-06T00:00:00Z'), '1');
      es.emit('build.step.succeeded', succeededPayload('clone-repo', '2026-06-06T00:00:00Z', '2026-06-06T00:00:10Z', 10), '2');
      es.emit('build.step.started', startedPayload('build-image-api', '2026-06-06T00:00:10Z'), '3');
      es.emit('build.step.succeeded', succeededPayload('build-image-api', '2026-06-06T00:00:10Z', '2026-06-06T00:00:30Z', 20), '4');
    });
    await waitFor(() => expect(result.current.status).toBe('succeeded'));
    expect(result.current.steps.length).toBe(2);
  });

  it('test_step_failed_returns_failed_with_error_class', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    act(() => {
      MockEventSource.instances[0].emit('build.step.failed', failedPayload('build-image-api'), '1');
    });
    await waitFor(() => expect(result.current.status).toBe('failed'));
    expect(result.current.error?.error_class).toBe('OOM');
  });

  it('test_currentStep_is_last_running', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    act(() => {
      MockEventSource.instances[0].emit('build.step.started', startedPayload('a', '2026-06-06T00:00:00Z'), '1');
      MockEventSource.instances[0].emit('build.step.succeeded', succeededPayload('a', '2026-06-06T00:00:00Z', '2026-06-06T00:00:05Z', 5), '2');
      MockEventSource.instances[0].emit('build.step.started', startedPayload('b', '2026-06-06T00:00:05Z'), '3');
    });
    await waitFor(() => expect(result.current.currentStep?.step_name).toBe('b'));
  });

  it('test_totalDuration_sums_completed_steps', async () => {
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    act(() => {
      MockEventSource.instances[0].emit('build.step.succeeded', succeededPayload('a', '2026-06-06T00:00:00Z', '2026-06-06T00:00:05Z', 5), '1');
      MockEventSource.instances[0].emit('build.step.succeeded', succeededPayload('b', '2026-06-06T00:00:05Z', '2026-06-06T00:00:13Z', 8), '2');
    });
    await waitFor(() => expect(result.current.totalDurationSeconds).toBe(13));
  });

  it('test_no_step_duplicate_after_rest_then_sse_EC5', async () => {
    // Simulate REST seeded the same 3 steps + SSE replays them — Map
    // upsert by step_name dedups so steps.length stays 3.
    const { result } = renderHook(() => useDeploymentStream('build-abc'));
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    const es = MockEventSource.instances[0];
    act(() => {
      es.emit('build.step.succeeded', succeededPayload('a', '2026-06-06T00:00:00Z', '2026-06-06T00:00:05Z', 5), '1');
      es.emit('build.step.succeeded', succeededPayload('b', '2026-06-06T00:00:05Z', '2026-06-06T00:00:10Z', 5), '2');
      es.emit('build.step.succeeded', succeededPayload('c', '2026-06-06T00:00:10Z', '2026-06-06T00:00:15Z', 5), '3');
      // duplicate replays
      es.emit('build.step.succeeded', succeededPayload('a', '2026-06-06T00:00:00Z', '2026-06-06T00:00:05Z', 5), '4');
      es.emit('build.step.succeeded', succeededPayload('b', '2026-06-06T00:00:05Z', '2026-06-06T00:00:10Z', 5), '5');
    });
    await waitFor(() => expect(result.current.steps.length).toBe(3));
  });

  it('test_lastEventID_from_REST_seeds_url_EC5', async () => {
    renderHook(() =>
      useDeploymentStream('build-abc', { initialLastEventID: '99' }),
    );
    await waitFor(() => expect(MockEventSource.instances.length).toBe(1));
    expect(MockEventSource.instances[0].url).toContain('last_event_id=99');
  });
});

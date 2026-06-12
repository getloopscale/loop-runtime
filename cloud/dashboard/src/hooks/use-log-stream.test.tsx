import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useLogStream } from './use-log-stream';
import type { LogLine } from '@/lib/api/types';

// Mock ensureSSEToken to bypass network — returns deterministic token.
vi.mock('@/lib/api', () => ({
  ensureSSEToken: vi.fn(async () => 'mock-jwt'),
}));

/**
 * Minimal EventSource mock — J-19 T1.3 only needs onmessage emission.
 * T2.4 will adopt a richer Heroku-style MockEventSource canonical pattern.
 */
class TestEventSource {
  static instances: TestEventSource[] = [];
  url: string;
  onopen: ((ev: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  readyState = 0;

  constructor(url: string) {
    this.url = url;
    TestEventSource.instances.push(this);
    // Fire onopen on next microtask (simulate async connect)
    Promise.resolve().then(() => {
      this.readyState = 1;
      this.onopen?.(new Event('open'));
    });
  }

  emit(line: LogLine) {
    const ev = new MessageEvent('message', { data: JSON.stringify(line) });
    this.onmessage?.(ev);
  }

  close() {
    this.readyState = 2;
  }
}

const TS_BASE = Date.now();
const mkLine = (i: number): LogLine => ({
  ts: new Date(TS_BASE + i).toISOString(),
  level: 'info',
  msg: `line ${i}`,
});

describe('useLogStream (J-19 T1.3 — R3 droppedCount field)', () => {
  beforeEach(() => {
    TestEventSource.instances = [];
    (globalThis as { EventSource: typeof EventSource }).EventSource =
      TestEventSource as unknown as typeof EventSource;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  async function setupHook(maxLines: number) {
    const r = renderHook(() => useLogStream('/test/path', { maxLines }));
    await waitFor(() => expect(TestEventSource.instances.length).toBe(1));
    return r;
  }

  it('test_use_log_stream_dropped_count_one_after_5001_lines', async () => {
    const { result } = await setupHook(5000);
    const es = TestEventSource.instances[0];
    act(() => {
      for (let i = 0; i < 5001; i++) es.emit(mkLine(i));
    });
    expect(result.current.droppedCount).toBe(1);
    expect(result.current.lines.length).toBe(5000);
  });

  it('test_use_log_stream_dropped_count_one_thousand_after_6000_lines', async () => {
    const { result } = await setupHook(5000);
    const es = TestEventSource.instances[0];
    act(() => {
      for (let i = 0; i < 6000; i++) es.emit(mkLine(i));
    });
    expect(result.current.droppedCount).toBe(1000);
    expect(result.current.lines.length).toBe(5000);
  });

  it('test_use_log_stream_dropped_count_resets_on_clear', async () => {
    const { result } = await setupHook(100);
    const es = TestEventSource.instances[0];
    act(() => {
      for (let i = 0; i < 250; i++) es.emit(mkLine(i));
    });
    expect(result.current.droppedCount).toBe(150);
    act(() => {
      result.current.clear();
    });
    expect(result.current.droppedCount).toBe(0);
    expect(result.current.lines.length).toBe(0);
  });

  it('test_use_log_stream_backward_compat_destructuring_no_droppedCount', async () => {
    // EC-6 backward compat: existing build-logs.tsx destructures
    // {lines, connected, error, clear} without droppedCount. New field
    // is additive (extension, not breaking) — destructuring without it
    // must still resolve every existing field.
    const { result } = await setupHook(5000);
    const { lines, connected, error, clear } = result.current;
    expect(Array.isArray(lines)).toBe(true);
    expect(typeof connected).toBe('boolean');
    expect(typeof error).toBe('boolean');
    expect(typeof clear).toBe('function');
  });
});

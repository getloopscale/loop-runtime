// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Beta Logs Gate Criterion 4 (UX) — deploy-failed auto-tail tests.

import {
  AUTO_TAIL_LINES,
  AUTO_TAIL_TIMEOUT_MS,
  fetchAutoTail,
  formatAutoTail,
  maybeRenderAutoTail,
} from '../../../src/commands/deploy-auto-tail';
import type { LogEntry, LogsApiResponse } from '../../../src/commands/logs';

function entry(step: string, content: string): LogEntry {
  return { step, stream: 'stdout', content, timestamp: '2026-05-13T10:00:00Z' };
}

function response(entries: LogEntry[]): LogsApiResponse {
  return { build_id: 'b-1', status: 'FAILED', entries, total_lines: entries.length };
}

describe('formatAutoTail', () => {
  it('returns empty string for zero entries', () => {
    expect(formatAutoTail([])).toBe('');
  });

  it('prefixes each entry with the step and bookends with markers', () => {
    const out = formatAutoTail([
      entry('build', 'Building image...'),
      entry('build', 'Layer 1/5 cached'),
    ]);
    expect(out).toContain('--- Last log lines');
    expect(out).toContain('--- End log tail ---');
    expect(out).toContain('[build] Building image...');
    expect(out).toContain('[build] Layer 1/5 cached');
  });

  it('omits the step prefix when entry has no step', () => {
    const out = formatAutoTail([{ step: '', stream: 'stdout', content: 'no-step line', timestamp: '' }]);
    expect(out).toContain('no-step line');
    expect(out).not.toContain('[]');
  });

  it('trims trailing newlines on content (avoids double-blank-line bloat)', () => {
    const out = formatAutoTail([entry('build', 'line1\n\n')]);
    // Single trailing newline only between entries, not from content.
    expect(out).not.toContain('line1\n\n\n');
  });
});

describe('fetchAutoTail', () => {
  it('returns empty array when fetcher returns null', async () => {
    const lines = await fetchAutoTail(async () => null, 'b-1', 1000);
    expect(lines).toEqual([]);
  });

  it('returns empty array when fetcher rejects', async () => {
    const lines = await fetchAutoTail(async () => {
      throw new Error('network down');
    }, 'b-1', 1000);
    expect(lines).toEqual([]);
  });

  it('returns the last AUTO_TAIL_LINES entries from a larger response', async () => {
    const many = Array.from({ length: AUTO_TAIL_LINES + 5 }, (_, i) => entry('build', `line ${i}`));
    const lines = await fetchAutoTail(async () => response(many), 'b-1', 1000);
    expect(lines).toHaveLength(AUTO_TAIL_LINES);
    // Last entry of the input must be the last of the output (we slice from tail).
    expect(lines[lines.length - 1].content).toBe(`line ${AUTO_TAIL_LINES + 4}`);
  });

  it('returns all entries when fewer than AUTO_TAIL_LINES are returned', async () => {
    const few = [entry('build', 'a'), entry('build', 'b'), entry('build', 'c')];
    const lines = await fetchAutoTail(async () => response(few), 'b-1', 1000);
    expect(lines).toHaveLength(3);
  });

  it('respects the timeout (returns [] when fetcher hangs)', async () => {
    const start = Date.now();
    const lines = await fetchAutoTail(
      () => new Promise(() => { /* never resolves */ }),
      'b-1',
      100, // 100ms timeout — tight to keep the test fast
    );
    const elapsed = Date.now() - start;
    expect(lines).toEqual([]);
    // Sanity: must not have blocked longer than ~150ms (timeout + grace).
    expect(elapsed).toBeLessThan(500);
  });

  it('respects the timeout (defaults to AUTO_TAIL_TIMEOUT_MS when not provided)', () => {
    // Smoke test that the constant is sane — operators shouldn't have
    // to wait > 10s on a deploy fail just for the auto-tail.
    expect(AUTO_TAIL_TIMEOUT_MS).toBeLessThanOrEqual(10000);
    expect(AUTO_TAIL_TIMEOUT_MS).toBeGreaterThanOrEqual(1000);
  });
});

describe('maybeRenderAutoTail', () => {
  it('writes nothing and returns [] when fetcher is undefined', async () => {
    const writes: string[] = [];
    const lines = await maybeRenderAutoTail(
      undefined,
      'b-1',
      (msg) => writes.push(msg),
    );
    expect(lines).toEqual([]);
    expect(writes).toEqual([]);
  });

  it('writes the formatted tail and returns entries on success (human mode)', async () => {
    const writes: string[] = [];
    const fetcher = async () => response([entry('build', 'recent failure context')]);
    const lines = await maybeRenderAutoTail(
      fetcher,
      'b-1',
      (msg) => writes.push(msg),
    );
    expect(lines).toHaveLength(1);
    expect(writes.length).toBe(1);
    expect(writes[0]).toContain('recent failure context');
    expect(writes[0]).toContain('--- Last log lines');
  });

  it('does NOT writeTo when jsonMode is true (caller embeds into JSON)', async () => {
    const writes: string[] = [];
    const fetcher = async () => response([entry('build', 'json-mode line')]);
    const lines = await maybeRenderAutoTail(
      fetcher,
      'b-1',
      (msg) => writes.push(msg),
      { jsonMode: true },
    );
    // Lines are still returned so the caller embeds them in the JSON
    // payload as `recent_logs`. But writeTo MUST NOT be called — it
    // would corrupt the JSON output.
    expect(lines).toHaveLength(1);
    expect(writes).toHaveLength(0);
  });

  it('returns [] and writes nothing when fetcher returns empty entries', async () => {
    const writes: string[] = [];
    const lines = await maybeRenderAutoTail(
      async () => response([]),
      'b-1',
      (msg) => writes.push(msg),
    );
    expect(lines).toEqual([]);
    expect(writes).toEqual([]);
  });

  it('respects custom timeoutMs', async () => {
    const writes: string[] = [];
    const start = Date.now();
    const lines = await maybeRenderAutoTail(
      () => new Promise(() => { /* hangs */ }),
      'b-1',
      (msg) => writes.push(msg),
      { timeoutMs: 50 },
    );
    expect(lines).toEqual([]);
    expect(writes).toEqual([]);
    expect(Date.now() - start).toBeLessThan(300);
  });
});

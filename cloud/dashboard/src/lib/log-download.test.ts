import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { formatLineForExport, triggerLogDownload } from './log-download';
import type { LogLine } from './api/types';

describe('formatLineForExport', () => {
  it('test_format_line_includes_ts_level_source_msg', () => {
    const line: LogLine = {
      ts: '2026-06-12T20:00:00.000Z',
      level: 'error',
      source: 'web-1',
      msg: 'connection refused',
    };
    const out = formatLineForExport(line);
    expect(out).toBe('2026-06-12T20:00:00.000Z ERROR web-1 connection refused');
  });

  it('test_format_line_without_source_skips_source_token', () => {
    const line: LogLine = {
      ts: '2026-06-12T20:00:00.000Z',
      level: 'info',
      msg: 'plain',
    };
    expect(formatLineForExport(line)).toBe('2026-06-12T20:00:00.000Z INFO plain');
  });
});

describe('triggerLogDownload (J-19 T2.3 — R7 SHOULD + EC-4 revoke timing)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('test_download_creates_blob_url_and_revokes_after_1000ms_EC4', () => {
    const createSpy = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:mock');
    const revokeSpy = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    const lines: LogLine[] = [
      { ts: '2026-06-12T20:00:00.000Z', level: 'info', msg: 'line one' },
    ];

    triggerLogDownload(lines);
    expect(createSpy).toHaveBeenCalled();
    // EC-4: revoke MUST be deferred so Safari/Firefox can finish streaming
    expect(revokeSpy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(999);
    expect(revokeSpy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(2);
    expect(revokeSpy).toHaveBeenCalledWith('blob:mock');
  });
});

import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import { formatLocalTime } from './format-ts';

const ORIG_TZ = process.env.TZ;

describe('formatLocalTime (J-19 T2.5 — R9 SHOULD)', () => {
  beforeAll(() => {
    process.env.TZ = 'UTC';
  });
  afterAll(() => {
    process.env.TZ = ORIG_TZ;
  });

  it('test_format_ts_iso_displays_local_with_utc_tooltip', () => {
    const result = formatLocalTime('2026-06-12T20:23:42.000Z');
    expect(result.local).toBe('20:23:42');
    expect(result.iso).toBe('2026-06-12T20:23:42.000Z');
  });

  it('test_format_ts_invalid_ts_falls_back_to_raw', () => {
    const result = formatLocalTime('not-a-date');
    expect(result.local).toBe('not-a-date');
    expect(result.iso).toBe('not-a-date');
  });

  it('test_format_ts_non_utc_tz_renders_local_zone', () => {
    process.env.TZ = 'America/Sao_Paulo';
    const result = formatLocalTime('2026-06-12T20:23:42.000Z');
    // São Paulo is UTC-3 — 20:23 UTC → 17:23 local
    expect(result.local).toBe('17:23:42');
    expect(result.iso).toBe('2026-06-12T20:23:42.000Z');
    process.env.TZ = 'UTC'; // restore
  });
});

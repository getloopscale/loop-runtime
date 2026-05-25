import { describe, it, expect } from 'vitest';
import { formatDate, formatNumber } from './format';

describe('formatDate', () => {
  it('test_format_date_handles_4_intents', () => {
    const iso = '2026-05-25T06:42:00Z';
    // en-US locale per ADR-D1; exact format depends on Intl but key tokens
    // (May, 25, 2026) must be present in date/datetime/long intents.
    expect(formatDate(iso, 'date')).toMatch(/May 25, 2026/);
    expect(formatDate(iso, 'datetime')).toMatch(/May 25, 2026/);
    expect(formatDate(iso, 'long')).toMatch(/Monday, May 25, 2026/);
    expect(formatDate(iso, 'relative')).toMatch(/(year|month|day|ago|in)/);
  });

  it('test_format_date_returns_em_dash_for_null', () => {
    expect(formatDate(null, 'date')).toBe('—');
    expect(formatDate(undefined, 'date')).toBe('—');
  });

  // EC-2 (MUST FIX): empty string defensive guard
  it('test_format_date_returns_em_dash_for_empty_string', () => {
    // Today `!''` is true so guard catches; this test pins the invariant
    // in case future refactor changes the guard to `=== null/undefined`.
    expect(formatDate('', 'date')).toBe('—');
    expect(formatDate('', 'datetime')).toBe('—');
    expect(formatDate('', 'relative')).toBe('—');
    expect(formatDate('', 'long')).toBe('—');
  });

  it('test_format_date_returns_em_dash_for_invalid_iso', () => {
    // new Date('not-a-date') → Invalid Date → guard returns em-dash
    expect(formatDate('not-a-date', 'date')).toBe('—');
    expect(formatDate('2026-13-99', 'date')).toBe('—');
  });
});

describe('formatNumber', () => {
  it('test_format_number_compact_formats_millions', () => {
    expect(formatNumber(1_240_000, 'compact')).toBe('1.24M');
    expect(formatNumber(1_500_000_000, 'compact')).toBe('1.5B');
    expect(formatNumber(500, 'compact')).toBe('500');
  });

  it('test_format_number_grouped_uses_thousand_separator', () => {
    expect(formatNumber(1_240_000, 'grouped')).toBe('1,240,000');
    expect(formatNumber(0, 'grouped')).toBe('0');
  });

  it('test_format_number_bytes_handles_KB_MB_GB', () => {
    // SI 1000-base per ADR-D4
    expect(formatNumber(500, 'bytes')).toBe('500 B');
    expect(formatNumber(1500, 'bytes')).toBe('1.5 KB');
    expect(formatNumber(1_500_000, 'bytes')).toBe('1.5 MB');
    expect(formatNumber(412_000_000_000, 'bytes')).toBe('412 GB');
  });

  it('test_format_number_returns_em_dash_for_NaN', () => {
    expect(formatNumber(NaN, 'compact')).toBe('—');
    expect(formatNumber(NaN, 'grouped')).toBe('—');
    expect(formatNumber(NaN, 'bytes')).toBe('—');
    expect(formatNumber(NaN, 'percent')).toBe('—');
  });

  // EC-8 (SHOULD TEST): Infinity guard — same family as NaN
  it('test_format_number_returns_em_dash_for_Infinity', () => {
    expect(formatNumber(Infinity, 'compact')).toBe('—');
    expect(formatNumber(-Infinity, 'compact')).toBe('—');
    expect(formatNumber(Infinity, 'bytes')).toBe('—');
  });

  it('test_format_number_percent_handles_fraction', () => {
    expect(formatNumber(0.423, 'percent')).toBe('42.3%');
    expect(formatNumber(0, 'percent')).toBe('0%');
    expect(formatNumber(1, 'percent')).toBe('100%');
  });
});

import { describe, expect, it } from 'vitest';
import { parseDotenv } from './dotenv';

describe('parseDotenv — J-22 T3.5', () => {
  it('test_parses_basic_key_value', () => {
    const { entries, warnings } = parseDotenv('DATABASE_URL=postgres://x');
    expect(warnings).toEqual([]);
    expect(entries).toEqual([{ key: 'DATABASE_URL', value: 'postgres://x', line: 1 }]);
  });

  it('test_strips_double_quotes_ec17', () => {
    const { entries, warnings } = parseDotenv('STRIPE_KEY="sk_live_with spaces"');
    expect(warnings).toEqual([]);
    expect(entries[0].value).toBe('sk_live_with spaces');
  });

  it('test_strips_single_quotes_ec17', () => {
    const { entries, warnings } = parseDotenv("MY_KEY='single quoted'");
    expect(warnings).toEqual([]);
    expect(entries[0].value).toBe('single quoted');
  });

  it('test_skips_comment_lines_ec17', () => {
    const src = ['# header comment', 'DATABASE_URL=postgres://x', '# trailing'].join('\n');
    const { entries, warnings } = parseDotenv(src);
    expect(warnings).toEqual([]);
    expect(entries).toHaveLength(1);
  });

  it('test_skips_empty_lines', () => {
    const src = '\n\nDATABASE_URL=x\n\n';
    const { entries } = parseDotenv(src);
    expect(entries).toHaveLength(1);
  });

  it('test_warns_on_malformed_line', () => {
    const src = ['DATABASE_URL=ok', 'not-a-valid-line', 'API_KEY=z'].join('\n');
    const { entries, warnings } = parseDotenv(src);
    expect(entries).toHaveLength(2);
    expect(warnings).toHaveLength(1);
    expect(warnings[0].line).toBe(2);
    expect(warnings[0].message).toMatch(/Malformed/);
  });

  it('test_warns_on_invalid_key_pattern', () => {
    const src = 'lower=bad';
    const { entries, warnings } = parseDotenv(src);
    expect(entries).toHaveLength(0);
    expect(warnings).toHaveLength(1);
    expect(warnings[0].message).toMatch(/Invalid key/);
  });

  it('test_warns_on_duplicate_key_ec14', () => {
    const src = ['DATABASE_URL=x', 'DATABASE_URL=y'].join('\n');
    const { entries, warnings } = parseDotenv(src);
    expect(entries).toHaveLength(1);
    expect(entries[0].value).toBe('x'); // first wins
    expect(warnings).toHaveLength(1);
    expect(warnings[0].message).toMatch(/Duplicate/);
  });

  it('test_plan_invariant_5_valid_1_malformed', () => {
    // Plan T3.5 TDD: ".env file with 5 valid + 1 malformed lines" →
    // "preview MUST show 5 entries + 1 warning"
    const src = [
      'KEY_A=a',
      'KEY_B=b',
      'this-is-malformed', // no =
      'KEY_C=c',
      'KEY_D=d',
      'KEY_E=e',
    ].join('\n');
    const { entries, warnings } = parseDotenv(src);
    expect(entries).toHaveLength(5);
    expect(warnings).toHaveLength(1);
  });
});

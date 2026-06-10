// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { runAdminEvict, AdminEvictDeps, AdminEvictInput, AdminEvictResponse } from '../../src/commands/admin-evict';

function makeDeps(overrides: Partial<AdminEvictDeps> = {}): {
  deps: AdminEvictDeps;
  out: string[];
  err: string[];
  postCalls: AdminEvictInput[];
} {
  const out: string[] = [];
  const err: string[] = [];
  const postCalls: AdminEvictInput[] = [];

  const defaultPost = async (input: AdminEvictInput): Promise<AdminEvictResponse> => {
    postCalls.push(input);
    return input.dry_run
      ? { job_id: 'dry-run', queued: 2, matched: ['tn-a', 'tn-b'], accepted: false }
      : { job_id: 'job-123', queued: 2, accepted: true };
  };

  const deps: AdminEvictDeps = {
    getServiceAuth: () => 'fake-secret',
    postEvict: defaultPost,
    writeTo: (m: string) => out.push(m),
    writeError: (m: string) => err.push(m),
    prompt: async () => 'yes',
    ...overrides,
  };

  return { deps, out, err, postCalls };
}

describe('runAdminEvict', () => {
  test('missing LOOPZERO_SERVICE_AUTH returns exit 4', async () => {
    const { deps, err } = makeDeps({ getServiceAuth: () => null });
    const code = await runAdminEvict({ pattern: 'tn-lt-%' }, deps);
    expect(code).toBe(4);
    expect(err.join('\n')).toMatch(/LOOPZERO_SERVICE_AUTH/);
  });

  test('missing both --pattern and --ids returns exit 1', async () => {
    const { deps, err } = makeDeps();
    const code = await runAdminEvict({}, deps);
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/pattern.*ids/i);
  });

  test('dangerous pattern "%" rejected with exit 1', async () => {
    const { deps, err } = makeDeps();
    const code = await runAdminEvict({ pattern: '%' }, deps);
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/too broad/i);
  });

  test('dangerous pattern "tn-%" rejected with exit 1', async () => {
    const { deps, err } = makeDeps();
    const code = await runAdminEvict({ pattern: 'tn-%' }, deps);
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/too broad/i);
  });

  test('--dry-run prints matches without committing', async () => {
    const { deps, out, postCalls } = makeDeps();
    const code = await runAdminEvict({ pattern: 'tn-lt-%', dryRun: true }, deps);
    expect(code).toBe(0);
    expect(postCalls).toHaveLength(1);
    expect(postCalls[0].dry_run).toBe(true);
    expect(out.join('\n')).toMatch(/tn-a/);
    expect(out.join('\n')).toMatch(/tn-b/);
  });

  test('--json outputs raw preview without prompting', async () => {
    const { deps, out } = makeDeps();
    const code = await runAdminEvict({ pattern: 'tn-lt-%', json: true }, deps);
    expect(code).toBe(0);
    const parsed = JSON.parse(out[0]);
    expect(parsed.matched).toEqual(['tn-a', 'tn-b']);
  });

  test('--yes skips prompt and commits eviction', async () => {
    const { deps, postCalls } = makeDeps();
    const code = await runAdminEvict({ pattern: 'tn-lt-%', yes: true }, deps);
    expect(code).toBe(0);
    // Two calls: preview then live.
    expect(postCalls).toHaveLength(2);
    expect(postCalls[0].dry_run).toBe(true);
    expect(postCalls[1].dry_run).toBe(false);
  });

  // CHG-2026-04-29-DOGFOOD iter 13 — F-CLI-1 fix.
  // Validation errors must emit JSON to stdout when --json is set, NOT plain
  // text to stderr. Otherwise CLI consumers piping to jq see empty stdout +
  // non-zero exit and have to fall back to text scraping (Rule 5 violation).
  describe('--json validation error contract (F-CLI-1)', () => {
    test('missing pattern + ids → JSON error on stdout, exit 1', async () => {
      const { deps, out, err } = makeDeps();
      const code = await runAdminEvict({ json: true }, deps);
      expect(code).toBe(1);
      expect(err).toEqual([]); // nothing on stderr
      expect(out).toHaveLength(1);
      const parsed = JSON.parse(out[0]);
      expect(parsed.exit_code).toBe(1);
      expect(parsed.code).toBe('MISSING_FILTER');
      expect(parsed.error).toMatch(/pattern.*ids/i);
    });

    test('missing service auth → JSON error on stdout, exit 4', async () => {
      const { deps, out, err } = makeDeps({ getServiceAuth: () => null });
      const code = await runAdminEvict({ pattern: 'tn-lt-%', json: true }, deps);
      expect(code).toBe(4);
      expect(err).toEqual([]);
      const parsed = JSON.parse(out[0]);
      expect(parsed.exit_code).toBe(4);
      expect(parsed.code).toBe('AUTH_REQUIRED');
    });

    test('too-broad pattern → JSON error on stdout, exit 1', async () => {
      const { deps, out, err } = makeDeps();
      const code = await runAdminEvict({ pattern: '%', json: true }, deps);
      expect(code).toBe(1);
      expect(err).toEqual([]);
      const parsed = JSON.parse(out[0]);
      expect(parsed.code).toBe('PATTERN_TOO_BROAD');
    });

    test('without --json, errors still go to stderr (preserves human flow)', async () => {
      const { deps, err } = makeDeps();
      const code = await runAdminEvict({}, deps);
      expect(code).toBe(1);
      expect(err.length).toBeGreaterThan(0);
    });
  });

  test('prompt "no" aborts with exit 1', async () => {
    const { deps, err, postCalls } = makeDeps({
      prompt: async () => 'no',
    });
    const code = await runAdminEvict({ pattern: 'tn-lt-%' }, deps);
    expect(code).toBe(1);
    // Only preview call — no commit after abort.
    expect(postCalls).toHaveLength(1);
    expect(err.join('\n')).toMatch(/Aborted/);
  });

  test('non-accepted response returns exit 5', async () => {
    const { deps } = makeDeps({
      postEvict: async (input) => ({
        job_id: input.dry_run ? 'dry-run' : '',
        queued: 2,
        matched: input.dry_run ? ['tn-a', 'tn-b'] : undefined,
        accepted: false,
      }),
    });
    const code = await runAdminEvict({ pattern: 'tn-lt-%', yes: true }, deps);
    expect(code).toBe(5);
  });

  test('--ids works without --pattern', async () => {
    const { deps, postCalls } = makeDeps();
    const code = await runAdminEvict({ ids: 'tn-a,tn-b', yes: true }, deps);
    expect(code).toBe(0);
    expect(postCalls[0].ids).toEqual(['tn-a', 'tn-b']);
  });
});

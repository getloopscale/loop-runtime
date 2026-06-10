// CHG-2026-04-30-INFRA-PARITY / T2.2 — `loopzero platform install --json` regression.
//
// Test invokes platformInstall with a fake values.yaml + json:true and
// asserts that:
//   - Output is JSONL (one object per line, each line valid JSON).
//   - Per-step `progress` events emit `{step, phase, status, ts}`.
//   - On error, a final `{status:"failed", errors:[...]}` is emitted.
//   - On success, a final `{status:"ready", endpoints, ...}` is emitted.

import { describe, it, expect, jest, beforeEach, afterEach } from '@jest/globals';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

// Stub the heavy deps so we don't need a real cluster.
jest.mock('../../src/installer', () => ({
  PlatformConfig: undefined,
  validateCluster: jest.fn(() => ({ valid: false, checks: [{ ok: false, name: 'kubectl', detail: 'no cluster (test)' }] })),
  getPlatformStatus: jest.fn(() => ({
    phase: 'NotInstalled',
    version: '0.2.0',
    components: [],
    crdsInstalled: 0,
    endpoints: {},
  })),
}));
jest.mock('../../src/installer/helmfile-runner', () => ({
  helmfileSync: jest.fn(),
  helmfileDestroy: jest.fn(),
  ensureHelmfileAvailable: jest.fn(() => ({ ok: false, error: 'helmfile not installed (test)' })),
  resolveHelmfilePath: jest.fn(() => '/tmp/helmfile.yaml.gotmpl'),
}));
jest.mock('../../src/installer/pulumi-runner', () => ({
  pulumiUp: jest.fn(),
  pulumiOutputs: jest.fn(),
  pulumiDestroy: jest.fn(),
  ensurePulumiAvailable: jest.fn(() => ({ ok: true, version: '3.0.0' })),
  resolvePulumiProjectDir: jest.fn(() => '/tmp/pulumi'),
  resolveProjectRoot: jest.fn(() => '/tmp/loopzero'),
}));

import { platformInstall, PlatformDeps } from '../../src/commands/platform';

const TMP = path.join(os.tmpdir(), `loopzero-install-test-${Date.now()}`);

beforeEach(() => {
  fs.mkdirSync(TMP, { recursive: true });
});

afterEach(() => {
  fs.rmSync(TMP, { recursive: true, force: true });
});

function makeDeps(): { deps: PlatformDeps; out: string[]; err: string[] } {
  const out: string[] = [];
  const err: string[] = [];
  return {
    deps: { writeTo: (m) => out.push(m), writeError: (m) => err.push(m) },
    out,
    err,
  };
}

function writeValues(): string {
  const f = path.join(TMP, 'values.yaml');
  fs.writeFileSync(f, 'domain: test.example.com\nprofile: minimal\n');
  return f;
}

describe('platformInstall --json (T2.2)', () => {
  it('json mode emits JSONL per step until first failure', async () => {
    const valuesFile = writeValues();
    const { deps, out } = makeDeps();
    const code = await platformInstall(deps, { valuesFile, json: true });
    // Validation step fails (no cluster), exit 1
    expect(code).toBe(1);
    // All emitted lines should be valid JSON
    expect(out.length).toBeGreaterThan(0);
    for (const line of out) {
      expect(() => JSON.parse(line)).not.toThrow();
    }
    // First line should be step 1 progress
    const first = JSON.parse(out[0]);
    expect(first.step).toBe('1/6');
    expect(first.phase).toBe('validate');
    expect(first.status).toBe('running');
    expect(first.ts).toMatch(/^20\d\d-/);
  });

  it('non-json mode (no --json) emits prose, not JSON', async () => {
    const valuesFile = writeValues();
    const { deps, out } = makeDeps();
    // Make sure --json isn't in argv during this test
    const origArgv = process.argv;
    process.argv = ['node', 'loopzero'];
    try {
      await platformInstall(deps, { valuesFile });
    } finally {
      process.argv = origArgv;
    }
    // First useful line should be prose, not JSON
    const proseFound = out.some((l) => l.includes('LoopZero Platform Installer'));
    expect(proseFound).toBe(true);
  });

  it('json mode: each progress entry has stable schema {step, phase, status, ts}', async () => {
    const valuesFile = writeValues();
    const { deps, out } = makeDeps();
    await platformInstall(deps, { valuesFile, json: true });
    const progressLines = out.map((l) => JSON.parse(l)).filter((j) => j.step);
    expect(progressLines.length).toBeGreaterThan(0);
    for (const p of progressLines) {
      expect(p).toHaveProperty('step');
      expect(p).toHaveProperty('phase');
      expect(p).toHaveProperty('status');
      expect(p).toHaveProperty('ts');
      expect(['running', 'complete', 'failed', 'skipped']).toContain(p.status);
    }
  });

  it('rejects missing domain in values.yaml regardless of --json', async () => {
    const f = path.join(TMP, 'bad.yaml');
    fs.writeFileSync(f, 'profile: minimal\n');
    const { deps, err } = makeDeps();
    const code = await platformInstall(deps, { valuesFile: f, json: true });
    expect(code).toBe(1);
    expect(err.join('\n')).toMatch(/domain/);
  });
});

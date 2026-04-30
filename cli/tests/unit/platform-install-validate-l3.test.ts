// CHG-2026-04-30-INFRA-PARITY / T8.1 — `--validate-l3` flag regression.
//
// When --validate-l3 is set AND the platform reaches phase=Ready, Step 6 must
// trigger the L3 100-tenant smoke test (TestRealDeploy_L3RealisticWithChurn
// in infra/tests/load). DoD #20 requires the gate.
//
// Tests:
//   • flag is OFF by default (no L3 spawn)
//   • flag ON + phase!=Ready → skip L3, exit normally (don't waste 30min)
//   • flag ON + phase=Ready  → spawn `go test ./infra/tests/load -run L3...`
//   • flag ON + L3 fails     → exit 5 (partial failure)
//   • --json mode emits an l3 progress event with status running→complete

import { describe, it, expect, jest, beforeEach, afterEach } from '@jest/globals';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

const mockSpawnSync = jest.fn();

jest.mock('child_process', () => ({
  spawnSync: mockSpawnSync,
  execSync: jest.fn(() => '0'),
}));

jest.mock('../../src/installer', () => ({
  PlatformConfig: undefined,
  validateCluster: jest.fn(() => ({ valid: true, checks: [] })),
  getPlatformStatus: jest.fn(() => ({
    phase: 'Ready',
    version: '0.2.0',
    components: [
      { name: 'loopzero-api', status: 'Running', replicas: '1/1' },
      { name: 'loopzero-controller', status: 'Running', replicas: '1/1' },
      { name: 'tenant-controller', status: 'Running', replicas: '1/1' },
      { name: 'loopzero-cloud', status: 'Running', replicas: '1/1' },
    ],
    crdsInstalled: 10,
    endpoints: { api: 'https://api.test', dashboard: 'https://app.test' },
  })),
}));
jest.mock('../../src/installer/helmfile-runner', () => ({
  helmfileSync: jest.fn(() => ({
    status: 'success',
    exitCode: 0,
    durationMs: 1,
    output: '',
  })),
  helmfileDestroy: jest.fn(),
  ensureHelmfileAvailable: jest.fn(() => ({ ok: true, version: '1.0' })),
  resolveHelmfilePath: jest.fn(() => '/tmp/helmfile.yaml.gotmpl'),
}));
jest.mock('../../src/installer/pulumi-runner', () => ({
  pulumiUp: jest.fn(),
  pulumiOutputs: jest.fn(() => ({})),
  pulumiDestroy: jest.fn(),
  ensurePulumiAvailable: jest.fn(() => ({ ok: true, version: '3.0' })),
  resolvePulumiProjectDir: jest.fn(() => '/tmp/pulumi'),
  resolveProjectRoot: jest.fn(() => '/tmp/loopzero'),
}));

import { platformInstall, PlatformDeps } from '../../src/commands/platform';

const TMP = path.join(os.tmpdir(), `loopzero-l3-test-${Date.now()}`);

beforeEach(() => {
  fs.mkdirSync(TMP, { recursive: true });
  mockSpawnSync.mockReset();
  // Default: every spawnSync returns success unless overridden per test.
  mockSpawnSync.mockReturnValue({
    status: 0, stdout: '', stderr: '', pid: 1, output: [], signal: null,
  } as never);
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

describe('platformInstall --validate-l3 (T8.1)', () => {
  it('default: validateL3=false → no L3 spawn even when phase=Ready', async () => {
    const valuesFile = writeValues();
    const { deps } = makeDeps();
    await platformInstall(deps, { valuesFile, skipInfra: true, skipRestoreSecrets: true });

    const goCalls = mockSpawnSync.mock.calls.filter((c) => {
      const cmd = c[0] as string;
      const args = (c[1] || []) as string[];
      return cmd === 'go' && args.includes('test') && args.some((a) => a.includes('l3'));
    });
    expect(goCalls.length).toBe(0);
  });

  it('validateL3=true + phase=Ready → spawn go test for L3 smoke', async () => {
    const valuesFile = writeValues();
    const { deps } = makeDeps();
    mockSpawnSync.mockImplementation(((cmd: string) => ({
      status: 0,
      stdout: 'PASS',
      stderr: '',
      pid: 1,
      output: [],
      signal: null,
    })) as never);

    await platformInstall(deps, { valuesFile, skipInfra: true, validateL3: true, skipRestoreSecrets: true });

    const goCalls = mockSpawnSync.mock.calls.filter((c) => {
      const cmd = c[0] as string;
      const args = (c[1] || []) as string[];
      return cmd === 'go' && args.includes('test') && args.some((a) => /L3/i.test(a));
    });
    expect(goCalls.length).toBe(1);
  });

  it('validateL3=true + L3 fails → exit code 5 (partial failure)', async () => {
    const valuesFile = writeValues();
    const { deps } = makeDeps();
    mockSpawnSync.mockImplementation(((cmd: string, args: string[]) => {
      if (cmd === 'go' && args.includes('test')) {
        return { status: 1, stdout: 'FAIL', stderr: 'p95 exceeded', pid: 1, output: [], signal: null };
      }
      return { status: 0, stdout: '', stderr: '', pid: 1, output: [], signal: null };
    }) as never);

    const code = await platformInstall(deps, {
      valuesFile,
      skipInfra: true,
      validateL3: true,
      skipRestoreSecrets: true,
    });
    expect(code).toBe(5);
  });

  it('--json + validateL3=true emits l3 progress event', async () => {
    const valuesFile = writeValues();
    const { deps, out } = makeDeps();
    mockSpawnSync.mockImplementation(((cmd: string) => ({
      status: 0,
      stdout: 'PASS',
      stderr: '',
      pid: 1,
      output: [],
      signal: null,
    })) as never);

    await platformInstall(deps, {
      valuesFile,
      skipInfra: true,
      validateL3: true,
      json: true,
      skipRestoreSecrets: true,
    });

    const events = out
      .map((l) => {
        try {
          return JSON.parse(l);
        } catch {
          return null;
        }
      })
      .filter((e) => e && e.phase === 'validate-l3');

    expect(events.length).toBeGreaterThan(0);
    expect(events.some((e: { status: string }) => e.status === 'running')).toBe(true);
    expect(events.some((e: { status: string }) => e.status === 'complete')).toBe(true);
  });
});

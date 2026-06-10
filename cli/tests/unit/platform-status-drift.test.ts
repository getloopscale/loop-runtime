// CHG-2026-04-30-INFRA-PARITY / T2.1 — drift detection regression test.
// Tests the pure function detectDrift() with a fake kubectl output via
// child_process mock, AND the platformStatus() exit code logic.

import { describe, it, expect, jest, afterEach, beforeEach } from '@jest/globals';
import { platformStatus, PlatformDeps } from '../../src/commands/platform';

// Mock child_process for deterministic kubectl output.
jest.mock('child_process', () => ({
  execSync: jest.fn(),
}));

// Mock installer/index getPlatformStatus so test doesn't need a real cluster.
jest.mock('../../src/installer', () => ({
  getPlatformStatus: jest.fn(),
}));

import { execSync } from 'child_process';
import { getPlatformStatus } from '../../src/installer';

const mockExec = execSync as jest.MockedFunction<typeof execSync>;
const mockStatus = getPlatformStatus as jest.MockedFunction<typeof getPlatformStatus>;

function makeDeps(): { deps: PlatformDeps; out: string[]; err: string[] } {
  const out: string[] = [];
  const err: string[] = [];
  return {
    deps: {
      writeTo: (m: string) => out.push(m),
      writeError: (m: string) => err.push(m),
    },
    out,
    err,
  };
}

afterEach(() => {
  jest.resetAllMocks();
});

describe('platformStatus drift detection (T2.1)', () => {
  beforeEach(() => {
    mockStatus.mockReturnValue({
      phase: 'Ready',
      version: '0.2.0',
      components: [],
      platformDefinitions: 10,
      endpoints: { api: 'https://api.test', dashboard: 'https://app.test' },
    });
  });

  it('exit 0 when phase=Ready and zero drift', async () => {
    mockExec.mockReturnValue(JSON.stringify({
      items: [
        { metadata: { name: 'a', namespace: 'argocd' }, status: { sync: { status: 'Synced' }, health: { status: 'Healthy' } } },
        { metadata: { name: 'b', namespace: 'argocd' }, status: { sync: { status: 'Synced' }, health: { status: 'Healthy' } } },
      ],
    }) as never);

    const { deps } = makeDeps();
    const code = await platformStatus(deps);
    expect(code).toBe(0);
  });

  it('exit 5 when phase=Ready but apps OutOfSync', async () => {
    mockExec.mockReturnValue(JSON.stringify({
      items: [
        { metadata: { name: 'a', namespace: 'argocd' }, status: { sync: { status: 'Synced' } } },
        { metadata: { name: 'b', namespace: 'argocd' }, status: { sync: { status: 'OutOfSync' }, health: { status: 'Healthy' } } },
      ],
    }) as never);

    const { deps, out } = makeDeps();
    const code = await platformStatus(deps);
    expect(code).toBe(5);
    expect(out.join('\n')).toMatch(/Drift detected/);
    expect(out.join('\n')).toMatch(/b.*OutOfSync/);
  });

  it('exit 5 when apps in Unknown sync state', async () => {
    mockExec.mockReturnValue(JSON.stringify({
      items: [
        { metadata: { name: 'a' }, status: { sync: { status: 'Unknown' } } },
      ],
    }) as never);

    const { deps } = makeDeps();
    const code = await platformStatus(deps);
    expect(code).toBe(5);
  });

  it('exit 1 when phase != Ready (not drift, install issue)', async () => {
    mockStatus.mockReturnValue({
      phase: 'Degraded',
      version: '0.2.0',
      components: [],
      platformDefinitions: 0,
      endpoints: {},
    });
    mockExec.mockReturnValue(JSON.stringify({ items: [] }) as never);

    const { deps } = makeDeps();
    const code = await platformStatus(deps);
    expect(code).toBe(1);
  });

  it('--json emits structured payload with sync.drift array', async () => {
    mockExec.mockReturnValue(JSON.stringify({
      items: [
        { metadata: { name: 'app1', namespace: 'argocd' }, status: { sync: { status: 'OutOfSync' }, health: { status: 'Healthy' } } },
      ],
    }) as never);

    const { deps, out } = makeDeps();
    const code = await platformStatus(deps, { json: true });
    expect(code).toBe(5);
    const parsed = JSON.parse(out[0]);
    expect(parsed.phase).toBe('Ready');
    expect(parsed.sync.drift).toHaveLength(1);
    expect(parsed.sync.drift[0].name).toBe('app1');
    expect(parsed.sync.drift[0].sync).toBe('OutOfSync');
  });

  it('handles missing sync engine gracefully (kubectl fails)', async () => {
    mockExec.mockImplementation(() => {
      throw new Error('connection refused');
    });

    const { deps } = makeDeps();
    const code = await platformStatus(deps);
    // Ready + no drift detected (no engine = no drift, kept Ready)
    expect(code).toBe(0);
  });
});

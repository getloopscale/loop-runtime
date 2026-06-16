// CHG-2026-04-30-INFRA-PARITY / T5.1 — Achados #34/#35/#36/#37 regression.
// Discovered live during T5.1 virgin install run 5: helmfile sync GREEN
// (23 charts deployed in 296.9s) but Step 6/6 validation reported phase=
// NotInstalled because:
//   #34: query used label `app.kubernetes.io/name=X` but charts apply `app: X`
//   #35: releaseExists('loopzero', ...) sentinel is for a release that never exists
//   #36: list contained `loopzero-tenant-controller` but real chart is `tenant-controller`
//   #37: list contained `loopzero-dashboard` but chart never existed
//
// This RED→GREEN test pins the LOOPZERO_COMPONENTS list against the filesystem
// (charts that actually exist) and pins the kubectl query template against
// the labels charts actually apply. Future drift in either direction breaks
// the test instead of failing silently in production validation.
//
// Pattern follows platform-status-drift.test.ts (mock child_process + helm).

import { describe, it, expect, jest, afterEach, beforeEach } from '@jest/globals';
import * as fs from 'fs';
import * as path from 'path';

jest.mock('child_process', () => ({
  execSync: jest.fn(),
}));

import { execSync } from 'child_process';
import { getPlatformStatus, LOOPZERO_COMPONENTS } from '../../src/installer/status';

const mockExec = execSync as jest.MockedFunction<typeof execSync>;

afterEach(() => {
  jest.resetAllMocks();
});

describe('getPlatformStatus — chart inventory pinning', () => {
  it('LOOPZERO_COMPONENTS only references charts that exist on disk', () => {
    // M2 (plan m2-extract-loopzero-cli): this is a MONOREPO-coupling check — it reaches
    // ../../../infra/helm, which exists only while the CLI is co-located in loopzero/.
    // After extraction into the standalone loopzero-cli repo there is no infra/helm, so
    // the on-disk assertion no-ops (the chart inventory is the engine repo's concern;
    // the polyrepo seam is the OpenAPI contract, not the chart tree). When co-located
    // (loopzero/ monorepo) the assertion runs as before.
    const helmDir = path.resolve(__dirname, '../../../infra/helm');
    if (!fs.existsSync(helmDir)) {
      return; // polyrepo (loopzero-cli standalone) — infra/helm not present
    }
    for (const name of LOOPZERO_COMPONENTS) {
      const chartPath = path.join(helmDir, name);
      expect(fs.existsSync(chartPath)).toBe(true);
    }
  });

  // Post-operator-split (CHG-2026-04-30-INFRA-PARITY T1.5): the legacy
  // loopzero-controller + tenant-controller charts were DELETED. Operators now
  // ship via charts/loopzero-{tenant,app,preview,platform}-operator (OCI). The
  // bootstrap-only charts in infra/helm/ are loopzero-api + loopzero-cloud.
  it('LOOPZERO_COMPONENTS contains the bootstrap charts in infra/helm/', () => {
    expect(LOOPZERO_COMPONENTS).toEqual([
      'loopzero-api',
      'loopzero-cloud',
    ]);
  });
});

describe('getPlatformStatus — kubectl query template (Achado #34)', () => {
  beforeEach(() => {
    mockExec.mockReturnValue('1/1' as never);
  });

  it('query uses label `app=X` not `app.kubernetes.io/name=X`', () => {
    getPlatformStatus();
    const calls = mockExec.mock.calls.map((c) => c[0] as string);
    const deployCalls = calls.filter((c) => c.includes('kubectl get deploy'));
    expect(deployCalls.length).toBeGreaterThan(0);
    for (const cmd of deployCalls) {
      expect(cmd).toMatch(/-l app=/);
      expect(cmd).not.toMatch(/-l app\.kubernetes\.io\/name=/);
    }
  });

  it('queries one selector per LoopZero component', () => {
    getPlatformStatus();
    const calls = mockExec.mock.calls.map((c) => c[0] as string);
    for (const name of LOOPZERO_COMPONENTS) {
      expect(calls.some((c) => c.includes(`-l app=${name}`))).toBe(true);
    }
  });
});

describe('getPlatformStatus — phase calculation (Achado #35)', () => {
  it('phase=Ready when all components return Running pods (no fake "loopzero" sentinel)', () => {
    mockExec.mockImplementation(((cmd: string) => {
      if (cmd.includes('kubectl get deploy')) return '1/1';
      if (cmd.includes('kubectl get crd')) return '10';
      return '';
    }) as never);

    const status = getPlatformStatus();
    expect(status.phase).toBe('Ready');
    expect(status.components.every((c) => c.status === 'Running')).toBe(true);
  });

  it('phase=Degraded when at least one component is Pending', () => {
    let i = 0;
    mockExec.mockImplementation(((cmd: string) => {
      if (cmd.includes('kubectl get deploy')) {
        i++;
        return i === 1 ? '0/1' : '1/1';
      }
      if (cmd.includes('kubectl get crd')) return '10';
      return '';
    }) as never);

    const status = getPlatformStatus();
    expect(status.phase).toBe('Degraded');
  });

  it('phase=NotInstalled when zero components are reachable', () => {
    mockExec.mockImplementation((() => {
      throw new Error('connection refused');
    }) as never);

    const status = getPlatformStatus();
    expect(status.phase).toBe('NotInstalled');
    expect(status.components.every((c) => c.status === 'NotInstalled')).toBe(true);
  });
});

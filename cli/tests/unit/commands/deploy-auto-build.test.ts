// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T3.1 (loopzero-cli-vercel-like-ux) — auto-build branch tests.

import { DeployCommand } from '../../../src/commands/deploy';
import type { DeployDeps } from '../../../src/commands/deploy-types';

interface SSEEvent { type: string; data: Record<string, unknown> }

function createDeps(overrides: Partial<DeployDeps> = {}): DeployDeps {
  return {
    getToken: jest.fn().mockReturnValue('t'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/p/loopzero.yaml',
      content: {
        project: 's', apps: { api: { path: 'a', framework: 'express', type: 'server', port: 3000 } },
      },
    }),
    findServicesJson: jest.fn().mockReturnValue({ path: '/p/.loopzero/services.json' }),
    runLoopZerokitBuild: jest.fn().mockResolvedValue(undefined),
    hasGitRepo: jest.fn().mockReturnValue(false),
    hasUncommittedChanges: jest.fn().mockReturnValue(false),
    hasLockfile: jest.fn().mockReturnValue(true),
    getSourceIdentifier: jest.fn().mockReturnValue('s1'),
    createSourceBundle: jest.fn().mockResolvedValue('/tmp/b'),
    uploadSource: jest.fn().mockResolvedValue({ commit_sha: 's', apps: ['api'] }),
    submitBuild: jest.fn().mockResolvedValue({ build_id: 'b1', stream_url: '' }),
    connectSSE: jest.fn().mockImplementation((_b: string, onEvent: (e: SSEEvent) => void) => {
      onEvent({
        type: 'deploy.completed',
        data: {
          deploy_id: 'd', status: 'success', duration_seconds: 1,
          apps: [{ name: 'api', status: 'deployed', url: 'https://x', build_duration_seconds: 1 }],
        },
      });
      return { close: jest.fn() };
    }),
    getSecrets: jest.fn().mockResolvedValue([]),
    cleanupFile: jest.fn(),
    generateIdempotencyKey: jest.fn().mockReturnValue('k'),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    isCI: false,
    ...overrides,
  };
}

describe('Plan v1.1 T3.1 — auto-build branch', () => {
  it('auto-builds when services.json is missing on first discovery', async () => {
    const runLoopZerokitBuild = jest.fn().mockResolvedValue(undefined);
    let firstCall = true;
    const findServicesJson = jest.fn().mockImplementation(() => {
      if (firstCall) { firstCall = false; return null; }
      return { path: '/p/.loopzero/services.json' };
    });
    const deps = createDeps({ findServicesJson, runLoopZerokitBuild });
    const cmd = new DeployCommand(deps);
    await cmd.execute({ env: 'production', json: true });
    expect(runLoopZerokitBuild).toHaveBeenCalledTimes(1);
    expect(findServicesJson).toHaveBeenCalledTimes(2);
  });

  it('skips auto-build when services.json already exists', async () => {
    const runLoopZerokitBuild = jest.fn().mockResolvedValue(undefined);
    const deps = createDeps({ runLoopZerokitBuild });
    const cmd = new DeployCommand(deps);
    await cmd.execute({ env: 'production', json: true });
    expect(runLoopZerokitBuild).not.toHaveBeenCalled();
  });

  it('skips auto-build when --skip-build is set, even if missing', async () => {
    const runLoopZerokitBuild = jest.fn();
    const writeErrors: string[] = [];
    const deps = createDeps({
      runLoopZerokitBuild,
      findServicesJson: jest.fn().mockReturnValue(null),
      writeError: jest.fn((msg: string) => { writeErrors.push(msg); }),
    });
    const cmd = new DeployCommand(deps);
    const exit = await cmd.execute({ env: 'production', json: false, skipBuild: true });
    expect(runLoopZerokitBuild).not.toHaveBeenCalled();
    expect(exit).toBe(1);
    expect(writeErrors.join('\n')).toContain('--skip-build/--prebuilt was passed');
  });

  it('throws LOOPZEROKIT_BUILD_EMPTY when build exit 0 but services.json still missing (EC-10)', async () => {
    const runLoopZerokitBuild = jest.fn().mockResolvedValue(undefined);
    const findServicesJson = jest.fn().mockReturnValue(null); // null forever
    const writeErrors: string[] = [];
    const deps = createDeps({
      runLoopZerokitBuild,
      findServicesJson,
      writeError: jest.fn((msg: string) => { writeErrors.push(msg); }),
    });
    const cmd = new DeployCommand(deps);
    const exit = await cmd.execute({ env: 'production', json: false });
    expect(exit).toBe(1);
    expect(writeErrors.join('\n')).toContain('loopzerokit doctor');
  });

  it('propagates LoopZerokitBuildError cause chain when build fails', async () => {
    const cause = new Error('vite blew up');
    const runLoopZerokitBuild = jest.fn().mockRejectedValue(
      Object.assign(new Error('loopzerokit build failed with exit code 7'), {
        code: 'LOOPZEROKIT_BUILD_FAILED',
        exitCode: 1,
        statusCode: 0,
        cause,
      }),
    );
    const deps = createDeps({
      runLoopZerokitBuild,
      findServicesJson: jest.fn().mockReturnValue(null),
    });
    const cmd = new DeployCommand(deps);
    const exit = await cmd.execute({ env: 'production', json: true });
    // handleError surfaces the typed ApiError exit code (non-zero).
    expect(exit).not.toBe(0);
    expect(runLoopZerokitBuild).toHaveBeenCalled();
  });

  it('runtime-asserts findServicesJson is wired (EC-9 internal-dep guard)', async () => {
    const writeErrors: string[] = [];
    const deps = createDeps({
      findServicesJson: undefined,
      writeError: jest.fn((msg: string) => { writeErrors.push(msg); }),
    });
    const cmd = new DeployCommand(deps);
    const exit = await cmd.execute({ env: 'production', json: false });
    expect(exit).toBe(2);
    expect(writeErrors.join('\n')).toContain('Internal');
  });

  it('forwards --services-json override even in auto-build branch', async () => {
    const runLoopZerokitBuild = jest.fn().mockResolvedValue(undefined);
    const findServicesJson = jest.fn()
      .mockImplementationOnce(() => null)
      .mockImplementationOnce(() => ({ path: '/custom/services.json' }));
    const deps = createDeps({ findServicesJson, runLoopZerokitBuild });
    const cmd = new DeployCommand(deps);
    await cmd.execute({ env: 'production', json: true, servicesJson: '/custom/services.json' });
    expect(findServicesJson).toHaveBeenNthCalledWith(1, '/custom/services.json');
    expect(findServicesJson).toHaveBeenNthCalledWith(2, '/custom/services.json');
  });
});

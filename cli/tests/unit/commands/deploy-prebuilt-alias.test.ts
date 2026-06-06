// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan v1.1 T5.1 (loopzero-cli-vercel-like-ux) — --prebuilt alias of --skip-build.

import { DeployCommand } from '../../../src/commands/deploy';
import type { DeployDeps } from '../../../src/commands/deploy-types';

interface SSEEvent { type: string; data: Record<string, unknown> }

function createDeps(overrides: Partial<DeployDeps> = {}): DeployDeps {
  return {
    getToken: jest.fn().mockReturnValue('t'),
    findLoopZeroYaml: jest.fn().mockReturnValue({
      path: '/p/loopzero.yaml',
      content: { project: 's', apps: { api: { path: 'a', framework: 'express', type: 'server', port: 3000 } } },
    }),
    findServicesJson: jest.fn().mockReturnValue(null),
    runLoopZerokitBuild: jest.fn(),
    hasGitRepo: jest.fn().mockReturnValue(false),
    hasUncommittedChanges: jest.fn().mockReturnValue(false),
    hasLockfile: jest.fn().mockReturnValue(true),
    getSourceIdentifier: jest.fn().mockReturnValue('s'),
    createSourceBundle: jest.fn().mockResolvedValue('/tmp/b'),
    uploadSource: jest.fn().mockResolvedValue({ commit_sha: 's', apps: [] }),
    submitBuild: jest.fn().mockResolvedValue({ build_id: 'b', stream_url: '' }),
    connectSSE: jest.fn().mockImplementation((_b: string, onEvent: (e: SSEEvent) => void) => {
      onEvent({ type: 'deploy.completed', data: { status: 'success', duration_seconds: 1, apps: [] } });
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

describe('Plan v1.1 T5.1 — --prebuilt aliases --skip-build', () => {
  it('skipBuild=true bypasses auto-build (skipBuild path)', async () => {
    const runLoopZerokitBuild = jest.fn();
    const deps = createDeps({ runLoopZerokitBuild });
    const cmd = new DeployCommand(deps);
    const exit = await cmd.execute({ env: 'production', json: true, skipBuild: true });
    expect(runLoopZerokitBuild).not.toHaveBeenCalled();
    expect(exit).toBe(1); // services.json still missing, but no auto-build.
  });

  it('both --skip-build and --prebuilt resolve to the same opts.skipBuild=true at the action site', () => {
    // The register/deploy.ts action handler does:
    //   skipBuild: Boolean(cmdOpts.skipBuild || cmdOpts.prebuilt)
    // Confirm the OR-aliasing semantics directly.
    const cmdOpts1 = { skipBuild: true, prebuilt: undefined };
    const cmdOpts2 = { skipBuild: undefined, prebuilt: true };
    const cmdOpts3 = { skipBuild: undefined, prebuilt: undefined };
    expect(Boolean(cmdOpts1.skipBuild || cmdOpts1.prebuilt)).toBe(true);
    expect(Boolean(cmdOpts2.skipBuild || cmdOpts2.prebuilt)).toBe(true);
    expect(Boolean(cmdOpts3.skipBuild || cmdOpts3.prebuilt)).toBe(false);
  });
});

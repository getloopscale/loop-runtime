// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ConnectCommand, DisconnectCommand, parseGitHubRepo } from '../../../src/commands/connect';

// Partial<any> allows overrides to be jest mock fns whose `.mock` property
// is accessed in assertions — jest.MockedFunction<typeof X> generics get
// too verbose to thread through every dep, so the helper type stays loose.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createMockDeps(overrides: Partial<any> = {}) {
  return {
    getToken: jest.fn().mockReturnValue('test-token'),
    getGitRemote: jest.fn().mockReturnValue('git@github.com:getloopscale/my-app.git'),
    getProjectName: jest.fn().mockReturnValue('my-app'),
    connectRepo: jest.fn().mockResolvedValue({ ok: true, project: 'my-app', repo: 'getloopscale/my-app', branch: 'main', auto_deploy: true }),
    disconnectRepo: jest.fn().mockResolvedValue({ ok: true }),
    getConnection: jest.fn().mockResolvedValue(null),
    writeTo: jest.fn(),
    writeError: jest.fn(),
    ...overrides,
  };
}

describe('parseGitHubRepo', () => {
  it('parses SSH remote', () => {
    expect(parseGitHubRepo('git@github.com:getloopscale/my-app.git')).toBe('getloopscale/my-app');
  });
  it('parses HTTPS remote', () => {
    expect(parseGitHubRepo('https://github.com/getloopscale/my-app.git')).toBe('getloopscale/my-app');
  });
  it('parses HTTPS without .git', () => {
    expect(parseGitHubRepo('https://github.com/owner/repo')).toBe('owner/repo');
  });
  it('returns null for non-GitHub', () => {
    expect(parseGitHubRepo('git@gitlab.com:owner/repo.git')).toBeNull();
  });
});

describe('ConnectCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    const cmd = new ConnectCommand(deps);
    expect(await cmd.execute()).toBe(4);
  });

  it('returns exit 1 when no loopzero.yaml', async () => {
    const deps = createMockDeps({ getProjectName: jest.fn().mockReturnValue(null) });
    const cmd = new ConnectCommand(deps);
    expect(await cmd.execute()).toBe(1);
  });

  it('detects repo from git remote and connects', async () => {
    const deps = createMockDeps();
    const cmd = new ConnectCommand(deps);
    const exit = await cmd.execute();
    expect(exit).toBe(0);
    expect(deps.connectRepo).toHaveBeenCalledWith('my-app', { repo: 'getloopscale/my-app', installation_id: 0, branch: 'main' });
    expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('Connected getloopscale/my-app'));
  });

  it('uses --repo flag when provided', async () => {
    const deps = createMockDeps();
    const cmd = new ConnectCommand(deps);
    await cmd.execute({ repo: 'other/repo', branch: 'develop' });
    expect(deps.connectRepo).toHaveBeenCalledWith('my-app', { repo: 'other/repo', installation_id: 0, branch: 'develop' });
  });

  it('returns exit 1 when no remote and no --repo', async () => {
    const deps = createMockDeps({ getGitRemote: jest.fn().mockReturnValue(null) });
    const cmd = new ConnectCommand(deps);
    expect(await cmd.execute()).toBe(1);
  });

  it('outputs JSON when --json', async () => {
    const deps = createMockDeps();
    const cmd = new ConnectCommand(deps);
    await cmd.execute({ json: true });
    const output = deps.writeTo.mock.calls[0][0];
    const parsed = JSON.parse(output);
    expect(parsed.ok).toBe(true);
    expect(parsed.repo).toBe('getloopscale/my-app');
  });

  it('shows GitHub App install link', async () => {
    const deps = createMockDeps();
    const cmd = new ConnectCommand(deps);
    await cmd.execute();
    const all = deps.writeTo.mock.calls.map((c: string[]) => c[0]).join('\n');
    expect(all).toContain('github.com/apps/loopzero-platform');
  });

  // CHG-2026-04-23-AUDIT-25: network errors carry code=API_UNREACHABLE.
  it('network error produces API_UNREACHABLE with Check LOOPZERO_URL hint', async () => {
    const deps = createMockDeps({
      connectRepo: jest.fn().mockRejectedValue(new Error('fetch failed')),
    });
    const cmd = new ConnectCommand(deps);
    const exit = await cmd.execute({ json: true });
    expect(exit).toBe(3);
    const parsed = JSON.parse(deps.writeTo.mock.calls[0][0]);
    expect(parsed.code).toBe('API_UNREACHABLE');
    expect(parsed.error).toContain('Check LOOPZERO_URL');
  });
});

describe('DisconnectCommand', () => {
  it('returns exit 4 when not authenticated', async () => {
    const deps = createMockDeps({ getToken: jest.fn().mockReturnValue(null) });
    const cmd = new DisconnectCommand(deps);
    expect(await cmd.execute()).toBe(4);
  });

  it('disconnects successfully', async () => {
    const deps = createMockDeps();
    const cmd = new DisconnectCommand(deps);
    const exit = await cmd.execute();
    expect(exit).toBe(0);
    expect(deps.disconnectRepo).toHaveBeenCalledWith('my-app');
    expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('Disconnected'));
  });

  it('outputs JSON', async () => {
    const deps = createMockDeps();
    const cmd = new DisconnectCommand(deps);
    await cmd.execute({ json: true });
    const parsed = JSON.parse(deps.writeTo.mock.calls[0][0]);
    expect(parsed.ok).toBe(true);
  });

  // CHG-2026-04-23-AUDIT-25: network errors carry code=API_UNREACHABLE.
  it('network error produces API_UNREACHABLE with Check LOOPZERO_URL hint', async () => {
    const deps = createMockDeps({
      disconnectRepo: jest.fn().mockRejectedValue(new Error('ECONNREFUSED 127.0.0.1:8080')),
    });
    const cmd = new DisconnectCommand(deps);
    const exit = await cmd.execute({ json: true });
    expect(exit).toBe(3);
    const parsed = JSON.parse(deps.writeTo.mock.calls[0][0]);
    expect(parsed.code).toBe('API_UNREACHABLE');
    expect(parsed.error).toContain('Check LOOPZERO_URL');
  });
});

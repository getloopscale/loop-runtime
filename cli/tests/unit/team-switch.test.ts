// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { TeamSwitchCommand } from '../../src/commands/team-switch';
import { ApiError } from '../../src/api/client';

describe('TeamSwitchCommand (plan T2.5 D8 full)', () => {
  test('happy path: switches active team, exit 0', async () => {
    const writeTo = jest.fn();
    const cmd = new TeamSwitchCommand({
      getToken: () => 'tok',
      switchTeam: async (name) => ({ active_team: name, message: `switched to ${name}` }),
      writeTo,
      writeError: jest.fn(),
    });
    const exit = await cmd.execute('tn-b');
    expect(exit).toBe(0);
    expect(writeTo).toHaveBeenCalledWith(expect.stringContaining('tn-b'));
  });

  test('missing name → exit 1', async () => {
    const cmd = new TeamSwitchCommand({
      getToken: () => 'tok',
      switchTeam: async () => ({ active_team: '', message: '' }),
      writeTo: jest.fn(),
      writeError: jest.fn(),
    });
    const exit = await cmd.execute('');
    expect(exit).toBe(1);
  });

  test('no auth → exit 4', async () => {
    const cmd = new TeamSwitchCommand({
      getToken: () => null,
      switchTeam: async () => ({ active_team: '', message: '' }),
      writeTo: jest.fn(),
      writeError: jest.fn(),
    });
    const exit = await cmd.execute('tn-b');
    expect(exit).toBe(4);
  });

  test('403 NOT_MEMBER → exit 1 with server message', async () => {
    const writeError = jest.fn();
    const cmd = new TeamSwitchCommand({
      getToken: () => 'tok',
      switchTeam: async () => {
        throw new ApiError('You are not a member of team "tn-x".', 1, 403, 'NOT_MEMBER');
      },
      writeTo: jest.fn(),
      writeError,
    });
    const exit = await cmd.execute('tn-x');
    expect(exit).toBe(1);
    expect(writeError).toHaveBeenCalledWith(expect.stringContaining('not a member'));
  });

  test('501 NOT_AVAILABLE → exit 1 with server message', async () => {
    const cmd = new TeamSwitchCommand({
      getToken: () => 'tok',
      switchTeam: async () => {
        throw new ApiError('Team switching is not configured on this server.', 1, 501, 'NOT_AVAILABLE');
      },
      writeTo: jest.fn(),
      writeError: jest.fn(),
    });
    const exit = await cmd.execute('tn-b');
    expect(exit).toBe(1);
  });

  test('--json output: emits JSON success body', async () => {
    const writeTo = jest.fn();
    const cmd = new TeamSwitchCommand({
      getToken: () => 'tok',
      switchTeam: async (name) => ({ active_team: name, message: `switched to ${name}` }),
      writeTo,
      writeError: jest.fn(),
    });
    const exit = await cmd.execute('tn-b', { json: true });
    expect(exit).toBe(0);
    const payload = JSON.parse(writeTo.mock.calls[0][0]);
    expect(payload.active_team).toBe('tn-b');
  });
});

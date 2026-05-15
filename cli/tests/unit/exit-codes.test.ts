// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import {
  EXIT_CODES,
  releaseStateToExitCode,
  buildStateToExitCode,
  mapTerminalStateToExitCode,
  isValidExitCode,
} from '../../src/lib/exit-codes';

describe('EXIT_CODES (plan T1.2 D3 — Rule 4)', () => {
  test('exactly 6 codes per Rule 4: 0/1/2/3/4/5', () => {
    expect(EXIT_CODES.SUCCESS).toBe(0);
    expect(EXIT_CODES.USAGE).toBe(1);
    expect(EXIT_CODES.BUILD_FAILED).toBe(2);
    expect(EXIT_CODES.DEPLOY_FAILED).toBe(3);
    expect(EXIT_CODES.AUTH_ERROR).toBe(4);
    expect(EXIT_CODES.PARTIAL).toBe(5);
    expect(Object.keys(EXIT_CODES)).toHaveLength(6);
  });
});

describe('releaseStateToExitCode', () => {
  test.each([
    ['ACTIVE', EXIT_CODES.SUCCESS],
    ['SUPERSEDED', EXIT_CODES.SUCCESS],
    ['SYNC_FAILED', EXIT_CODES.DEPLOY_FAILED],
    ['GIT_MIRROR_FAILED', EXIT_CODES.DEPLOY_FAILED],
    ['ROLLBACK_FAILED', EXIT_CODES.DEPLOY_FAILED],
    ['CANCELLED', EXIT_CODES.DEPLOY_FAILED],
  ])('release state %s → exit %d', (state, expected) => {
    expect(releaseStateToExitCode(state)).toBe(expected);
  });

  test('unknown release state defaults to DEPLOY_FAILED', () => {
    expect(releaseStateToExitCode('WEIRD_STATE')).toBe(EXIT_CODES.DEPLOY_FAILED);
    expect(releaseStateToExitCode(undefined)).toBe(EXIT_CODES.DEPLOY_FAILED);
  });
});

describe('buildStateToExitCode', () => {
  test.each([
    ['SUCCEEDED', EXIT_CODES.SUCCESS],
    ['FAILED', EXIT_CODES.BUILD_FAILED],
    ['CANCELLED', EXIT_CODES.BUILD_FAILED],
  ])('build state %s → exit %d', (state, expected) => {
    expect(buildStateToExitCode(state)).toBe(expected);
  });

  test('unknown build state defaults to BUILD_FAILED', () => {
    expect(buildStateToExitCode('WEIRD')).toBe(EXIT_CODES.BUILD_FAILED);
    expect(buildStateToExitCode(undefined)).toBe(EXIT_CODES.BUILD_FAILED);
  });
});

describe('mapTerminalStateToExitCode (T1.2 plan)', () => {
  test('test_deploy_exits_0_on_success — build SUCCEEDED + release ACTIVE → 0', () => {
    expect(mapTerminalStateToExitCode('SUCCEEDED', 'ACTIVE')).toBe(EXIT_CODES.SUCCESS);
  });

  test('test_deploy_exits_2_on_build_failed — build FAILED → 2', () => {
    expect(mapTerminalStateToExitCode('FAILED', undefined)).toBe(EXIT_CODES.BUILD_FAILED);
  });

  test('test_deploy_exits_3_on_release_sync_failed — release SYNC_FAILED → 3', () => {
    expect(mapTerminalStateToExitCode('SUCCEEDED', 'SYNC_FAILED')).toBe(EXIT_CODES.DEPLOY_FAILED);
  });

  test('test_deploy_exits_3_on_release_git_mirror_failed — release GIT_MIRROR_FAILED → 3', () => {
    expect(mapTerminalStateToExitCode('SUCCEEDED', 'GIT_MIRROR_FAILED')).toBe(EXIT_CODES.DEPLOY_FAILED);
  });

  test('test_deploy_exits_3_on_release_rollback_failed — release ROLLBACK_FAILED → 3', () => {
    expect(mapTerminalStateToExitCode('SUCCEEDED', 'ROLLBACK_FAILED')).toBe(EXIT_CODES.DEPLOY_FAILED);
  });

  test('release state wins over build state (deploy is post-build)', () => {
    // Build SUCCEEDED but release SYNC_FAILED → deploy failure (release wins)
    expect(mapTerminalStateToExitCode('SUCCEEDED', 'SYNC_FAILED')).toBe(EXIT_CODES.DEPLOY_FAILED);
  });
});

describe('isValidExitCode (T1.2 plan — exhaustive)', () => {
  test('test_deploy_never_exits_with_code_outside_rule_4_table', () => {
    expect(isValidExitCode(0)).toBe(true);
    expect(isValidExitCode(1)).toBe(true);
    expect(isValidExitCode(2)).toBe(true);
    expect(isValidExitCode(3)).toBe(true);
    expect(isValidExitCode(4)).toBe(true);
    expect(isValidExitCode(5)).toBe(true);
    expect(isValidExitCode(6)).toBe(false);
    expect(isValidExitCode(-1)).toBe(false);
    expect(isValidExitCode(127)).toBe(false);
  });
});

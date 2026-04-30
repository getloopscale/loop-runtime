// CHG-2026-04-30-INFRA-PARITY / T3.2 — Secret backup detection regression.
// Tests the pure decideRestore() function (no I/O beyond filesystem stat,
// which is mocked).

import { describe, it, expect, beforeEach, afterEach, jest } from '@jest/globals';
import { decideRestore } from '../../src/installer/secret-backups';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

const TMPDIR = path.join(os.tmpdir(), 'loopzero-secret-backups-test');
const HOME_BACKUP_DIR = path.join(process.env.HOME || '/tmp', '.loopzero', 'backups');

beforeEach(() => {
  if (!fs.existsSync(TMPDIR)) fs.mkdirSync(TMPDIR, { recursive: true });
});

afterEach(() => {
  if (fs.existsSync(TMPDIR)) {
    for (const f of fs.readdirSync(TMPDIR)) {
      fs.unlinkSync(path.join(TMPDIR, f));
    }
  }
});

describe('decideRestore (T3.2)', () => {
  it('returns restore when --restore-secrets <file> points to existing file', () => {
    const file = path.join(TMPDIR, 'my-backup.yaml');
    fs.writeFileSync(file, 'kind: Secret\n');
    const r = decideRestore({ isCI: true, restoreFile: file });
    expect(r.action).toBe('restore');
    if (r.action === 'restore') expect(r.file).toBe(file);
  });

  it('returns fail when --restore-secrets points to missing file', () => {
    const r = decideRestore({ isCI: true, restoreFile: '/tmp/does-not-exist.yaml' });
    expect(r.action).toBe('fail');
    if (r.action === 'fail') expect(r.error).toMatch(/not found/);
  });

  it('returns skip when --no-restore-secrets is set (skip=true)', () => {
    const r = decideRestore({ isCI: true, skip: true });
    expect(r.action).toBe('skip');
    if (r.action === 'skip') expect(r.reason).toMatch(/explicit/);
  });

  it('CI mode + no flag + no backup → skip (nothing to restore)', () => {
    // Use a fake HOME with no backups dir.
    const orig = process.env.HOME;
    process.env.HOME = '/tmp/nonexistent-home-' + Date.now();
    try {
      const r = decideRestore({ isCI: true });
      expect(r.action).toBe('skip');
      if (r.action === 'skip') expect(r.reason).toMatch(/no backup/);
    } finally {
      process.env.HOME = orig;
    }
  });

  it('non-CI mode + no flag + no backup → skip', () => {
    const orig = process.env.HOME;
    process.env.HOME = '/tmp/nonexistent-home-' + Date.now();
    try {
      const r = decideRestore({ isCI: false });
      expect(r.action).toBe('skip');
    } finally {
      process.env.HOME = orig;
    }
  });

  // Note: testing `prompt` and `fail` in CI-with-backup paths requires
  // mocking the real ~/.loopzero/backups directory or stubbing findLatestBackup.
  // The integration is covered by the live test below.
  it('CI mode + no flag + backup exists → fail (must be explicit)', () => {
    // Drop a fake backup into the real ~/.loopzero/backups
    const stamp = Date.now();
    const real = HOME_BACKUP_DIR;
    fs.mkdirSync(real, { recursive: true, mode: 0o700 });
    const fakeBackup = path.join(real, `secrets-manual-test-${stamp}.yaml`);
    fs.writeFileSync(fakeBackup, 'kind: Secret\n', { mode: 0o600 });
    try {
      const r = decideRestore({ isCI: true });
      expect(r.action).toBe('fail');
      if (r.action === 'fail') expect(r.error).toMatch(/explicit decision/);
    } finally {
      if (fs.existsSync(fakeBackup)) fs.unlinkSync(fakeBackup);
    }
  });

  it('non-CI mode + no flag + backup exists → prompt', () => {
    const stamp = Date.now();
    const real = HOME_BACKUP_DIR;
    fs.mkdirSync(real, { recursive: true, mode: 0o700 });
    const fakeBackup = path.join(real, `secrets-manual-promptcase-${stamp}.yaml`);
    fs.writeFileSync(fakeBackup, 'kind: Secret\n', { mode: 0o600 });
    try {
      const r = decideRestore({ isCI: false });
      expect(r.action).toBe('prompt');
      if (r.action === 'prompt') expect(r.latest.path).toMatch(/secrets-/);
    } finally {
      if (fs.existsSync(fakeBackup)) fs.unlinkSync(fakeBackup);
    }
  });
});

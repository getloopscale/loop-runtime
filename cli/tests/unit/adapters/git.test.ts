// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { createGitBundle } from '../../../src/adapters/git';

describe('createGitBundle', () => {
  let tmpDir: string;
  const origCwd = process.cwd();

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-git-test-'));
    process.chdir(tmpDir);
    execSync('git init', { stdio: 'pipe' });
    execSync('git config user.email "test@test.com"', { stdio: 'pipe' });
    execSync('git config user.name "Test"', { stdio: 'pipe' });
  });

  afterEach(() => {
    process.chdir(origCwd);
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it('should create a bundle that includes full history (no prerequisites)', async () => {
    // Arrange: create a repo with 3 commits
    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'v1');
    execSync('git add -A && git commit -m "commit 1"', { stdio: 'pipe' });

    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'v2');
    execSync('git add -A && git commit -m "commit 2"', { stdio: 'pipe' });

    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'v3');
    execSync('git add -A && git commit -m "commit 3"', { stdio: 'pipe' });

    // Act
    const bundlePath = await createGitBundle();

    // Assert: bundle should have no prerequisites (self-contained)
    const verifyOutput = execSync(`git bundle verify ${bundlePath}`, {
      encoding: 'utf-8',
      stdio: ['pipe', 'pipe', 'pipe'],
    });

    // "The bundle requires these" should NOT appear — a self-contained bundle has no prerequisites
    expect(verifyOutput).not.toContain('requires');

    // Verify: bundle can be cloned into an empty repo (simulates fresh Gitea repo)
    const cloneDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-clone-test-'));
    try {
      execSync(`git clone ${bundlePath} ${cloneDir}/repo`, { stdio: 'pipe' });
      const log = execSync('git log --oneline', {
        cwd: path.join(cloneDir, 'repo'),
        encoding: 'utf-8',
      });
      // All 3 commits should be present
      expect(log.trim().split('\n')).toHaveLength(3);
    } finally {
      fs.rmSync(cloneDir, { recursive: true, force: true });
    }

    // Cleanup bundle
    fs.unlinkSync(bundlePath);
  });

  it('should work with a single-commit repo', async () => {
    // Arrange: repo with only 1 commit
    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'only');
    execSync('git add -A && git commit -m "initial"', { stdio: 'pipe' });

    // Act
    const bundlePath = await createGitBundle();

    // Assert: can be cloned from scratch
    const cloneDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-clone-test-'));
    try {
      execSync(`git clone ${bundlePath} ${cloneDir}/repo`, { stdio: 'pipe' });
      const content = fs.readFileSync(path.join(cloneDir, 'repo', 'file.txt'), 'utf-8');
      expect(content).toBe('only');
    } finally {
      fs.rmSync(cloneDir, { recursive: true, force: true });
    }

    fs.unlinkSync(bundlePath);
  });
});

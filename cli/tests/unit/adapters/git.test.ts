// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  createGitBundle,
  computeContentHash,
  createTarBundle,
  getSourceIdentifier,
  checkSensitiveFilesOnDisk,
} from '../../../src/adapters/git';

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

describe('computeContentHash', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-hash-test-'));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it('should produce deterministic hash for same files', () => {
    fs.writeFileSync(path.join(tmpDir, 'a.txt'), 'hello');
    fs.writeFileSync(path.join(tmpDir, 'b.txt'), 'world');

    const hash1 = computeContentHash(tmpDir);
    const hash2 = computeContentHash(tmpDir);
    expect(hash1).toBe(hash2);
    expect(hash1).toHaveLength(64); // SHA256 hex
  });

  it('should produce different hash for different content', () => {
    fs.writeFileSync(path.join(tmpDir, 'a.txt'), 'v1');
    const hash1 = computeContentHash(tmpDir);

    fs.writeFileSync(path.join(tmpDir, 'a.txt'), 'v2');
    const hash2 = computeContentHash(tmpDir);

    expect(hash1).not.toBe(hash2);
  });

  it('should exclude node_modules', () => {
    fs.writeFileSync(path.join(tmpDir, 'index.js'), 'code');
    const hashWithout = computeContentHash(tmpDir);

    fs.mkdirSync(path.join(tmpDir, 'node_modules', 'pkg'), { recursive: true });
    fs.writeFileSync(path.join(tmpDir, 'node_modules', 'pkg', 'x.js'), 'dep');
    const hashWith = computeContentHash(tmpDir);

    expect(hashWith).toBe(hashWithout);
  });

  it('should exclude .env files', () => {
    fs.writeFileSync(path.join(tmpDir, 'app.js'), 'code');
    const hashWithout = computeContentHash(tmpDir);

    fs.writeFileSync(path.join(tmpDir, '.env'), 'SECRET=abc');
    fs.writeFileSync(path.join(tmpDir, '.env.local'), 'LOCAL=xyz');
    const hashWith = computeContentHash(tmpDir);

    expect(hashWith).toBe(hashWithout);
  });

  it('should exclude loopzero.yaml from hash', () => {
    fs.writeFileSync(path.join(tmpDir, 'index.js'), 'code');
    const hashWithout = computeContentHash(tmpDir);

    fs.writeFileSync(path.join(tmpDir, 'loopzero.yaml'), 'project: test');
    const hashWith = computeContentHash(tmpDir);

    expect(hashWith).toBe(hashWithout);
  });

  it('should produce empty-dir hash for empty directory', () => {
    const hash = computeContentHash(tmpDir);
    expect(hash).toHaveLength(64);
    // SHA256 of empty input
    expect(hash).toBe('e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
  });

  it('should respect .loopzeroignore patterns', () => {
    fs.writeFileSync(path.join(tmpDir, 'app.js'), 'code');
    fs.writeFileSync(path.join(tmpDir, 'data.csv'), 'big data');
    const hashWithBoth = computeContentHash(tmpDir);

    fs.writeFileSync(path.join(tmpDir, '.loopzeroignore'), '*.csv');
    const hashIgnored = computeContentHash(tmpDir);

    expect(hashIgnored).not.toBe(hashWithBoth);
  });
});

describe('createTarBundle', () => {
  let tmpDir: string;
  const origCwd = process.cwd();

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-tar-test-'));
    process.chdir(tmpDir);
  });

  afterEach(() => {
    process.chdir(origCwd);
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it('should create a gzip tar file', async () => {
    fs.writeFileSync(path.join(tmpDir, 'index.js'), 'code');
    fs.writeFileSync(path.join(tmpDir, 'package.json'), '{}');

    const tarPath = await createTarBundle();
    expect(fs.existsSync(tarPath)).toBe(true);
    expect(tarPath).toMatch(/\.tar\.gz$/);

    // Verify it starts with gzip magic bytes
    const buf = Buffer.alloc(2);
    const fd = fs.openSync(tarPath, 'r');
    fs.readSync(fd, buf, 0, 2, 0);
    fs.closeSync(fd);
    expect(buf[0]).toBe(0x1f);
    expect(buf[1]).toBe(0x8b);

    fs.unlinkSync(tarPath);
  });

  it('should exclude node_modules from tar', async () => {
    fs.writeFileSync(path.join(tmpDir, 'app.js'), 'code');
    fs.mkdirSync(path.join(tmpDir, 'node_modules', 'pkg'), { recursive: true });
    fs.writeFileSync(path.join(tmpDir, 'node_modules', 'pkg', 'x.js'), 'dep');

    const tarPath = await createTarBundle();
    expect(fs.existsSync(tarPath)).toBe(true);

    // Extract to verify contents
    const extractDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-tar-extract-'));
    execSync(`tar xzf ${tarPath} -C ${extractDir}`, { stdio: 'pipe' });

    expect(fs.existsSync(path.join(extractDir, 'app.js'))).toBe(true);
    expect(fs.existsSync(path.join(extractDir, 'node_modules'))).toBe(false);

    fs.rmSync(extractDir, { recursive: true, force: true });
    fs.unlinkSync(tarPath);
  });

  it('should work with empty directory (no files)', async () => {
    const tarPath = await createTarBundle();
    expect(fs.existsSync(tarPath)).toBe(true);
    fs.unlinkSync(tarPath);
  });
});

describe('getSourceIdentifier', () => {
  let tmpDir: string;
  const origCwd = process.cwd();

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-srcid-test-'));
  });

  afterEach(() => {
    process.chdir(origCwd);
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it('should return git SHA when in a git repo', () => {
    process.chdir(tmpDir);
    execSync('git init', { stdio: 'pipe' });
    execSync('git config user.email "test@test.com"', { stdio: 'pipe' });
    execSync('git config user.name "Test"', { stdio: 'pipe' });
    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'content');
    execSync('git add -A && git commit -m "init"', { stdio: 'pipe' });

    const id = getSourceIdentifier();
    expect(id).toHaveLength(40); // SHA1 hex
    expect(id).not.toMatch(/^ch-/);
  });

  it('should return content hash when not in a git repo', () => {
    process.chdir(tmpDir);
    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'content');

    const id = getSourceIdentifier();
    expect(id).toMatch(/^ch-/);
    expect(id).toHaveLength(67); // "ch-" + 64 hex chars
  });

  it('should return content hash for git repo with no commits', () => {
    process.chdir(tmpDir);
    execSync('git init', { stdio: 'pipe' });
    fs.writeFileSync(path.join(tmpDir, 'file.txt'), 'content');

    const id = getSourceIdentifier();
    expect(id).toMatch(/^ch-/);
  });
});

describe('checkSensitiveFilesOnDisk', () => {
  let tmpDir: string;

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'loopzero-sensitive-test-'));
  });

  afterEach(() => {
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  it('should detect .key files', () => {
    fs.writeFileSync(path.join(tmpDir, 'app.js'), 'code');
    fs.writeFileSync(path.join(tmpDir, 'server.key'), 'private key');

    const result = checkSensitiveFilesOnDisk(tmpDir);
    expect(result).toContain('server.key');
  });

  it('should detect .pem files', () => {
    fs.writeFileSync(path.join(tmpDir, 'cert.pem'), 'certificate');

    const result = checkSensitiveFilesOnDisk(tmpDir);
    expect(result).toContain('cert.pem');
  });

  it('should detect credentials files', () => {
    fs.writeFileSync(path.join(tmpDir, 'credentials.json'), '{}');

    const result = checkSensitiveFilesOnDisk(tmpDir);
    expect(result).toContain('credentials.json');
  });

  it('should return empty for safe files', () => {
    fs.writeFileSync(path.join(tmpDir, 'index.js'), 'code');
    fs.writeFileSync(path.join(tmpDir, 'package.json'), '{}');

    const result = checkSensitiveFilesOnDisk(tmpDir);
    expect(result).toHaveLength(0);
  });

  it('should skip files in ignored directories', () => {
    fs.mkdirSync(path.join(tmpDir, 'node_modules'), { recursive: true });
    fs.writeFileSync(path.join(tmpDir, 'node_modules', 'secret.key'), 'dep key');

    const result = checkSensitiveFilesOnDisk(tmpDir);
    expect(result).toHaveLength(0);
  });
});

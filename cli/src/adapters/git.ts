// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { execSync, execFile } from 'child_process';
import { promisify } from 'util';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';
import * as tar from 'tar';
import ignore from 'ignore';

const execFileAsync = promisify(execFile);

/** Default patterns excluded from content hash and tar bundles. */
const DEFAULT_IGNORE_PATTERNS = [
  'node_modules/', '.git/', '.DS_Store', '*.log', 'dist/', 'build/',
  '.next/', '__pycache__/', '*.pyc', '.env', '.env.*', '.env.local',
  'coverage/', '.nyc_output/', '.cache/', 'tmp/', '.turbo/',
];

/** Additional patterns excluded from content hash only (not from tar bundles).
 * loopzero.yaml changes config, not source code — it shouldn't change the version hash. */
const HASH_ONLY_IGNORE_PATTERNS = [
  'loopzero.yaml',
];

export function hasGitRepo(): boolean {
  try {
    execSync('git rev-parse --git-dir', { encoding: 'utf-8', stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

export function hasUncommittedChanges(): boolean {
  if (!hasGitRepo()) return false;
  try {
    const status = execSync('git status --porcelain', { encoding: 'utf-8' }).trim();
    return status.length > 0;
  } catch {
    return false;
  }
}

export function getHeadCommit(): string {
  return execSync('git rev-parse HEAD', { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
}

/**
 * Returns a source identifier: git commit SHA (with .git) or content hash (without).
 * Content hashes are prefixed with "ch-" to distinguish from git SHAs.
 */
export function getSourceIdentifier(): string {
  if (hasGitRepo()) {
    try {
      return getHeadCommit();
    } catch {
      // git repo exists but no commits — fall through to content hash
    }
  }
  return 'ch-' + computeContentHash(process.cwd());
}

/**
 * Computes a deterministic SHA256 hash of all source files in a directory.
 * Excludes node_modules, .git, .env, and patterns from .gitignore / .loopzeroignore.
 * Same files with same contents always produce the same hash.
 */
export function computeContentHash(dir: string): string {
  const ig = loadIgnorePatterns(dir);
  ig.add(HASH_ONLY_IGNORE_PATTERNS);
  const files = collectFiles(dir, dir, ig);
  files.sort(); // deterministic order

  const hash = crypto.createHash('sha256');
  for (const relPath of files) {
    hash.update(relPath);
    hash.update(fs.readFileSync(path.join(dir, relPath)));
  }
  return hash.digest('hex');
}

/** Recursively collect files, respecting ignore patterns. */
function collectFiles(baseDir: string, currentDir: string, ig: ReturnType<typeof ignore>): string[] {
  const results: string[] = [];
  for (const entry of fs.readdirSync(currentDir, { withFileTypes: true })) {
    const fullPath = path.join(currentDir, entry.name);
    const relPath = path.relative(baseDir, fullPath);
    // Add trailing slash for directories so ignore patterns like "node_modules/" match
    const testPath = entry.isDirectory() ? relPath + '/' : relPath;
    if (ig.ignores(testPath)) continue;
    if (entry.isDirectory()) {
      results.push(...collectFiles(baseDir, fullPath, ig));
    } else if (entry.isFile()) {
      results.push(relPath);
    }
  }
  return results;
}

/** Load ignore patterns from .gitignore, .loopzeroignore, and defaults. */
function loadIgnorePatterns(dir: string): ReturnType<typeof ignore> {
  const ig = ignore().add(DEFAULT_IGNORE_PATTERNS);
  for (const ignoreFile of ['.gitignore', '.loopzeroignore']) {
    const filePath = path.join(dir, ignoreFile);
    if (fs.existsSync(filePath)) {
      ig.add(fs.readFileSync(filePath, 'utf-8'));
    }
  }
  return ig;
}

export function getGitRoot(): string | null {
  try {
    return execSync('git rev-parse --show-toplevel', { encoding: 'utf-8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
  } catch {
    return null;
  }
}

export function hasLockfile(): boolean {
  return fs.existsSync('package-lock.json') || fs.existsSync('yarn.lock') || fs.existsSync('pnpm-lock.yaml');
}

/**
 * Creates a source bundle: git bundle (with .git) or tar.gz (without).
 * Returns the path to the temporary bundle file.
 */
export async function createSourceBundle(): Promise<string> {
  if (hasGitRepo()) {
    try {
      return await createGitBundle();
    } catch {
      // git bundle failed (no commits?) — fall through to tar
    }
  }
  return await createTarBundle();
}

/** Creates a git bundle from HEAD. */
export async function createGitBundle(): Promise<string> {
  const tmpFile = path.join(os.tmpdir(), `loopzero-bundle-${Date.now()}.bundle`);
  await execFileAsync('git', ['bundle', 'create', tmpFile, 'HEAD']);
  return tmpFile;
}

/** Creates a tar.gz bundle of the current directory, respecting ignore patterns. */
export async function createTarBundle(): Promise<string> {
  const tmpFile = path.join(os.tmpdir(), `loopzero-bundle-${Date.now()}.tar.gz`);
  const dir = process.cwd();
  const ig = loadIgnorePatterns(dir);
  const files = collectFiles(dir, dir, ig);

  if (files.length === 0) {
    // tar library throws on empty file list — create an empty gzip file
    const { createGzip } = await import('zlib');
    const { pipeline } = await import('stream/promises');
    const { Readable } = await import('stream');
    const gzip = createGzip();
    const out = fs.createWriteStream(tmpFile);
    await pipeline(Readable.from(Buffer.alloc(0)), gzip, out);
  } else {
    await tar.create(
      { gzip: true, file: tmpFile, cwd: dir },
      files,
    );
  }
  return tmpFile;
}

/** Patterns that indicate potentially sensitive files. */
const SENSITIVE_PATTERNS = [
  /\.env$/,
  /\.env\..+$/,
  /\.key$/,
  /\.pem$/,
  /credentials/i,
  /secret/i,
  /\.p12$/,
  /\.pfx$/,
  /id_rsa/,
  /id_ed25519/,
  /\.keystore$/,
];

/**
 * Check for potentially sensitive files in the project.
 * Uses git ls-files when available, falls back to filesystem scan.
 * Returns file paths that match common sensitive patterns.
 */
export function checkSensitiveFiles(): string[] {
  if (hasGitRepo()) {
    try {
      const output = execSync(
        'git ls-files -- "*.env" "*.env.*" "*.key" "*.pem" "*credentials*" "*secret*"',
        { encoding: 'utf-8' },
      ).trim();
      if (!output) return [];
      return output.split('\n');
    } catch {
      return [];
    }
  }

  // Filesystem-based scan for non-git projects
  return checkSensitiveFilesOnDisk(process.cwd());
}

/**
 * Scans the directory for sensitive files using ignore patterns + sensitive patterns.
 * Respects .gitignore / .loopzeroignore so node_modules etc. are skipped.
 */
export function checkSensitiveFilesOnDisk(dir: string): string[] {
  const ig = loadIgnorePatterns(dir);
  const files = collectFiles(dir, dir, ig);
  return files.filter((f) => {
    const basename = path.basename(f);
    return SENSITIVE_PATTERNS.some((p) => p.test(basename));
  });
}

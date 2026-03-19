import { execSync, execFile } from 'child_process';
import { promisify } from 'util';
import * as fs from 'fs';
import * as path from 'path';
import * as os from 'os';

const execFileAsync = promisify(execFile);

export function hasGitRepo(): boolean {
  try {
    execSync('git rev-parse --git-dir', { encoding: 'utf-8', stdio: 'pipe' });
    return true;
  } catch {
    return false;
  }
}

export function hasUncommittedChanges(): boolean {
  try {
    const status = execSync('git status --porcelain', { encoding: 'utf-8' }).trim();
    return status.length > 0;
  } catch {
    return false;
  }
}

export function getHeadCommit(): string {
  return execSync('git rev-parse HEAD', { encoding: 'utf-8' }).trim();
}

export function getGitRoot(): string | null {
  try {
    return execSync('git rev-parse --show-toplevel', { encoding: 'utf-8' }).trim();
  } catch {
    return null;
  }
}

export function hasLockfile(): boolean {
  return fs.existsSync('package-lock.json') || fs.existsSync('yarn.lock') || fs.existsSync('pnpm-lock.yaml');
}

export async function createGitBundle(): Promise<string> {
  const tmpFile = path.join(os.tmpdir(), `loopzero-bundle-${Date.now()}.bundle`);
  // Bundle only HEAD's reachable history (not --all which includes every branch/tag).
  // The API's PushBundle does `git pull bundle HEAD`, so only the HEAD ref is needed.
  // Uses execFile (async) to avoid blocking the event loop on large repos.
  await execFileAsync('git', ['bundle', 'create', tmpFile, 'HEAD']);
  return tmpFile;
}

/**
 * Check for potentially sensitive files tracked in git.
 * Returns file paths that match common sensitive patterns.
 * Uses git pathspec filtering to avoid listing all tracked files.
 */
export function checkSensitiveFiles(): string[] {
  try {
    // Use git pathspec to filter server-side instead of listing all files
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

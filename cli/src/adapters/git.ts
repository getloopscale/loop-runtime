import { execSync } from 'child_process';
import * as path from 'path';
import * as os from 'os';

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
  const fs = require('fs');
  return fs.existsSync('package-lock.json') || fs.existsSync('yarn.lock') || fs.existsSync('pnpm-lock.yaml');
}

export async function createGitBundle(): Promise<string> {
  const tmpFile = path.join(os.tmpdir(), `loopzero-bundle-${Date.now()}.bundle`);
  // --all: include full commit history so the bundle is self-contained.
  // A shallow bundle (-1 HEAD) requires prerequisite commits that won't exist
  // in the remote source repo on first deploy, causing every deploy to fail.
  execSync(`git bundle create ${tmpFile} --all HEAD`, { stdio: 'pipe' });
  return tmpFile;
}

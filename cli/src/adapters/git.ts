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

export async function createGitBundle(): Promise<string> {
  const tmpFile = path.join(os.tmpdir(), `loopzero-bundle-${Date.now()}.bundle`);
  execSync(`git bundle create ${tmpFile} -1 HEAD`, { stdio: 'pipe' });
  return tmpFile;
}

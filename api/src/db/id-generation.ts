import { randomBytes } from 'crypto';

export function generateBuildId(): string {
  return `build-${randomBytes(4).toString('hex')}`;
}

export function generateReleaseId(): string {
  return `rel-${randomBytes(4).toString('hex')}`;
}

import { randomBytes } from 'crypto';

export function generateId(prefix: string): string {
  const hex = randomBytes(4).toString('hex');
  return `${prefix}-${hex}`;
}

import * as fs from 'fs';

export function cleanupFile(filePath: string): void {
  try {
    fs.unlinkSync(filePath);
  } catch {
    /* ignore — file may already be gone */
  }
}

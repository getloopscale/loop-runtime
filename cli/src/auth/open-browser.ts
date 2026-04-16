// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { exec } from 'child_process';
import { platform } from 'os';

/**
 * Opens a URL in the user's default browser.
 * Cross-platform: macOS (open), Linux (xdg-open), Windows (start).
 * Never throws — returns silently on failure (headless/SSH environments).
 */
export async function openBrowser(url: string): Promise<void> {
  const cmd = platform() === 'darwin'
    ? `open "${url}"`
    : platform() === 'win32'
      ? `start "" "${url}"`
      : `xdg-open "${url}"`;

  return new Promise((resolve) => {
    exec(cmd, (_err) => {
      // Silent failure — in SSH/headless, the URL is displayed in the terminal
      resolve();
    });
  });
}

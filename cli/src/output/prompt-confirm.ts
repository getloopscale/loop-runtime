// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Interactive yes/no confirmation prompt.
 *
 * CHG-2026-04-23-AUDIT-9: the inline `readline.question` pattern used in
 * `register/preview.ts`, `register/project.ts`, and `register/auth.ts` hangs
 * indefinitely when stdin is a pipe or closed (CI without CI env var, scripts,
 * `< /dev/null`). Fix:
 *   - detect non-TTY stdin and return `false` (implicit "no")
 *   - return `false` on EOF without an answer
 *
 * Callers interpret `false` as "user declined" and abort the operation — same
 * behaviour as if the user typed "n".
 */
export async function promptConfirm(message: string): Promise<boolean> {
  // Non-TTY stdin → no human can answer. Fail safe: decline.
  if (!process.stdin.isTTY) {
    return false;
  }

  const readline = await import('readline');
  const rl = readline.createInterface({ input: process.stdin, output: process.stdout });

  return new Promise<boolean>((resolve) => {
    let settled = false;
    const settle = (answer: boolean) => {
      if (settled) return;
      settled = true;
      rl.close();
      resolve(answer);
    };

    // EOF (Ctrl-D or closed stdin) → decline.
    rl.on('close', () => settle(false));

    rl.question(message, (answer) => {
      const a = answer.toLowerCase().trim();
      settle(a === 'y' || a === 'yes');
    });
  });
}

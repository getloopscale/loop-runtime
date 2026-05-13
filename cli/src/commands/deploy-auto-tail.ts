// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Beta Logs Gate Criterion 4 (UX) — deploy-failed auto-tail.
//
// When a deploy fails (build.failed / deploy.sync_failed /
// deploy.delivery_failed SSE event), the CLI traditionally exits
// with an error message + the `loopzero logs --build <id>` hint. The
// operator then types that command and sees the logs.
//
// This module provides a best-effort short-circuit: if the deploy
// fails AND we can fetch the last ~20 log lines in under 5 seconds,
// embed them in the error output so the operator sees the failure
// context immediately. The hint is preserved as a fallback for
// when the auto-tail fails OR returns too few lines.
//
// Design constraints:
//   - NON-blocking: the auto-tail MUST NOT delay the exit code more
//     than `TAIL_TIMEOUT_MS` (5s default).
//   - Silent on failure: any error during the tail (network, auth,
//     Loki down) is swallowed; the operator still gets the original
//     error + the manual hint.
//   - JSON mode preserved: when --json is set, the tail lines are
//     embedded as a `recent_logs` array in the JSON payload rather
//     than printed inline.
//   - Best-effort: tail returns up to TAIL_LINES (default 20). Empty
//     result is acceptable; the helper writes nothing in that case.

import type { LogEntry, LogsApiResponse } from './logs';

/** Limit how long auto-tail can block before falling back. */
export const AUTO_TAIL_TIMEOUT_MS = 5000;

/** Number of trailing lines to fetch. */
export const AUTO_TAIL_LINES = 20;

/**
 * Optional dependency injected by the CLI register layer. Same shape
 * as LogsDeps.getLogs but with a leaner signature — the auto-tail
 * always uses tail=20 and no step filter.
 *
 * Returns a LogsApiResponse OR null if the fetch failed in any way
 * (the implementer swallows errors and returns null rather than
 * throwing — auto-tail is best-effort by design).
 */
export type AutoTailFetcher = (buildID: string) => Promise<LogsApiResponse | null>;

/**
 * Format the auto-tail output for human-readable display. The result
 * is the message a `writeTo` callback should emit when auto-tail
 * succeeds — multiple lines separated by `\n`.
 *
 * Returns empty string when entries is empty (caller should NOT emit
 * anything — the original error message + hint is the only output).
 */
export function formatAutoTail(entries: LogEntry[]): string {
  if (!entries || entries.length === 0) {
    return '';
  }
  const lines: string[] = [];
  lines.push('');
  lines.push('--- Last log lines (recent deploy context) ---');
  for (const entry of entries) {
    const prefix = entry.step ? `[${entry.step}] ` : '';
    // Trim trailing newlines so the output is tight; the lines array
    // join below adds exactly one newline between entries.
    const content = entry.content.replace(/\n+$/, '');
    lines.push(`${prefix}${content}`);
  }
  lines.push('--- End log tail ---');
  return lines.join('\n');
}

/**
 * Fetch the recent build logs with a hard timeout. Returns null on
 * timeout OR fetcher rejection — the caller treats null as "no
 * auto-tail available; fall back to the manual hint".
 *
 * The withTimeout pattern matches the existing CLI usage in
 * register/deploy.ts so testers can mock the fetcher and still
 * exercise the timeout branch.
 */
export async function fetchAutoTail(
  fetcher: AutoTailFetcher,
  buildID: string,
  timeoutMs: number = AUTO_TAIL_TIMEOUT_MS,
): Promise<LogEntry[]> {
  let timeoutHandle: ReturnType<typeof setTimeout> | null = null;
  const timeoutPromise = new Promise<null>((resolve) => {
    timeoutHandle = setTimeout(() => resolve(null), timeoutMs);
  });

  try {
    const result = await Promise.race([
      fetcher(buildID).catch(() => null),
      timeoutPromise,
    ]);
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
    }
    if (!result || !result.entries) {
      return [];
    }
    // Keep only the last N entries (the API may have returned more
    // depending on the tail param; we re-cap defensively).
    return result.entries.slice(-AUTO_TAIL_LINES);
  } catch {
    if (timeoutHandle) {
      clearTimeout(timeoutHandle);
    }
    return [];
  }
}

/**
 * Convenience helper: combines fetch + format into a single call.
 * The CLI uses this in the deploy-failed branches to embed the tail
 * into the error output without spreading the fetch/format/timeout
 * logic across multiple call sites.
 */
export async function maybeRenderAutoTail(
  fetcher: AutoTailFetcher | undefined,
  buildID: string,
  writeTo: (msg: string) => void,
  options?: { timeoutMs?: number; jsonMode?: boolean },
): Promise<LogEntry[]> {
  if (!fetcher) {
    return [];
  }
  const timeoutMs = options?.timeoutMs ?? AUTO_TAIL_TIMEOUT_MS;
  const entries = await fetchAutoTail(fetcher, buildID, timeoutMs);
  if (entries.length === 0) {
    return [];
  }
  // In JSON mode, the caller is responsible for embedding the array
  // into the JSON payload. We don't writeTo the human-readable
  // formatted output because that would corrupt the JSON.
  if (options?.jsonMode) {
    return entries;
  }
  const formatted = formatAutoTail(entries);
  if (formatted) {
    writeTo(formatted);
  }
  return entries;
}

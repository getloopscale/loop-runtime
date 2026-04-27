// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * Helpers for building user-facing error messages.
 *
 * CHG-2026-04-23-AUDIT-8: 28 call sites across 10 commands concatenated
 * `` `<prefix>: ${err.message}. Try again.` `` which produced double punctuation
 * like `"Upgrade your plan to continue.. Try again."` whenever the API message
 * already ended with a period. The helper strips trailing period before
 * concatenating so output is always single-period.
 */

/**
 * formatErrorMessage builds a clean CLI error line from a prefix and an
 * underlying error message, optionally followed by a short hint.
 *
 * Guarantees:
 *   - exactly one period between the prefix and the err message
 *   - no trailing double-period (strips trailing `.` from err message)
 *   - optional hint is appended with a single space + single period
 *
 * Examples:
 *   formatErrorMessage("Failed to create project", "Plan limit reached.")
 *     → "Failed to create project: Plan limit reached."
 *
 *   formatErrorMessage("Failed to create project", "Plan limit reached.", "Try again")
 *     → "Failed to create project: Plan limit reached. Try again."
 *
 *   formatErrorMessage("Failed to create project", "Network timeout", "Try again")
 *     → "Failed to create project: Network timeout. Try again."
 */
export function formatErrorMessage(
  prefix: string,
  errMessage: string,
  hint?: string,
): string {
  // Strip trailing periods (and surrounding whitespace) from err message.
  const trimmed = errMessage.replace(/\s*\.+\s*$/, '').trim();
  let result = `${prefix}: ${trimmed}.`;
  if (hint) {
    const hintClean = hint.replace(/\s*\.+\s*$/, '').trim();
    if (hintClean) {
      result += ` ${hintClean}.`;
    }
  }
  return result;
}

/**
 * CHG-2026-04-23-AUDIT-24: unified detection of "API unreachable".
 *
 * Prior behavior: each command's handleError generated its own ad-hoc
 * message for network failures ("Failed to list: fetch failed. Try again."),
 * and every branch used `code: "UNEXPECTED_ERROR"` regardless of whether
 * the cause was a wrong LOOPZERO_URL or a real 5xx on the server.
 *
 * Operators lost a diagnostic signal: scripts couldn't distinguish a
 * temporary retry situation from a permanent config error, and the hint
 * didn't point users at the most likely fix (Check LOOPZERO_URL).
 *
 * This helper centralizes the heuristic. Pair with code=`API_UNREACHABLE`
 * at the call site (the helper is classifier-only; exit code + code label
 * stay with each command to preserve their contracts).
 *
 * Returns isNetwork=true only for plain Error instances carrying
 * classic Node/undici network markers. ApiError (HTTP response) is
 * intentionally NOT classified here — the server responded, just
 * unhappily.
 */
export interface NetworkErrorClassification {
  isNetwork: boolean;
  /**
   * User-facing message. Only populated when isNetwork=true. Always ends
   * with ". Check LOOPZERO_URL." so scripts can rely on the suffix.
   */
  message: string;
}

const NETWORK_MARKERS = [
  'fetch failed',
  'ECONNREFUSED',
  'ECONNRESET',
  'ENOTFOUND',
  'ETIMEDOUT',
  'EAI_AGAIN',
  'network timeout',
] as const;

export function classifyNetworkError(err: unknown): NetworkErrorClassification {
  if (!(err instanceof Error)) {
    return { isNetwork: false, message: '' };
  }
  const raw = err.message ?? '';
  const hit = NETWORK_MARKERS.some((m) => raw.includes(m));
  if (!hit) {
    return { isNetwork: false, message: '' };
  }
  // Reuse formatErrorMessage to keep punctuation policy in one place.
  return {
    isNetwork: true,
    message: formatErrorMessage('Cannot reach API', raw, 'Check LOOPZERO_URL'),
  };
}

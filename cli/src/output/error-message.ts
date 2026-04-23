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

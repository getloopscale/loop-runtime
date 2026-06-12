/**
 * format-ts.ts — J-19 T2.5 (R9 blueprint SHOULD).
 *
 * Converts an ISO 8601 UTC timestamp to the browser's local time-of-day
 * display (HH:mm:ss, 24h) and a separate full-ISO tooltip string.
 *
 * EC-5 time-zone semantics (per impl edge cases review): Heroku's
 * `docs/logs.md:24` documents UTC display gotchas — operators in different
 * regions see UTC literally and must convert mentally. LoopZero's pre-T2.5
 * behavior was the same (`line.ts.slice(11, 19)`). Post-T2.5 the viewer
 * shows browser-local time-of-day with the full UTC ISO in a `title`
 * tooltip — operator sees BOTH without leaving the page.
 *
 * Edge case: invalid ISO input falls back to the raw string (no crash).
 */

export interface FormattedTimestamp {
  /** Browser-local time-of-day for display (HH:mm:ss in 24h). */
  local: string;
  /** Full original ISO 8601 for the tooltip / `<time dateTime>` attribute. */
  iso: string;
}

export function formatLocalTime(iso: string): FormattedTimestamp {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) {
    return { local: iso, iso };
  }
  // toLocaleTimeString respects process.env.TZ in node (vitest) AND
  // navigator timezone in browser. 24h forced by hour12:false.
  const local = d.toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  return { local, iso };
}

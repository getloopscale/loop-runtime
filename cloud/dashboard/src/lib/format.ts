// Display formatters — single source of truth for dates + numbers across
// the dashboard. Eliminates the drift documented in plan v1.1 P-1 + P-2:
// before this module, pages used a mix of `new Date(iso).toLocaleDateString()`
// (browser-locale dependent — DD/MM/YYYY in PT-BR, MM/DD/YYYY in en-US),
// raw `iso` strings, and inline number rendering without separators.
//
// Decisions (ADR-D1 + ADR-D4 of plan v1.1):
//   - Locale: en-US hard-coded (dashboard is English-only in pre-release;
//     defer i18n to v1.x)
//   - Bytes: SI 1000-base (KB/MB/GB matches DO/AWS/GCP billing surfaces;
//     not IEC 1024-base KiB/MiB/GiB)
//   - Null safety: return `—` (em-dash) for null/undefined/empty/NaN/Infinity
//
// Bundle: ZERO impact in prod — all helpers use native Intl APIs that Vite-
// targeted browsers ship by default.

const EMDASH = '—';

/**
 * Format an ISO date string for display per intent.
 *
 * @example
 *   formatDate('2026-05-25T06:42:00Z', 'date')     // "May 25, 2026"
 *   formatDate('2026-05-25T06:42:00Z', 'datetime') // "May 25, 2026, 6:42 AM"
 *   formatDate('2026-05-25', 'long')               // "Monday, May 25, 2026"
 *   formatDate(null, 'date')                       // "—"
 *   formatDate('', 'date')                         // "—" (EC-2 guard)
 *   formatDate('invalid', 'date')                  // "—" (Invalid Date guard)
 */
export function formatDate(
  iso: string | undefined | null,
  intent: 'date' | 'datetime' | 'relative' | 'long',
): string {
  // EC-2: empty string + null + undefined all collapse to em-dash via !iso
  if (!iso) return EMDASH;
  const d = new Date(iso);
  // Invalid Date guard — Number.isNaN of date.getTime() is the canonical check
  if (Number.isNaN(d.getTime())) return EMDASH;

  switch (intent) {
    case 'date':
      return d.toLocaleDateString('en-US', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
      });
    case 'datetime':
      return d.toLocaleString('en-US', {
        dateStyle: 'medium',
        timeStyle: 'short',
      });
    case 'relative':
      return relativeTime(d);
    case 'long':
      return d.toLocaleDateString('en-US', {
        weekday: 'long',
        year: 'numeric',
        month: 'long',
        day: 'numeric',
      });
  }
}

/**
 * Relative time formatter ("2h ago", "yesterday", "3 days ago", "in 5 min").
 * Uses Intl.RelativeTimeFormat for proper grammar. Buckets: minute, hour,
 * day, week, month, year.
 */
function relativeTime(d: Date): string {
  const diffMs = d.getTime() - Date.now();
  const absSec = Math.abs(diffMs) / 1000;
  const rtf = new Intl.RelativeTimeFormat('en-US', { numeric: 'auto' });

  if (absSec < 60) return rtf.format(Math.round(diffMs / 1000), 'second');
  if (absSec < 3600) return rtf.format(Math.round(diffMs / 60_000), 'minute');
  if (absSec < 86_400) return rtf.format(Math.round(diffMs / 3_600_000), 'hour');
  if (absSec < 604_800) return rtf.format(Math.round(diffMs / 86_400_000), 'day');
  if (absSec < 2_592_000) return rtf.format(Math.round(diffMs / 604_800_000), 'week');
  if (absSec < 31_536_000) return rtf.format(Math.round(diffMs / 2_592_000_000), 'month');
  return rtf.format(Math.round(diffMs / 31_536_000_000), 'year');
}

/**
 * Format a number for display per intent.
 *
 * @example
 *   formatNumber(1_240_000, 'compact')   // "1.24M"
 *   formatNumber(1_240_000, 'grouped')   // "1,240,000"
 *   formatNumber(1_500_000, 'bytes')     // "1.5 MB" (SI 1000-base per ADR-D4)
 *   formatNumber(0.423, 'percent')       // "42.3%"
 *   formatNumber(NaN, 'compact')         // "—"
 *   formatNumber(Infinity, 'compact')    // "—" (EC-8 guard)
 *
 * ADR-D4 ratification: `bytes` intent uses SI 1000-base (1 KB = 1000 bytes),
 * NOT IEC 1024-base. Matches DigitalOcean / AWS / GCP billing surface
 * conventions. Edge Requests in Billing shows `1.24M` (1 million, not
 * 1,048,576). Fast Data Transfer shows `412 GB` per SI.
 */
export function formatNumber(
  value: number,
  intent: 'compact' | 'grouped' | 'bytes' | 'percent',
): string {
  // EC-8: NaN + Infinity + -Infinity all collapse via Number.isFinite
  if (!Number.isFinite(value)) return EMDASH;

  switch (intent) {
    case 'compact':
      return new Intl.NumberFormat('en-US', {
        notation: 'compact',
        maximumFractionDigits: 2,
      }).format(value);
    case 'grouped':
      return new Intl.NumberFormat('en-US').format(value);
    case 'bytes':
      return formatBytes(value);
    case 'percent':
      return new Intl.NumberFormat('en-US', {
        style: 'percent',
        maximumFractionDigits: 1,
      }).format(value);
  }
}

/**
 * SI 1000-base byte formatting per ADR-D4. Returns largest unit ≥ 1.
 *   1500       → "1.5 KB"
 *   1_500_000  → "1.5 MB"
 *   412_000_000_000 → "412 GB"
 */
function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let unitIdx = 0;
  let value = bytes;
  while (value >= 1000 && unitIdx < units.length - 1) {
    value /= 1000;
    unitIdx++;
  }
  const formatted = unitIdx === 0
    ? value.toString()
    : value.toLocaleString('en-US', { maximumFractionDigits: 2 });
  return `${formatted} ${units[unitIdx]}`;
}

/**
 * Mask email's local part while preserving the full domain. Strong signal
 * of "which account am I logged into" without exposing the local identifier.
 * Per plan v1.1 T-D.1 (P-3 + EC-10 defensive).
 *
 * @example
 *   maskEmail('demo@loopzero.example')        // "d***@loopzero.example"
 *   maskEmail('a@b.com')                 // "*@b.com"            (single-char local)
 *   maskEmail('verylonglocalpart@x.com') // "v********@x.com"    (capped at 8 stars)
 *   maskEmail('john@doe@malformed.com')  // "john@doe@malformed.com" (EC-10 passthrough)
 *   maskEmail('x@')                      // "x@"                 (no domain)
 *   maskEmail('@y.com')                  // "@y.com"             (empty local)
 */
export function maskEmail(email: string): string {
  const parts = email.split('@');
  if (parts.length !== 2) return email;   // EC-10: multi-@ defensive passthrough
  const [local, domain] = parts;
  if (!domain) return email;               // "x@" → no domain
  if (local.length === 0) return email;    // "@y.com" → empty local
  if (local.length === 1) return `*@${domain}`;
  return `${local[0]}${'*'.repeat(Math.min(local.length - 1, 8))}@${domain}`;
}

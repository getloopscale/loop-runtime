import Convert from 'ansi-to-html';
import type { ReactNode } from 'react';

/**
 * ansi.tsx — J-19 T1.2 (R2 blueprint MUST).
 *
 * Wraps the `ansi-to-html` library (MIT, 0.7.2, zero CVEs per OSV.dev
 * 2026-06-12 deps-audit) to convert ANSI escape sequences emitted by user
 * applications (`pino-pretty`, `chalk`, `colorette`, etc.) into colored HTML
 * spans rendered by the dashboard log viewer.
 *
 * **EC-10 XSS mitigation (per impl edge cases review)**: the Convert
 * instance is configured with `escapeXML: true` so any user-controlled `<`,
 * `>`, `&`, `"`, `'` is HTML-encoded BEFORE ANSI parsing. The `<` in
 * `<script>` becomes `&lt;script&gt;` — innerHTML rendering of the escaped
 * string cannot inject executable nodes. The single remaining injection
 * surface is the `<span style="...">` emitted by the library itself, whose
 * output we trust because (a) it only varies the color hex per ANSI code
 * and (b) the library is widely audited.
 *
 * Per blueprint ADR D2 + plan v1.2 § Dependencies, `ansi-to-html` was
 * chosen over `ansi_up` (more recent BUT class-based + lower adoption),
 * `anser` (stale 4y + AST output), `strip-ansi` (loses color), and
 * custom-parser (reinvent the wheel). See deps audit report for full
 * Rule 9 evaluation.
 *
 * Per `cli-output.md § Rule 3`, this layer uses product language only; no
 * "ANSI"/"escape sequence" terms appear in user-facing UX recommendations
 * — the colored output IS the product affordance.
 */

const convert = new Convert({
  newline: false,
  escapeXML: true, // EC-10 — server-controlled log content + permissive HTML library
});

const ANSI_ESC = '\x1b[';

export function hasAnsi(line: string | null | undefined): boolean {
  if (!line) return false;
  return line.includes(ANSI_ESC);
}

/**
 * Parse a single log line; return React node ready for render.
 *
 * - Fast path: lines without ANSI sequences return as-is (text node).
 * - Slow path: lines with ANSI sequences get converted to HTML by
 *   `ansi-to-html`, then wrapped in a single `<span>` via
 *   `dangerouslySetInnerHTML`. Acceptable XSS posture per EC-10 mitigation
 *   above (escapeXML escapes user payload before HTML emission).
 */
export function parseAnsi(line: string): ReactNode {
  if (!hasAnsi(line)) return line;
  const html = convert.toHtml(line);
  return <span dangerouslySetInnerHTML={{ __html: html }} />;
}

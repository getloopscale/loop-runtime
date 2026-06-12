/**
 * log-download.ts — J-19 T2.3 (R7 blueprint SHOULD).
 *
 * Exports the current `LogLine[]` buffer as a plain-text `.log` file via
 * the canonical Blob + anchor.click pattern.
 *
 * **EC-4 SAFARI/FIREFOX REVOKE TIMING** (per impl edge cases review):
 * `URL.revokeObjectURL` IMMEDIATELY after `anchor.click()` invalidates
 * the `blob:` URL before Safari (and some older Chrome / Firefox) can
 * stream the download. The MDN-canonical fix is to defer revoke via
 * `setTimeout(..., 1000)` — gives the browser time to start reading.
 *
 * Decision Q3 (plan Unresolved Questions, v1.1 default): the download
 * exports the FULL buffer, not the filter-mismatch subset. Matches Vercel
 * V1 terminal-redirect behavior. Revisit per sponsor walkthrough feedback.
 */

import type { LogLine } from './api/types';

export function formatLineForExport(line: LogLine): string {
  const src = line.source ? `${line.source} ` : '';
  return `${line.ts} ${line.level.toUpperCase()} ${src}${line.msg}`;
}

export function triggerLogDownload(lines: LogLine[]): void {
  if (typeof document === 'undefined') return;
  const text = lines.map(formatLineForExport).join('\n');
  const blob = new Blob([text], { type: 'text/plain' });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = `runtime-logs-${new Date().toISOString()}.log`;
  document.body.appendChild(a);
  a.click();
  // EC-4 — defer revoke so Safari + Firefox can finish streaming
  setTimeout(() => {
    URL.revokeObjectURL(url);
    a.remove();
  }, 1000);
}

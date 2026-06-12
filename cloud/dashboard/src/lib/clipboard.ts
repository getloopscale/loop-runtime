/**
 * clipboard.ts — J-19 T2.2 (R6 blueprint SHOULD).
 *
 * `navigator.clipboard.writeText` requires a SECURE CONTEXT per W3C —
 * HTTPS, `https://*.loopzero.example`, `localhost`, or `127.0.0.1`. On
 * non-secure origins (e.g., `http://192.168.x.x:5173` LAN dev pairing)
 * `navigator.clipboard` is `undefined` and the Promise-style copy
 * silently fails.
 *
 * **EC-3 MANDATORY FALLBACK** (per impl edge cases review): when the
 * native API is unavailable, fall back to `document.execCommand('copy')`
 * via a temporary `<textarea>` — works in ALL contexts. Caller receives
 * a boolean success indicator so a toast can render either way.
 */

export async function copyToClipboard(text: string): Promise<boolean> {
  // Happy path — modern secure context
  if (typeof navigator !== 'undefined' && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      // fall through to execCommand fallback
    }
  }
  // EC-3 fallback — temporary textarea + execCommand
  if (typeof document === 'undefined') return false;
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.top = '-9999px';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  let ok: boolean;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  document.body.removeChild(ta);
  return ok;
}

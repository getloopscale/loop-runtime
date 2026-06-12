/**
 * J-22 T3.5 — Dotenv parser (browser-side, KISS).
 *
 * Handles:
 * - KEY=value
 * - KEY="quoted value with spaces"
 * - KEY='single-quoted value'
 * - # comment lines (silently stripped per EC-17)
 * - empty lines (skipped)
 *
 * Detects:
 * - malformed lines (no `=` separator) — surfaced as warnings, NOT entries
 * - duplicate keys within the same upload — surfaced as warnings (EC-14)
 *
 * NEVER mutates the input file; pure function.
 */

export interface DotenvEntry {
  key: string;
  value: string;
  /** 1-based line number in the source file. */
  line: number;
}

export interface DotenvWarning {
  line: number;
  message: string;
}

export interface DotenvParseResult {
  entries: DotenvEntry[];
  warnings: DotenvWarning[];
}

const KEY_PATTERN = /^[A-Z][A-Z0-9_]{1,63}$/;

/**
 * Strip surrounding double-quotes or single-quotes from a value.
 * Quote-mismatch is treated as literal characters (no auto-escape).
 */
function unquote(raw: string): string {
  const trimmed = raw.trim();
  if (
    trimmed.length >= 2 &&
    ((trimmed[0] === '"' && trimmed[trimmed.length - 1] === '"') ||
      (trimmed[0] === "'" && trimmed[trimmed.length - 1] === "'"))
  ) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

export function parseDotenv(source: string): DotenvParseResult {
  const entries: DotenvEntry[] = [];
  const warnings: DotenvWarning[] = [];
  const seenKeys = new Set<string>();

  const lines = source.split(/\r?\n/);
  lines.forEach((rawLine, idx) => {
    const lineNum = idx + 1;
    const line = rawLine.trim();
    if (line.length === 0) return; // empty line
    if (line.startsWith('#')) return; // comment per EC-17

    const eqIdx = line.indexOf('=');
    if (eqIdx === -1) {
      warnings.push({ line: lineNum, message: `Malformed line (no '=' found)` });
      return;
    }

    const key = line.slice(0, eqIdx).trim();
    const value = unquote(line.slice(eqIdx + 1));

    if (!KEY_PATTERN.test(key)) {
      warnings.push({
        line: lineNum,
        message: `Invalid key "${key}" (must be SCREAMING_SNAKE_CASE, 2-64 chars)`,
      });
      return;
    }

    if (seenKeys.has(key)) {
      // EC-14: duplicate-key pre-flight warning.
      warnings.push({ line: lineNum, message: `Duplicate key "${key}"` });
      return;
    }
    seenKeys.add(key);

    entries.push({ key, value, line: lineNum });
  });

  return { entries, warnings };
}

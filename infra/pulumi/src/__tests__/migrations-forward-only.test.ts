// CHG-2026-04-30-INFRA-NORMALIZE / T5.3 — migration forward-only invariant
//
// Translates plan TDD pseudocode `test_migration_is_forward_only()` to real
// vitest. Pins the rule from CLAUDE.md ("Forward-only `.up.sql`") + plan
// T5.3 acceptance "Forward-only" + ADR D5 (no DOWN migrations).
//
// Three invariants:
//   1. NO DROP TABLE / DROP COLUMN / DROP INDEX / DROP CONSTRAINT in any .up.sql
//      (drops are destructive and irreversible; data loss in production)
//   2. NO *.down.sql files exist (golang-migrate would attempt rollback if present)
//   3. Sequence numbers are monotonically increasing (gaps allowed for renames
//      per CHANGELOG fix; 000003→000006 cells renumber is documented exception)
//
// Catches drift like 'someone wrote ALTER TABLE x DROP COLUMN y' in a migration
// without realizing this destroys live data and breaks the forward-only
// contract. Production migration would silently destroy customer state.

import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

const REPO_ROOT = resolve(__dirname, "..", "..", "..", "..");
const MIGRATIONS_DIR = resolve(REPO_ROOT, "api", "internal", "db", "migrations");

function listMigrations(): string[] {
  return readdirSync(MIGRATIONS_DIR).sort();
}

describe("migration forward-only invariant (T5.3)", () => {
  const files = listMigrations();
  const upFiles = files.filter((f) => f.endsWith(".up.sql"));
  const downFiles = files.filter((f) => f.endsWith(".down.sql"));

  it("at least one .up.sql migration exists (sanity)", () => {
    expect(upFiles.length, "expected ≥1 *.up.sql").toBeGreaterThan(0);
  });

  it("zero *.down.sql files (forward-only enforced by absence)", () => {
    expect(downFiles, `forward-only violation: down migrations present: ${downFiles.join(", ")}`).toEqual([]);
  });

  describe("no destructive DROP statements in any .up.sql", () => {
    // Match DROP <whitespace> + (TABLE|COLUMN|INDEX|CONSTRAINT) word-boundary,
    // case-insensitive, multiline. Allow 'IF EXISTS' between DROP and target.
    const DROP_RE = /\bDROP\s+(?:IF\s+EXISTS\s+)?(?:TABLE|COLUMN|INDEX|CONSTRAINT)\b/i;
    // CHECK constraint replacement pattern (DROP CONSTRAINT X + ADD CONSTRAINT X CHECK)
    // is NOT data-destructive — it's the only PostgreSQL idiom to extend a CHECK
    // enum (see migration 000008_archive_stale_releases for the canonical example).
    // We allow `DROP CONSTRAINT <name>` ONLY when an `ADD CONSTRAINT <name> CHECK`
    // appears in the same file (same logical transaction).
    const DROP_CONSTRAINT_RE = /\bDROP\s+(?:IF\s+EXISTS\s+)?CONSTRAINT\s+(\w+)\b/i;
    const ADD_CHECK_RE = /\bADD\s+CONSTRAINT\s+(\w+)\s+CHECK\b/i;

    for (const f of upFiles) {
      it(f, () => {
        const text = readFileSync(resolve(MIGRATIONS_DIR, f), "utf-8");
        const lines = text.split("\n");
        const offenders = lines
          .map((line, idx) => ({ line, idx: idx + 1 }))
          .filter(({ line }) => {
            // Skip comment lines (-- prefix or /* ... */ blocks).
            const stripped = line.replace(/--.*$/, "").trim();
            if (stripped.length === 0 || !DROP_RE.test(stripped)) return false;
            // Whitelist: DROP CONSTRAINT X is OK if same file has ADD CONSTRAINT X CHECK.
            const dropMatch = stripped.match(DROP_CONSTRAINT_RE);
            if (dropMatch) {
              const cname = dropMatch[1];
              const addCheckRe = new RegExp(`\\bADD\\s+CONSTRAINT\\s+${cname}\\s+CHECK\\b`, "i");
              if (addCheckRe.test(text)) return false; // constraint replacement, not data drop
            }
            return true;
          });
        expect(
          offenders.length,
          `${f}: forward-only violation at line(s) ${offenders.map((o) => `${o.idx} (${o.line.trim()})`).join("; ")}`,
        ).toBe(0);
      });
    }
  });

  it("migration sequence is monotonically increasing", () => {
    const seq = upFiles.map((f) => {
      const m = f.match(/^(\d+)_/);
      expect(m, `${f}: missing leading sequence number`).not.toBeNull();
      return parseInt(m![1], 10);
    });
    for (let i = 1; i < seq.length; i++) {
      expect(seq[i], `migration ${upFiles[i]} (${seq[i]}) must be > ${upFiles[i - 1]} (${seq[i - 1]})`).toBeGreaterThan(seq[i - 1]);
    }
  });

  it("000003 sequence gap is documented (cells migration renumbered)", () => {
    // The plan CHANGELOG records that 000003_cells.up.sql was renamed to
    // 000006_cells.up.sql because forward-only sequencing skipped 000003
    // on existing DBs already at version=5. Expected gap.
    const seq = upFiles.map((f) => parseInt(f.match(/^(\d+)_/)![1], 10));
    if (!seq.includes(3) && seq.includes(6)) {
      // Verify the cells migration ended up at 000006 with the rename
      // semantically intact (the file still creates the cells table).
      const cellsFile = upFiles.find((f) => f.includes("cells"));
      expect(cellsFile).toMatch(/^000006_/);
      const text = readFileSync(resolve(MIGRATIONS_DIR, cellsFile!), "utf-8");
      expect(text).toMatch(/CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?cells/i);
    }
  });
});

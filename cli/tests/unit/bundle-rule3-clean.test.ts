// CHG-2026-04-30-INFRA-PARITY / T3.1 — Rule 3 regression at the bundle level.
//
// CLI dist/ should contain ZERO occurrences of forbidden internal terms
// outside of explicit Rule 3 detection code (which lists them as forbidden
// strings). This catches regressions where someone reintroduces a leaky
// `console.log("Bootstrapping Gitea...")` style line in user-facing code.
//
// We allow occurrences in:
//   - Files that are themselves Rule 3 enforcement (mention forbidden terms
//     as a regex/list to detect leaks elsewhere).
//   - Test fixtures (not built into dist).
//
// Test reads dist/ AFTER `npm run build` runs. CI must build before tests.

import { describe, it, expect } from '@jest/globals';
import { execSync } from 'child_process';
import * as fs from 'fs';
import * as path from 'path';

const FORBIDDEN_TERMS = ['Gitea', 'MinIO', 'Kaniko', 'kubebuilder', 'envtest', 'ArgoCD', 'GitOps'];

const ALLOW_PATTERNS = [
  /forbidden/i,
  /forbiddenTerms/,
  /INTERNAL_TERMS/,
  /Rule 3/,
  // Comment-only lines: JSDoc blocks (`* ...`), block-comment continuations,
  // and trailing `//` line comments. Rule 3 enforces product language in
  // *user-facing output* (stdout/stderr, --json, error messages) — not in
  // technical comments that ship in the bundle but never execute. The grep
  // line format is `file:lineN:<content>`; the colon split lets us inspect
  // the actual content portion. (close-all-gaps iter 4 broad-verification)
  /:\s*\*\s/,         // JSDoc / block-comment body line (` *   text`)
  /:\s*\/\//,         // `// trailing comment`
  /:\s*\/\*/,         // `/* opening block-comment marker`
];

function isAllowedContext(line: string): boolean {
  return ALLOW_PATTERNS.some((re) => re.test(line));
}

describe('CLI bundle is free of Rule 3 internal-term leaks', () => {
  const distDir = path.resolve(__dirname, '../../dist');

  it('dist/ exists (build must run first)', () => {
    expect(fs.existsSync(distDir)).toBe(true);
  });

  for (const term of FORBIDDEN_TERMS) {
    it(`dist bundle contains zero "${term}" occurrences outside Rule 3 detection code`, () => {
      let leaks: string[] = [];
      try {
        const output = execSync(
          `grep -rnE "\\\\b${term}\\\\b" "${distDir}" || true`,
          { encoding: 'utf-8' },
        );
        leaks = output
          .split('\n')
          .filter((line) => line.length > 0)
          .filter((line) => !isAllowedContext(line));
      } catch {
        // grep -r with no matches exits 1; fine.
      }
      // Jest 27+ removed the global `fail()` helper. Surface the sample via
      // console.error so jest output captures it, then assert via expect.
      // (close-all-gaps iter 4 broad-verification)
      if (leaks.length > 0) {
        const sample = leaks.slice(0, 5).join('\n');
        console.error(`Found ${leaks.length} "${term}" leaks in dist/. Sample:\n${sample}`);
      }
      expect(leaks.length).toBe(0);
    });
  }
});

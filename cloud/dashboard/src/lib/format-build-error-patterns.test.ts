// format-build-error-patterns.test.ts
//
// Plan 2026-06-08-dashboard-screens-review-fixes T3.1 + ADR D4 + EC-3 +
// EC-5. 14+ tests covering:
//   - 8 pattern matchers (one happy-path per known build error class)
//   - Fallback chain (a) cap 10KB / (b) /workspace/ strip / (c) stack
//     frame strip / (d) node:internal strip / (e) blocklist sweep
//   - Wire-format invariants ({summary, detail} shape; details preserves
//     raw for the "Show details" toggle)
//
// CLAUDE.md Rule 3: the rendered `summary` MUST NOT contain any of:
//   ArgoCD | BuildKit | cosign | Kaniko | gyp | node-gyp
//   /workspace/<...>
//   node:internal/<...>
//   absolute stack-frame paths like <...>/foo.js:42

import { describe, it, expect } from 'vitest';
import {
  formatBuildError,
  PATTERN_IDS,
} from './format-build-error-patterns';

describe('formatBuildError — 8 documented patterns', () => {
  it('gyp/Python missing → product-language summary, NOT "gyp"', () => {
    const raw = 'npm ERR! code 1\ngyp ERR! find Python\ngyp ERR! python not found';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toContain('python');
    expect(out.summary.toLowerCase()).not.toContain('gyp');
    expect(out.detail).toBe(raw); // raw preserved for Show details toggle
  });

  it('manifest digest missing → "signing did not run"', () => {
    const raw =
      'Error: manifest_builder: missing image digest for app "web" (build did not emit cosign attestation)';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toMatch(/sign(ing)?\s+\S*\s*did not run/);
    expect(out.summary.toLowerCase()).not.toContain('cosign');
  });

  it('source bundle miss (Object does not exist) → "Push again"', () => {
    const raw =
      'clone-repo error: NoSuchKey: Object does not exist tn-abc/source/def456.bundle';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toContain('push again');
  });

  it('npm peer-dep missing → product summary', () => {
    const raw = 'npm ERR! ERESOLVE could not resolve\nnpm ERR! peer dep missing react@^17';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toMatch(/peer/);
  });

  it('WorkflowSpec.Plan must be non-nil → "loopzero.yaml has no apps"', () => {
    const raw = 'admission webhook denied: WorkflowSpec.Plan must be non-nil with at least one app';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toContain('loopzero.yaml');
    expect(out.summary.toLowerCase()).toContain('apps');
  });

  it('clone-repo Max duration limit → "Source clone took too long; retry"', () => {
    const raw = 'clone-repo: Max duration limit exceeded';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toMatch(/clone/);
    expect(out.summary.toLowerCase()).toMatch(/retry/);
  });

  it('forbidden service auth → "Internal credentials are being rotated"', () => {
    const raw = 'workflow submit failed: forbidden — service auth rejected (HTTP 403)';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).toMatch(/credentials/);
    expect(out.summary.toLowerCase()).toMatch(/retry|few minutes/);
  });

  it('pattern set IDs are stable + non-empty', () => {
    expect(PATTERN_IDS.length).toBeGreaterThanOrEqual(7);
    // Length-7 anti-drift guard — match plan T3.1 § Deep Dives 7 named
    // patterns (8th is the implicit fallback in formatBuildError).
    for (const id of PATTERN_IDS) {
      expect(id).toMatch(/^[a-z][a-z0-9-]+$/);
    }
  });
});

describe('formatBuildError — fallback chain (a-e) EC-3 + EC-5 absorptions', () => {
  it('(b) strips /workspace/ paths', () => {
    const raw =
      "TypeError: Cannot read property 'foo' of undefined\n    at Object.<anonymous> (/workspace/src/index.js)";
    const out = formatBuildError(raw);
    expect(out.summary).not.toContain('/workspace/');
    expect(out.summary).toContain('<path>');
  });

  it('(c) EC-3 — strips stack-frame file paths with line numbers', () => {
    const raw =
      'Error\n    at Object.<anonymous> (/usr/local/lib/node_modules/foo/index.js:42:11)';
    const out = formatBuildError(raw);
    expect(out.summary).toContain('<frame>');
    expect(out.summary).not.toMatch(/index\.js:42/);
  });

  it('(d) EC-3 — strips node:internal/* internals', () => {
    const raw = 'Error: ENOENT at node:internal/modules/cjs/loader.js:1234';
    const out = formatBuildError(raw);
    expect(out.summary).toContain('<runtime>');
    expect(out.summary).not.toContain('node:internal/');
  });

  it('(a) EC-5 — caps input at 10KB; suffix mentions truncation', () => {
    const raw = 'X'.repeat(15 * 1024); // 15KB
    const out = formatBuildError(raw);
    // Summary capped: 10KB + suffix not much longer
    expect(out.summary.length).toBeLessThanOrEqual(10240 + 200);
    expect(out.summary.toLowerCase()).toContain('truncated');
  });

  it('(e) blocklist — when ArgoCD/BuildKit/etc persists, replace whole summary', () => {
    // A failure that doesn't match any pattern but contains "ArgoCD".
    const raw = 'unexpected error: ArgoCD sync rejected with status FAILED';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).not.toContain('argocd');
    // Generic safe message replaces the whole summary.
    expect(out.summary.toLowerCase()).toMatch(/build failed/);
    expect(out.summary.toLowerCase()).toMatch(/log/);
  });

  it('(e) blocklist — BuildKit stripped to safe message', () => {
    const raw = 'unhandled: BuildKit emit failed for app';
    const out = formatBuildError(raw);
    expect(out.summary.toLowerCase()).not.toContain('buildkit');
  });

  it('empty input → "Unknown error." (defense in depth)', () => {
    expect(formatBuildError('').summary).toMatch(/unknown/i);
    expect(formatBuildError('').detail).toBe('');
  });
});

describe('formatBuildError — wire-format invariants', () => {
  it('always returns {summary, detail} shape', () => {
    const out = formatBuildError('any random string');
    expect(typeof out.summary).toBe('string');
    expect(typeof out.detail).toBe('string');
    expect(out.summary.length).toBeGreaterThan(0);
  });

  it('detail preserves raw (Show details toggle source-of-truth)', () => {
    const raw = 'arbitrary build output that the operator may need to debug';
    const out = formatBuildError(raw);
    expect(out.detail).toBe(raw);
  });
});

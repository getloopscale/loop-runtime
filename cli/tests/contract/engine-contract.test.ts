// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// M2 (plan m2-extract-loopzero-cli) — cli ↔ engine consumer-driven contract, TS side.
//
// This is the TS counterpart of the Go engine-side test (loopzero/api/internal/
// contract/cli_consumer_test.go). It lives in the CLI so that after extraction
// the consumer drift guard travels WITH loopzero-cli (M1-review F-4 / M2 D3/D4): the
// CLI is the consumer; it asserts every /api/v1 path it actually calls exists in
// the vendored engine OpenAPI snapshot. EC-1: the path extractor + normalizer is
// implemented in TS here (the Go normalizer is not reusable from TS).

import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'js-yaml';

const SRC_DIR = path.resolve(__dirname, '../../src');
const SNAPSHOT = path.resolve(__dirname, 'engine.yaml');

// Known consumer/producer gaps: paths the CLI references that the engine does NOT
// serve. Documented, not hidden (mirrors the Go knownConsumerGaps). dns-records
// is CLI dead/aspirational code with no engine route at HEAD.
const KNOWN_GAPS = new Set<string>(['/api/v1/domains/{}/dns-records']);

// normalizePath: raw CLI URL → template form. Mirrors the Go normalizer:
// drop query, ${...}→{}, {param}→{}, glued word{}→word, trim trailing punct.
export function normalizePath(raw: string): string {
  let p = raw;
  const q = p.indexOf('?');
  if (q >= 0) p = p.slice(0, q);
  p = p.replace(/\$\{[^}]*\}/g, '{}'); // closed ${...}
  const open = p.indexOf('${'); // unclosed / ternary boundary
  if (open >= 0) p = p.slice(0, open);
  for (const b of [' ', '`', "'", '"']) {
    const i = p.indexOf(b);
    if (i >= 0) p = p.slice(0, i);
  }
  p = p.replace(/\{[^}]*\}/g, '{}'); // {id}/{name}
  p = p.replace(/([A-Za-z0-9])\{\}/g, '$1'); // glued query interp
  p = p.replace(/[./]+$/, ''); // trailing punct / slash
  return p;
}

function walkTsFiles(dir: string): string[] {
  const out: string[] = [];
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) out.push(...walkTsFiles(full));
    else if (full.endsWith('.ts') || full.endsWith('.tsx')) out.push(full);
  }
  return out;
}

const API_PATH_RE = /\/api\/v1\/[A-Za-z0-9/_${}().:?=&-]*/g;
const CLEAN_RE = /^\/api\/v1\/[A-Za-z0-9/{}_-]+$/;

function liveCliPaths(): Set<string> {
  const set = new Set<string>();
  for (const f of walkTsFiles(SRC_DIR)) {
    const src = fs.readFileSync(f, 'utf8');
    for (const m of src.match(API_PATH_RE) ?? []) {
      const n = normalizePath(m);
      if (CLEAN_RE.test(n)) set.add(n);
    }
  }
  return set;
}

function specPaths(): Set<string> {
  const doc = yaml.load(fs.readFileSync(SNAPSHOT, 'utf8')) as { paths?: Record<string, unknown> };
  const set = new Set<string>();
  for (const p of Object.keys(doc.paths ?? {})) {
    set.add(p.replace(/\{[^}]*\}/g, '{}'));
  }
  return set;
}

describe('normalizePath', () => {
  it.each([
    ['/api/v1/builds/${encodeURIComponent(id)}/cancel', '/api/v1/builds/{}/cancel'],
    ['/api/v1/projects/${encodeURIComponent(p)}/builds${query ?', '/api/v1/projects/{}/builds'],
    ['/api/v1/environments${query}', '/api/v1/environments'],
    ['/api/v1/audit?${params.toString()}', '/api/v1/audit'],
    ['/api/v1/projects', '/api/v1/projects'],
  ])('normalizes %s', (input, want) => {
    expect(normalizePath(input)).toBe(want);
  });
});

describe('cli ↔ engine contract (vendored engine.yaml snapshot)', () => {
  it('every CLI-consumed /api/v1 path exists in the engine spec (or is a known gap)', () => {
    const spec = specPaths();
    const live = liveCliPaths();
    const missing = [...live].filter((p) => !spec.has(p) && !KNOWN_GAPS.has(p)).sort();
    expect(missing).toEqual([]);
  });

  it('known gaps are real (referenced by the CLI, absent from the spec)', () => {
    const spec = specPaths();
    const live = liveCliPaths();
    for (const gap of KNOWN_GAPS) {
      expect(live.has(gap)).toBe(true); // CLI still references it
      expect(spec.has(gap)).toBe(false); // engine still does not serve it
    }
  });
});

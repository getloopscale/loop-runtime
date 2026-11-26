// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// CHG-2026-04-30-INFRA-PARITY / T4.2 — image tag format enforcement.
//
// Helmfile-rendered LoopRuntime charts must use:
//   (a) semver  — `v0.2.0`, `v1.10.3-rc1`
//   (b) SHA     — `sha-{8..40 hex chars}` or just `[a-f0-9]{8,40}`
//   (c) latest  — `latest` (allowed; production blocked by VAP
//                 `loop-runtime-disallow-latest-tag` runtime; CI here only blocks
//                 branch-name tags from leaking).
//
// Branch-name tags like `f07-fix`, `REDACTED_SECRET_061`, `achado28` mask the
// relationship between Git and runtime — kubectl set image hot-patches go
// undetected by ArgoCD reconciliation. ADR D1 forbids them.

import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

const REPO_ROOT = path.resolve(__dirname, '..', '..', '..', '..');

// Accepted formats:
//   v0.2.0, v1.10.3, v1.10.3-rc1, v0.2.0-alpha.1
//   sha-deadbeef12345678, deadbeef (8-40 hex)
//   latest
const ACCEPTED_TAG_RE =
  /^(v\d+\.\d+\.\d+(-[a-zA-Z0-9.-]+)?|sha-[a-f0-9]{8,40}|[a-f0-9]{8,40}|latest)$/;

interface TagInstance {
  file: string;
  tag: string;
  context: string; // e.g. "loop-runtime-api/values.yaml"
}

function findHelmTagsInDir(dir: string): TagInstance[] {
  const tags: TagInstance[] = [];
  if (!fs.existsSync(dir)) return tags;

  function walk(current: string) {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const fp = path.join(current, entry.name);
      if (entry.isDirectory()) {
        if (entry.name === 'node_modules' || entry.name === 'charts') continue;
        walk(fp);
      } else if (entry.isFile() && /^values.*\.ya?ml$/.test(entry.name)) {
        const lines = fs.readFileSync(fp, 'utf-8').split('\n');
        for (let i = 0; i < lines.length; i++) {
          const m = lines[i].match(/^\s+tag:\s*"?([^"\s#]+)"?\s*(#.*)?$/);
          if (m) {
            tags.push({
              file: fp,
              tag: m[1],
              context: path.relative(REPO_ROOT, fp),
            });
          }
        }
      }
    }
  }
  walk(dir);
  return tags;
}

describe('image tag format enforcement (ADR D1)', () => {
  const helmDir = path.join(REPO_ROOT, 'infra', 'helm');
  const tags = findHelmTagsInDir(helmDir);

  // Filter only tags from LoopRuntime charts (not third-party). LoopRuntime charts include
  // loop-runtime-* AND tenant-controller (sibling without prefix).
  const loopRuntimeTags = tags.filter((t) => /infra\/helm\/(loop-runtime-|tenant-controller\/)/.test(t.context));

  it('found at least one LoopRuntime chart tag to validate', () => {
    expect(loopRuntimeTags.length).toBeGreaterThan(0);
  });

  for (const t of loopRuntimeTags) {
    it(`tag in ${t.context} matches ADR D1 format (semver|sha|latest)`, () => {
      const ok = ACCEPTED_TAG_RE.test(t.tag);
      if (!ok) {
        throw new Error(
          `Invalid tag "${t.tag}" in ${t.context}. ` +
            `Expected semver (v0.2.0), SHA (sha-deadbeef…), or latest. ` +
            `Branch-name tags like 'f07-fix' or 'REDACTED_SECRET_061' are forbidden by ADR D1.`,
        );
      }
      expect(ok).toBe(true);
    });

    if (t.tag === 'latest') {
      it(`pullPolicy=Always when tag=latest in ${t.context} (avoid K8s caching the same tag)`, () => {
        const lines = fs.readFileSync(t.file, 'utf-8').split('\n');
        // pullPolicy lives in the same image: block as the tag we matched.
        // Find the nearest pullPolicy after the image: marker.
        const imageIdx = lines.findIndex((l) => /^image:/.test(l));
        const policyLine = lines.slice(imageIdx, imageIdx + 10).find((l) => /pullPolicy:/.test(l));
        if (!policyLine) {
          throw new Error(`No pullPolicy found in image: block of ${t.context}`);
        }
        const policy = policyLine.split(':')[1].trim();
        if (policy !== 'Always') {
          throw new Error(
            `tag=latest requires pullPolicy=Always (got "${policy}") in ${t.context}. ` +
              `IfNotPresent + latest = K8s never pulls updates after first run.`,
          );
        }
        expect(policy).toBe('Always');
      });
    }
  }
});

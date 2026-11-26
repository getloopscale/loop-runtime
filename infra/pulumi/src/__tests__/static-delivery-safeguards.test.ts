// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Audit gap M05 #17 / §6.C.7b regression suite.
//
// The original audit pointed at hardcoded values in infra/cloudflare/worker.mjs.
// Investigation (§6.C.7 in the active plan) showed the Pulumi component
// `createStaticDelivery` already injects per-environment values into the worker
// at deploy time via regex-replace on the rendered worker.mjs text:
//
//   const S3_ENDPOINT = "..."  →  const S3_ENDPOINT = "<injected>";
//   const BASE_DOMAIN  = "..."  →  const BASE_DOMAIN  = "<injected>";
//
// Two silent-drift failure modes existed:
//
//   D1. The constant name in worker.mjs is renamed (S3_ENDPOINT → S3_URL).
//       The Pulumi regex no longer matches, replace silently no-ops, and the
//       deployed worker carries whatever literal is in the file (typically
//       the local-dev default) into production.
//
//   D2. The Pulumi config value resolves to empty string (Pulumi stack
//       output missing, secret unset, upstream returned ""). The regex
//       succeeds and writes `const S3_ENDPOINT = "";` into the worker.
//       Every static request 502s with no diagnostic signal.
//
// §6.C.7b adds 4 deploy-time safeguards in static-delivery.ts:
//   (1) sentinel check that S3_ENDPOINT declaration is present in worker.mjs
//   (2) sentinel check that BASE_DOMAIN declaration is present
//   (3) refuse empty s3Endpoint at apply-time
//   (4) refuse empty baseDomain at apply-time
//
// These tests guard the 4 safeguards from drift — if someone weakens them
// (e.g., relaxes the regex, removes the empty-string check), the test fails.

import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

const REPO_ROOT = path.resolve(__dirname, '../../../../');
const STATIC_DELIVERY_SRC = path.join(
  REPO_ROOT,
  'infra/pulumi/src/providers/cloudflare/static-delivery.ts',
);
const WORKER_MJS = path.join(REPO_ROOT, 'infra/cloudflare/worker.mjs');

describe('static-delivery.ts safeguards (audit M05 #17 / §6.C.7b)', () => {
  const src = fs.readFileSync(STATIC_DELIVERY_SRC, 'utf-8');

  describe('D1 — sentinel checks on worker.mjs template', () => {
    it('asserts the S3_ENDPOINT constant declaration is present in worker.mjs', () => {
      // The component must run a regex test on the template BEFORE the
      // replace, and throw if the sentinel constant is missing. Otherwise
      // a rename in worker.mjs silently no-ops the inject.
      expect(src).toMatch(/!\/const S3_ENDPOINT = \".\*\";\/.test\(workerTemplate\)/);
    });

    it('asserts the BASE_DOMAIN constant declaration is present in worker.mjs', () => {
      expect(src).toMatch(/!\/const BASE_DOMAIN = \".\*\";\/.test\(workerTemplate\)/);
    });

    it('throws an Error citing audit gap on missing sentinel', () => {
      // Anchors the audit-ref string so log search can correlate a deploy
      // failure to this guard.
      expect(src).toContain('audit M05 #17 (§6.C.7b)');
    });
  });

  describe('D2 — empty-value refusal at apply-time', () => {
    it('refuses empty s3Endpoint in the apply() closure', () => {
      // The apply() closure receives the resolved s3Endpoint; the
      // safeguard must throw on empty string. Pattern: `if (!s3Endpoint
      // || s3Endpoint.trim() === "") throw ...`.
      expect(src).toMatch(/!s3Endpoint\s*\|\|\s*s3Endpoint\.trim\(\)\s*===\s*""/);
    });

    it('refuses empty config.baseDomain', () => {
      expect(src).toMatch(/!config\.baseDomain\s*\|\|\s*config\.baseDomain\.trim\(\)\s*===\s*""/);
    });
  });

  describe('worker.mjs side — invariant the safeguards depend on', () => {
    // The sentinel checks in static-delivery.ts assume worker.mjs declares
    // BOTH constants at the top with the canonical `const X = "...";`
    // pattern. If the worker file ever drops to env-binding or wrangler
    // config, the safeguard regex no longer matches anything and the
    // component fails closed — which is the intended behavior, but this
    // test pins the current contract so the failure is visible BEFORE a
    // pulumi up against prod.
    const workerSrc = fs.readFileSync(WORKER_MJS, 'utf-8');

    it('declares const S3_ENDPOINT at the top of worker.mjs', () => {
      expect(workerSrc).toMatch(/^const S3_ENDPOINT = ".*";$/m);
    });

    it('declares const BASE_DOMAIN at the top of worker.mjs', () => {
      expect(workerSrc).toMatch(/^const BASE_DOMAIN = ".*";$/m);
    });
  });
});

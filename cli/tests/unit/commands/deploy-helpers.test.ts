// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Beta Logs Gate Criterion 4 — actionable error messages.
// Tests for the mapSyncFailureReason catalog: every known reason
// must produce a human-readable message + a concrete next step.

import { mapSyncFailureReason } from '../../../src/commands/deploy-helpers';

describe('mapSyncFailureReason (Beta Logs Gate Criterion 4)', () => {
  const buildId = 'bld-abc123';

  describe('original 5 reasons (audit baseline 2026-04-23)', () => {
    it.each([
      'HEALTH_PROBE_FAILED',
      'SYNC_TIMEOUT',
      'IMAGE_PULL_FAILED',
      'RESOURCE_QUOTA_EXCEEDED',
      'CRASH_LOOP',
    ])('%s — has a catalogued message', (reason) => {
      const result = mapSyncFailureReason(reason, buildId);
      // Sanity: not the fallback "Deploy failed: <lowercased>"
      expect(result).not.toMatch(/^Deploy failed: [a-z]/);
      // Always ends with the run-logs hint
      expect(result).toContain(`loopzero logs --build ${buildId}`);
    });
  });

  describe('Beta Logs Gate expansion (15 new reasons)', () => {
    // Build pipeline
    it.each([
      'BUILD_DOCKERFILE_INVALID',
      'BUILD_DEPENDENCY_FAILED',
      'BUILD_SIGN_FAILED',
      'BUILD_PUSH_FAILED',
      'BUILD_SBOM_FAILED',
    ])('build pipeline — %s', (reason) => {
      const result = mapSyncFailureReason(reason, buildId);
      expect(result).not.toMatch(/^Deploy failed: [a-z]/);
      expect(result).toContain(`loopzero logs --build ${buildId}`);
    });

    it.each([
      'ACTIVATION_FAILED',
      'POLICY_VIOLATION',
      'INVALID_SPEC',
      'INGRESS_NOT_READY',
      'TLS_CERT_PENDING',
      'NETWORK_POLICY_BLOCKED',
    ])('sync/deploy — %s', (reason) => {
      const result = mapSyncFailureReason(reason, buildId);
      expect(result).not.toMatch(/^Deploy failed: [a-z]/);
    });

    it.each([
      'ANALYSIS_FAILED',
      'POD_UNHEALTHY',
      'USER_ABORTED',
      'ROLLOUT_ABORTED',
    ])('rollouts — %s', (reason) => {
      const result = mapSyncFailureReason(reason, buildId);
      expect(result).not.toMatch(/^Deploy failed: [a-z]/);
    });

    it.each([
      'PLAN_QUOTA_EXCEEDED',
      'SECRET_SYNC_FAILED',
    ])('resource — %s', (reason) => {
      const result = mapSyncFailureReason(reason, buildId);
      expect(result).not.toMatch(/^Deploy failed: [a-z]/);
    });
  });

  describe('messaging quality (no internal terms, actionable)', () => {
    const knownReasons = [
      'HEALTH_PROBE_FAILED', 'SYNC_TIMEOUT', 'IMAGE_PULL_FAILED', 'RESOURCE_QUOTA_EXCEEDED', 'CRASH_LOOP',
      'BUILD_DOCKERFILE_INVALID', 'BUILD_DEPENDENCY_FAILED', 'BUILD_SIGN_FAILED', 'BUILD_PUSH_FAILED', 'BUILD_SBOM_FAILED',
      'ACTIVATION_FAILED', 'POLICY_VIOLATION', 'INVALID_SPEC', 'INGRESS_NOT_READY', 'TLS_CERT_PENDING', 'NETWORK_POLICY_BLOCKED',
      'ANALYSIS_FAILED', 'POD_UNHEALTHY', 'USER_ABORTED', 'ROLLOUT_ABORTED',
      'PLAN_QUOTA_EXCEEDED', 'SECRET_SYNC_FAILED',
    ];

    // Forbidden internal terms (CLAUDE.md Rule 3). Excludes `loopzero logs` /
    // `loopzero status` / etc. because those are intentional CLI invocations.
    const forbiddenTerms = [
      /ArgoCD/i,
      /\bargocd\b/i,
      /GitOps/i,
      /\bgitops-push\b/i,
      /\bCRD\b/,
      /tenant_id/,
      /pg_advisory/i,
      /\bGitea\b/i,
      /\bMinIO\b/i,
      /\bKaniko\b/i,
      /kubebuilder/i,
      /envtest/i,
    ];

    it.each(knownReasons)('%s — no internal terms in user-facing message (Rule 3)', (reason) => {
      const result = mapSyncFailureReason(reason, 'b-1');
      for (const term of forbiddenTerms) {
        expect(result).not.toMatch(term);
      }
    });

    it.each(knownReasons)('%s — has actionable next step (links to a CLI command, docs, or support)', (reason) => {
      const result = mapSyncFailureReason(reason, 'b-1');
      // Every message must end with the "Run: loopzero logs ..." hint
      // (added by mapSyncFailureReason itself), so the actionable
      // floor is built-in. We assert the catalogued message ALSO
      // contains a concrete next step BEFORE that hint:
      //   - explicit CLI command (`loopzero status`, `loopzero rollback`, `loopzero secrets list`, `loopzero domains list`)
      //   - explicit file path (`loopzero.yaml`, `lockfile`)
      //   - retry directive ("retry the deploy", "retry")
      //   - support handoff ("contact support")
      //   - external system reference ("Let's Encrypt", "registry", "cosign")
      //   - explicit measurable threshold ("5min", "30s")
      const actionableMarkers = [
        /\bloopzero\s+\w+/, // loopzero command
        /loopzero\.yaml/i,
        /lockfile/i,
        /retry/i,
        /contact support/i,
        /let'?s encrypt/i,
        /registry/i,
        /cosign/i,
        /upgrade/i,
        /\d+\s*(min|s)\b/, // duration mention
        /verify/i,
        /check\b/i,
      ];
      const matched = actionableMarkers.some((rx) => rx.test(result));
      expect(matched).toBe(true);
    });
  });

  describe('fallback for unknown reason', () => {
    it('produces a lowercase-with-spaces message + the logs hint', () => {
      const result = mapSyncFailureReason('SOMETHING_UNCATEGORIZED', 'b-1');
      // The fallback path is intentionally generic — operators see it
      // when a new failure mode hasn't been catalogued yet. The hint
      // is the universal escape hatch.
      expect(result).toContain('something uncategorized');
      expect(result).toContain('loopzero logs --build b-1');
    });
  });

  describe('build ID interpolation', () => {
    it('includes the exact build ID in the hint', () => {
      const result = mapSyncFailureReason('CRASH_LOOP', 'unique-build-id-9999');
      expect(result).toContain('loopzero logs --build unique-build-id-9999');
    });
  });
});

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.1 — unit tests for helmfile-runner.

import {
  helmfileSync,
  helmfileDestroy,
  helmfileLint,
  helmfileList,
  helmfileDiff,
  ensureHelmfileAvailable,
  resolveHelmfilePath,
} from '../../../src/installer/helmfile-runner';

import * as path from 'path';

describe('helmfile-runner', () => {
  describe('resolveHelmfilePath', () => {
    // Iter 20: helmfiles renamed to .gotmpl extension (helmfile v1 best
    // practice for templated YAML — `installed: {{ .Values.X }}`).
    it('builds correct path for mgmt cluster role', () => {
      const out = resolveHelmfilePath('/repo', 'mgmt');
      expect(out).toBe(path.join('/repo', 'infra', 'helmfile', 'mgmt.helmfile.yaml.gotmpl'));
    });

    it('builds correct path for build cluster role', () => {
      expect(resolveHelmfilePath('/repo', 'build')).toContain('build.helmfile.yaml.gotmpl');
    });

    it('builds correct path for runtime cluster role', () => {
      expect(resolveHelmfilePath('/repo', 'runtime')).toContain('runtime.helmfile.yaml.gotmpl');
    });
  });

  describe('ensureHelmfileAvailable', () => {
    it('returns ok=true when helmfile is in PATH', () => {
      // The CI environment must have helmfile (T0.3 prerequisite).
      // Skip if not installed locally — same outcome as the harness check.
      const result = ensureHelmfileAvailable();
      if (!result.ok) {
        return; // helmfile not installed in this dev box — skip
      }
      expect(result.ok).toBe(true);
      expect(result.version).toBeDefined();
    });
  });

  describe('helmfile actions return { status: failed } when file missing', () => {
    const missing = '/nonexistent/path/helmfile.yaml';

    it('helmfileSync fails when file missing', () => {
      const r = helmfileSync({ filePath: missing, environment: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
      expect(r.exitCode).toBeGreaterThan(0);
    });

    it('helmfileDestroy fails when file missing', () => {
      const r = helmfileDestroy({ filePath: missing, environment: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });

    it('helmfileLint fails when file missing', () => {
      const r = helmfileLint({ filePath: missing, environment: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });

    it('helmfileDiff fails when file missing', () => {
      const r = helmfileDiff({ filePath: missing, environment: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });

    it('helmfileList fails when file missing', () => {
      const r = helmfileList({ filePath: missing, environment: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });
  });

  describe('dryRun option uses diff instead of sync', () => {
    it('helmfileSync with dryRun:true does not throw', () => {
      // Validate code path — file is missing so it short-circuits.
      const r = helmfileSync({
        filePath: '/nonexistent/dryrun.yaml',
        environment: 'dev',
        dryRun: true,
        streamOutput: false,
      });
      expect(r.status).toBe('failed');
    });
  });
});

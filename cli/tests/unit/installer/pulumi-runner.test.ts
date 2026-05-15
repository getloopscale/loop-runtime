// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.1 — unit tests for pulumi-runner.

import {
  pulumiUp,
  pulumiPreview,
  pulumiDestroy,
  pulumiOutput,
  pulumiOutputs,
  ensurePulumiAvailable,
  resolvePulumiProjectDir,
  resolveProjectRoot,
} from '../../../src/installer/pulumi-runner';

import * as path from 'path';

describe('pulumi-runner', () => {
  describe('resolvePulumiProjectDir', () => {
    it('builds the canonical L1 project directory under repo root', () => {
      const out = resolvePulumiProjectDir('/repo');
      expect(out).toBe(path.join('/repo', 'infra', 'pulumi'));
    });
  });

  describe('resolveProjectRoot', () => {
    it('walks up to find Pulumi.yaml or returns cwd', () => {
      const root = resolveProjectRoot();
      expect(typeof root).toBe('string');
      expect(root.length).toBeGreaterThan(0);
    });
  });

  describe('actions short-circuit when projectDir is missing', () => {
    const missing = '/nonexistent/pulumi/project';

    it('pulumiUp fails on missing dir', () => {
      const r = pulumiUp({ projectDir: missing, stack: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });

    it('pulumiPreview fails on missing dir', () => {
      const r = pulumiPreview({ projectDir: missing, stack: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });

    it('pulumiDestroy fails on missing dir', () => {
      const r = pulumiDestroy({ projectDir: missing, stack: 'dev', streamOutput: false });
      expect(r.status).toBe('failed');
    });
  });

  describe('pulumiOutput / pulumiOutputs return null on missing project', () => {
    const missing = '/nonexistent/pulumi/project';

    it('pulumiOutput returns null when pulumi fails', () => {
      const v = pulumiOutput({ projectDir: missing, stack: 'dev' }, 'someKey');
      expect(v).toBeNull();
    });

    it('pulumiOutputs returns null when pulumi fails', () => {
      const v = pulumiOutputs({ projectDir: missing, stack: 'dev' });
      expect(v).toBeNull();
    });
  });

  describe('ensurePulumiAvailable', () => {
    it('returns a result object with ok flag', () => {
      const r = ensurePulumiAvailable();
      expect(typeof r.ok).toBe('boolean');
      if (r.ok) {
        expect(typeof r.version).toBe('string');
      } else {
        expect(typeof r.error).toBe('string');
      }
    });
  });
});

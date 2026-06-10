// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T5.1 — unit tests for preflight.

import { runPreflight, formatPreflightResult } from '../../../src/installer/preflight';

describe('preflight', () => {
  describe('basic structure', () => {
    it('returns a PreflightResult object with checks/blockers/warnings/ok', () => {
      const r = runPreflight();
      expect(Array.isArray(r.checks)).toBe(true);
      expect(Array.isArray(r.blockers)).toBe(true);
      expect(Array.isArray(r.warnings)).toBe(true);
      expect(typeof r.ok).toBe('boolean');
    });

    it('includes core tooling checks regardless of provider', () => {
      const r = runPreflight();
      const names = r.checks.map((c) => c.name);
      expect(names.some((n) => n.includes('helmfile'))).toBe(true);
      expect(names.some((n) => n.includes('helm '))).toBe(true);
      expect(names.some((n) => n.includes('kubectl'))).toBe(true);
    });

    it('every check has a severity in {BLOCK, WARN, INFO}', () => {
      const r = runPreflight();
      for (const c of r.checks) {
        expect(['BLOCK', 'WARN', 'INFO']).toContain(c.severity);
      }
    });

    it('every check has a status in {pass, fail, warn, info, skipped}', () => {
      const r = runPreflight();
      for (const c of r.checks) {
        expect(['pass', 'fail', 'warn', 'info', 'skipped']).toContain(c.status);
      }
    });

    it('blockers contain only severity=BLOCK with status=fail', () => {
      const r = runPreflight();
      for (const b of r.blockers) {
        expect(b.severity).toBe('BLOCK');
        expect(b.status).toBe('fail');
      }
    });
  });

  describe('provider-specific checks', () => {
    it('digitalocean provider adds pulumi/doctl/quota checks', () => {
      const r = runPreflight({ provider: 'digitalocean' });
      const names = r.checks.map((c) => c.name);
      expect(names.some((n) => n.toLowerCase().includes('pulumi'))).toBe(true);
      expect(names.some((n) => n.toLowerCase().includes('doctl'))).toBe(true);
    });

    it('digitalocean + cfAccountId adds CF token check', () => {
      const r = runPreflight({ provider: 'digitalocean', cfAccountId: 'fake-account' });
      const names = r.checks.map((c) => c.name);
      expect(names.some((n) => n.includes('CF API'))).toBe(true);
    });

    it('no provider — preflight only includes generic tools', () => {
      const r = runPreflight();
      const names = r.checks.map((c) => c.name);
      expect(names.some((n) => n.toLowerCase().includes('doctl'))).toBe(false);
    });
  });

  describe('formatPreflightResult', () => {
    it('writes one line per check', () => {
      const r = runPreflight();
      const lines: string[] = [];
      formatPreflightResult(r, (msg) => lines.push(msg));
      // At least one line per check (some messages emit additional context)
      expect(lines.length).toBeGreaterThanOrEqual(r.checks.length);
    });

    it('uses ✓ icon for pass status', () => {
      const r: import('../../../src/installer/preflight').PreflightResult = {
        ok: true,
        checks: [{ name: 'fake', severity: 'WARN', status: 'pass' }],
        blockers: [],
        warnings: [],
      };
      const lines: string[] = [];
      formatPreflightResult(r, (msg) => lines.push(msg));
      expect(lines.some((l) => l.includes('✓'))).toBe(true);
    });

    it('uses ✗ icon for fail status with remediation', () => {
      const r: import('../../../src/installer/preflight').PreflightResult = {
        ok: false,
        checks: [{ name: 'fake', severity: 'BLOCK', status: 'fail', remediation: 'install X' }],
        blockers: [{ name: 'fake', severity: 'BLOCK', status: 'fail', remediation: 'install X' }],
        warnings: [],
      };
      const lines: string[] = [];
      formatPreflightResult(r, (msg) => lines.push(msg));
      expect(lines.some((l) => l.includes('✗'))).toBe(true);
      expect(lines.some((l) => l.includes('install X'))).toBe(true);
    });
  });
});

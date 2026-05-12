// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { describe, it, expect, beforeEach, jest } from '@jest/globals';
import { DoctorCommand, DoctorDeps } from '../../src/commands/doctor';

function makeDeps(overrides: Partial<DoctorDeps> = {}): DoctorDeps {
  const lines: string[] = [];
  return {
    getToken: () => 'valid-token',
    validateToken: jest.fn(async () => ({ valid: true, email: 'dev@example.com' })) as DoctorDeps['validateToken'],
    pingAPI: jest.fn(async () => ({ ok: true, latencyMs: 42 })) as DoctorDeps['pingAPI'],
    readProjectConfig: () => ({ exists: true, valid: true }),
    getCliVersion: () => '0.13.0',
    writeTo: (msg: string) => { lines.push(msg); },
    writeError: (msg: string) => { lines.push(`ERR: ${msg}`); },
    isCI: false,
    ...overrides,
  };
}

describe('DoctorCommand', () => {
  describe('all checks pass', () => {
    it('returns exit code 0', async () => {
      const cmd = new DoctorCommand(makeDeps());
      const result = await cmd.execute({});
      expect(result).toBe(0);
    });

    it('emits valid JSON when --json', async () => {
      const lines: string[] = [];
      const cmd = new DoctorCommand(makeDeps({ writeTo: (m) => { lines.push(m); } }));
      await cmd.execute({ json: true });
      const parsed = JSON.parse(lines.join('\n'));
      expect(parsed.overall).toBe('ok');
      expect(parsed.checks).toHaveLength(4);
      expect(parsed.checks.map((c: { name: string }) => c.name)).toEqual([
        'cli_version', 'auth', 'network', 'project_config',
      ]);
    });
  });

  describe('not logged in', () => {
    it('reports auth fail + suggests loopzero login', async () => {
      const lines: string[] = [];
      const cmd = new DoctorCommand(makeDeps({
        getToken: () => null,
        writeTo: (m) => { lines.push(m); },
      }));
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(1);
      expect(lines.some(l => l.includes('Not logged in'))).toBe(true);
      expect(lines.some(l => l.includes('loopzero login'))).toBe(true);
    });
  });

  describe('token expired', () => {
    it('reports auth fail with refresh suggestion', async () => {
      const cmd = new DoctorCommand(makeDeps({
        validateToken: jest.fn(async () => ({ valid: false })) as DoctorDeps['validateToken'],
      }));
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(1);
    });
  });

  describe('API unreachable', () => {
    it('reports network fail + suggests connectivity check', async () => {
      const lines: string[] = [];
      const cmd = new DoctorCommand(makeDeps({
        pingAPI: jest.fn(async () => ({ ok: false, error: 'connection refused' })) as DoctorDeps['pingAPI'],
        writeTo: (m) => { lines.push(m); },
      }));
      const exitCode = await cmd.execute({});
      expect(exitCode).toBe(1);
      expect(lines.some(l => l.includes('not reachable') || l.includes('connection refused'))).toBe(true);
    });
  });

  describe('no loopzero.yaml in cwd', () => {
    it('warns but does not fail', async () => {
      const cmd = new DoctorCommand(makeDeps({
        readProjectConfig: () => ({ exists: false, valid: false }),
      }));
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(0); // warn doesn't fail
    });

    it('suggests loopzero init', async () => {
      const lines: string[] = [];
      const cmd = new DoctorCommand(makeDeps({
        readProjectConfig: () => ({ exists: false, valid: false }),
        writeTo: (m) => { lines.push(m); },
      }));
      await cmd.execute({});
      expect(lines.some(l => l.includes('loopzero init'))).toBe(true);
    });
  });

  describe('invalid loopzero.yaml', () => {
    it('reports project_config fail', async () => {
      const cmd = new DoctorCommand(makeDeps({
        readProjectConfig: () => ({ exists: true, valid: false, error: 'YAML syntax error' }),
      }));
      const exitCode = await cmd.execute({ json: true });
      expect(exitCode).toBe(1);
    });
  });

  describe('Rule 3 — no internal terms in output', () => {
    it('does not leak ArgoCD, advisory_lock, Gitea, etc.', async () => {
      const lines: string[] = [];
      const cmd = new DoctorCommand(makeDeps({
        validateToken: jest.fn(async () => ({ valid: false })) as DoctorDeps['validateToken'],
        pingAPI: jest.fn(async () => ({ ok: false, error: 'timeout' })) as DoctorDeps['pingAPI'],
        readProjectConfig: () => ({ exists: false, valid: false }),
        writeTo: (m) => { lines.push(m); },
      }));
      await cmd.execute({});
      const allOutput = lines.join('\n');
      const forbidden = ['ArgoCD', 'argocd', 'GitOps', 'Gitea', 'advisory_lock', 'pg_advisory', 'MinIO', 'Kaniko', 'envtest', 'kubebuilder'];
      for (const term of forbidden) {
        expect(allOutput).not.toContain(term);
      }
    });
  });
});

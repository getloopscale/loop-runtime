// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Audit gap M08 #18 / §6.C.4b — regression suite for the Loki cross-cluster
// push URL wiring.
//
// Path A of `concepts/observability/loki-cross-cluster-design.md`:
//   - `infra/helm/promtail/values.yaml.gotmpl` reads
//     `.Environment.Values.loki.pushUrl` at helmfile render time.
//   - `infra/helmfile/environments/*.yaml` MUST declare `loki.pushUrl` per
//     environment so the templated value is non-empty in production.
//   - A missing key would silently fall back to the k3d-local default,
//     re-introducing the cross-VPC silent-loss bug the audit flagged.
//
// These tests are pure YAML parsing — no helmfile binary required, no
// kubeconfig, no cloud calls. They guard the contract from drift.

import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';
import * as yaml from 'js-yaml';

const REPO_ROOT = path.resolve(__dirname, '../../../../');
const ENVS_DIR = path.join(REPO_ROOT, 'infra/helmfile/environments');
const PROMTAIL_TMPL = path.join(REPO_ROOT, 'infra/helm/promtail/values.yaml.gotmpl');
const PROMTAIL_OLD_YAML = path.join(REPO_ROOT, 'infra/helm/promtail/values.yaml');

interface LokiBlock {
  pushUrl?: string;
  maxGlobalStreamsPerUser?: number;
}

interface EnvDoc {
  loki?: LokiBlock;
}

function loadEnv(name: string): EnvDoc {
  const raw = fs.readFileSync(path.join(ENVS_DIR, `${name}.yaml`), 'utf-8');
  return yaml.load(raw) as EnvDoc;
}

describe('Loki cross-cluster push URL wiring (audit M08 #18 / §6.C.4b)', () => {
  describe('promtail values template', () => {
    it('exists as values.yaml.gotmpl (Path A conversion landed)', () => {
      expect(fs.existsSync(PROMTAIL_TMPL)).toBe(true);
    });

    it('the legacy hardcoded values.yaml was deleted (no dual SoT)', () => {
      // Helmfile would prefer .yaml over .gotmpl in some configurations,
      // and having both would mean changes to the gotmpl never take effect.
      // The legacy file MUST be gone.
      expect(fs.existsSync(PROMTAIL_OLD_YAML)).toBe(false);
    });

    it('reads loki.pushUrl from the helmfile environment', () => {
      const src = fs.readFileSync(PROMTAIL_TMPL, 'utf-8');
      // Verify the template references `.Environment.Values` and the
      // `loki.pushUrl` path — drift detection for someone replacing the
      // lookup with a different key.
      expect(src).toContain('.Environment.Values');
      expect(src).toContain('loki.pushUrl');
    });

    it('falls back to the k3d-local default when loki.pushUrl is absent', () => {
      // The default preserves backward-compat for the standalone harness
      // that may not render through helmfile (direct helm install).
      const src = fs.readFileSync(PROMTAIL_TMPL, 'utf-8');
      expect(src).toContain('http://172.18.0.3:30094/loki/api/v1/push');
    });
  });

  describe('per-environment loki.pushUrl declarations', () => {
    it('dev points at the k3d mgmt NodePort', () => {
      const env = loadEnv('dev');
      expect(env.loki?.pushUrl).toBe('http://172.18.0.3:30094/loki/api/v1/push');
    });

    it('standalone points at the k3d mgmt NodePort', () => {
      const env = loadEnv('standalone');
      expect(env.loki?.pushUrl).toBe('http://172.18.0.3:30094/loki/api/v1/push');
    });

    it('standalone-multi points at the k3d mgmt NodePort', () => {
      const env = loadEnv('standalone-multi');
      expect(env.loki?.pushUrl).toBe('http://172.18.0.3:30094/loki/api/v1/push');
    });

    it('drill points at the DOKS internal LB for the drill cluster', () => {
      const env = loadEnv('drill');
      expect(env.loki?.pushUrl).toBe(
        'http://loki-internal.drill.loopzero-drill.example:3100/loki/api/v1/push',
      );
    });

    it('prod points at the DOKS internal LB (NOT k3d default)', () => {
      const env = loadEnv('prod');
      // Critical assertion: prod MUST NOT carry the k3d-local IP.
      // That was the entire point of §6.C.4b — silent cross-VPC loss
      // was caused by the hardcoded local IP leaking to prod.
      expect(env.loki?.pushUrl).not.toContain('172.18.0.3');
      expect(env.loki?.pushUrl).toMatch(/loki.*\.internal/);
    });
  });

  describe('cross-VPC silent-loss invariant', () => {
    it('NO production-tier environment carries the k3d NodePort IP', () => {
      // Inverse of the per-env tests: parse every environment file and
      // assert that any env whose name contains "prod" or "drill" does
      // NOT have the k3d default URL. Catches a regression where a copy
      // paste accidentally leaks the local IP to a cloud env.
      const files = fs.readdirSync(ENVS_DIR).filter((f) => f.endsWith('.yaml'));
      for (const file of files) {
        const base = path.basename(file, '.yaml');
        if (!/prod|drill/.test(base)) continue;
        const env = loadEnv(base);
        expect(env.loki?.pushUrl).not.toContain('172.18.0.3');
      }
    });

    it('every environment file declares a pushUrl (no implicit fallback in prod)', () => {
      // The template falls back to the k3d default when the key is absent.
      // For prod/drill, an absent key would silently route to 172.18.0.3
      // (a private k3d IP that does not exist cross-VPC). The contract is:
      // EVERY environment file declares pushUrl explicitly.
      const files = fs.readdirSync(ENVS_DIR).filter((f) => f.endsWith('.yaml'));
      for (const file of files) {
        const base = path.basename(file, '.yaml');
        const env = loadEnv(base);
        expect(env.loki?.pushUrl, `${base}.yaml missing loki.pushUrl`).toBeDefined();
        expect(typeof env.loki?.pushUrl, `${base}.yaml pushUrl not a string`).toBe('string');
        expect(env.loki?.pushUrl, `${base}.yaml pushUrl empty`).not.toBe('');
      }
    });
  });
});

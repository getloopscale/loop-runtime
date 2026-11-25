// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Beta Logs Gate (audit M08) — regression suite for the promtail
// scrape config. Asserts that:
//   1. Tenant runtime pods are scraped (the pod's own loopzero.io/tenant label)
//   2. LoopZero control-plane pods are scraped (loopzero.io/component label)
//   3. JSON correlation_id is promoted to a Loki label
//   4. Argo Workflow scrape is preserved
//   5. Cardinality-safe labels (no per-pod unique values escape to Loki labels)

import { describe, it, expect } from 'vitest';
import * as fs from 'fs';
import * as path from 'path';

const REPO_ROOT = path.resolve(__dirname, '../../../../');
const PROMTAIL_TMPL = path.join(REPO_ROOT, 'infra/helm/promtail/values.yaml.gotmpl');

describe('Promtail scrape config (Beta Logs Gate)', () => {
  const src = fs.readFileSync(PROMTAIL_TMPL, 'utf-8');

  it('scrapes Argo Workflow pods (build cluster) — preserved from pre-Beta config', () => {
    expect(src).toContain('job_name: argo-workflow-pods');
    expect(src).toContain('__meta_kubernetes_pod_label_workflows_argoproj_io_workflow');
  });

  it('scrapes LoopZero tenant runtime + control-plane pods (Beta gap fix)', () => {
    // The whole point of the Beta gate fix: tenant pods WERE not
    // being scraped. The new loopzero-pods job has two membership
    // filters that OR'd via __tmp_keep idiom.
    expect(src).toContain('job_name: loopzero-pods');
    // The pod's own label: pod service discovery gives no namespace label, so a
    // rule on `__meta_kubernetes_namespace_label_*` drops every tenant pod
    // (getloopscale/loopzero#110). The behaviour is asserted over the rendered
    // job in infra/tests/charts/promtail_tenant_pods_test.go.
    expect(src).not.toContain('__meta_kubernetes_namespace_label_');
    expect(src).toContain('source_labels: [__meta_kubernetes_pod_label_loopzero_io_tenant]');
    expect(src).toContain('__meta_kubernetes_pod_label_loopzero_io_component');
  });

  it('extracts correlation_id from JSON log lines and promotes to Loki label', () => {
    // Without this, `logcli query '{correlation_id="X"}'` is impossible
    // and cross-system grep is gone. The pipeline_stages json + labels
    // sequence is canonical.
    expect(src).toContain('correlation_id: correlation_id');
    // Both jobs (workflow + loopzero-pods) must emit the label.
    const correlationLabels = (src.match(/correlation_id:\s*$/gm) || []).length;
    expect(correlationLabels).toBeGreaterThanOrEqual(2);
  });

  it('also extracts level + msg for the loopzero-pods scrape (operator log hygiene)', () => {
    // Level is a low-cardinality enum (debug/info/warn/error) — safe
    // as a Loki label. msg is NOT promoted to a label (cardinality
    // would explode). The expressions stage extracts both; only level
    // makes it to `labels:` block.
    expect(src).toContain('level: level');
    expect(src).toContain('msg: msg');
  });

  it('promotes loopzero.io/app + tenant + environment + component as Loki labels', () => {
    // These are the labels `loopzero logs --runtime --app <name>` will
    // filter on. Without app/tenant/environment labels, the runtime
    // log endpoint can only scope by namespace (= tenant+env) — the
    // exact 60% gap the audit flagged.
    expect(src).toContain('target_label: app');
    expect(src).toContain('target_label: loopzero_tenant');
    expect(src).toContain('target_label: loopzero_project');
    expect(src).toContain('target_label: loopzero_environment');
    expect(src).toContain('target_label: loopzero_component');
  });

  it('does NOT promote step or pod_name as labels (cardinality cap)', () => {
    // Per CHG-002b 2026-04-27 cardinality fix: step + pod each
    // carried per-execution unique values, multiplying active streams
    // ~5x. The Beta gate fix preserves that cardinality discipline —
    // adding correlation_id is OK because LIVE traffic produces
    // bounded unique values within Loki's retention window.
    const hasStep = /target_label:\s*step\s*$/m.test(src);
    const hasPod = /target_label:\s*pod\s*$/m.test(src);
    expect(hasStep).toBe(false);
    expect(hasPod).toBe(false);
  });
});

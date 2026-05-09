// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-PARITY / T3.1 + T5.1 — `getPlatformStatus` extraído de
// installer/components.ts (deletado por dead-code Rule 3 violations).
// Único módulo neutro de status; sem strings com termos internos.
//
// 2026-04-30 (T5.1 run 5 hardening — Achados #34/#35/#36/#37):
//   • Selector mudou de `app.kubernetes.io/name` para `app` — charts loopzero-*
//     aplicam apenas a label de compatibilidade legada `app: X` (templates/
//     deployment.yaml em loopzero-api, loopzero-controller, tenant-controller,
//     loopzero-cloud). A query antiga retornava `items[]` vazio → jsonpath
//     erro → todos os componentes reportavam NotInstalled mesmo com
//     helmfile sync GREEN.
//   • Lista de componentes pinada nos 4 charts reais; entrada
//     `loopzero-tenant-controller` (chart real é `tenant-controller`) e
//     `loopzero-dashboard` (chart inexistente) foram removidas.
//   • Sentinel `releaseExists('loopzero', 'loopzero-system')` removido — nunca
//     existiu uma release "loopzero". Phase agora é derivada inteiramente do
//     estado dos pods (allRunning → Ready, anyFailed → Failed,
//     none reachable → NotInstalled, demais → Degraded).
//   • Pinned por tests/unit/getPlatformStatus-charts.test.ts.

import { execSync } from 'child_process';
import { ComponentStatus, PlatformStatus } from './types';

const CHART_VERSION = '0.2.0';

/**
 * Charts gerenciados pelo LoopZero (em ordem de install). Pinado contra
 * filesystem por getPlatformStatus-charts.test.ts.
 *
 * Post operator-split (CHG-2026-04-30-INFRA-PARITY T1.5): the legacy
 * `loopzero-controller` + `tenant-controller` charts were DELETED. Operators
 * now ship as 4 split binaries via `charts/loopzero-{tenant,app,preview,
 * platform}-operator` (OCI-published, NOT in infra/helm/). Bootstrap
 * charts in infra/helm/ are limited to loopzero-api + loopzero-cloud + their
 * dependencies.
 *
 * LOOPZERO_COMPONENTS now lists ONLY the bootstrap charts that physically
 * exist in infra/helm/ — the test pin is back to GREEN.
 */
export const LOOPZERO_COMPONENTS = [
  'loopzero-api',
  'loopzero-cloud',
] as const;

/**
 * Returns the current platform installation status by querying live K8s
 * state. Used by `loopzero platform status` and platform install Step 6
 * (validation).
 *
 * Output is product-language only (Rule 3): no "ArgoCD"/"Gitea"/"MinIO"
 * leaks. Component names map directly to user-facing chart releases.
 */
export function getPlatformStatus(): PlatformStatus {
  const components: ComponentStatus[] = [];

  for (const name of LOOPZERO_COMPONENTS) {
    try {
      const output = execSync(
        `kubectl get deploy -n loopzero-system -l app=${name} -o jsonpath="{.items[0].status.readyReplicas}/{.items[0].spec.replicas}"`,
        { encoding: 'utf-8', timeout: 5_000 },
      ).trim();
      components.push({
        name,
        status: output.startsWith('0/') || !output ? 'Pending' : 'Running',
        replicas: output || '0/0',
      });
    } catch {
      components.push({ name, status: 'NotInstalled' });
    }
  }

  let platformDefinitions = 0;
  try {
    platformDefinitions = parseInt(
      execSync('kubectl get crd -o name 2>/dev/null | grep loopzero.io | wc -l', { encoding: 'utf-8' }).trim(),
      10,
    );
  } catch {
    // ignore
  }

  // Achado #35: phase derivada do estado dos pods, não de uma release
  // sentinel. O install instala N charts (loopzero-api, loopzero-controller, ...);
  // não há release agregadora "loopzero" que sirva de check.
  const allRunning = components.length > 0 && components.every((c) => c.status === 'Running');
  const allNotInstalled = components.every((c) => c.status === 'NotInstalled');
  const anyFailed = components.some((c) => c.status === 'Failed');

  let phase: PlatformStatus['phase'];
  if (allNotInstalled) {
    phase = 'NotInstalled';
  } else if (anyFailed) {
    phase = 'Failed';
  } else if (allRunning) {
    phase = 'Ready';
  } else {
    phase = 'Degraded';
  }

  const installed = !allNotInstalled;

  return {
    phase,
    version: CHART_VERSION,
    components,
    platformDefinitions,
    endpoints: {
      api: installed ? `https://api.${process.env.LOOPZERO_DOMAIN || 'localhost'}` : undefined,
      dashboard: installed ? `https://cloud.${process.env.LOOPZERO_DOMAIN || 'localhost'}` : undefined,
    },
  };
}

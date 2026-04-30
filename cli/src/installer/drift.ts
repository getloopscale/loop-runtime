// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-PARITY / T2.1 — drift detection between Git and runtime.
//
// Reads sync-engine state via `kubectl get applications -A -o json` and
// returns a structured list of apps where the runtime diverges from Git.
// Used by `loopzero platform status` to decide exit code 5 (drift detected).

import { execSync } from 'child_process';

export interface AppDrift {
  name: string;
  namespace: string;
  sync: string;
  health: string;
}

export interface DriftReport {
  total: number;
  synced: number;
  drift: AppDrift[];
}

/**
 * Collect drift from the sync-engine. An app is "in drift" when sync status
 * is anything other than `Synced` — `OutOfSync` (runtime diverged from Git),
 * `Unknown` (engine can't evaluate), or any other state.
 *
 * Returns an empty drift array when no sync-engine is installed (treat as
 * "no drift" — caller decides if that's a fail or pass).
 */
export function detectDrift(): DriftReport {
  let raw: string;
  try {
    raw = execSync('kubectl get applications -A -o json 2>/dev/null', {
      encoding: 'utf-8',
      timeout: 10_000,
    });
  } catch {
    return { total: 0, synced: 0, drift: [] };
  }

  let parsed: { items?: unknown[] };
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { total: 0, synced: 0, drift: [] };
  }

  const items = (parsed.items ?? []) as Array<{
    metadata?: { name?: string; namespace?: string };
    status?: { sync?: { status?: string }; health?: { status?: string } };
  }>;

  let synced = 0;
  const drift: AppDrift[] = [];
  for (const it of items) {
    const sync = it.status?.sync?.status ?? 'Unknown';
    const health = it.status?.health?.status ?? 'Unknown';
    const name = it.metadata?.name ?? '?';
    const namespace = it.metadata?.namespace ?? '?';
    if (sync === 'Synced') {
      synced++;
    } else {
      drift.push({ name, namespace, sync, health });
    }
  }

  return { total: items.length, synced, drift };
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { LoopZeroYamlApp, BuildStrategy } from './loopzero-yaml';

/**
 * AppUnit is a fully resolved app — all fields required, ready for build plan generation.
 * This is the normalization boundary: before AppUnit, data comes from parsing and heuristics.
 * After AppUnit, everything is deterministic.
 */
export type RenderMode = 'static' | 'server';

export interface AppAutoscaling {
  min: number;       // default: 1
  max: number;       // default: 3
  targetCPU: number; // default: 80
}

export interface AppUnit {
  name: string;
  type: 'server' | 'worker' | 'frontend';
  path: string;
  framework: string;
  port: number | null;
  build: BuildStrategy;
  dockerfile: string | null;          // resolved Dockerfile path (relative to app path)
  rendering: RenderMode;              // v2: "static" (CDN) or "server" (container). MEET-009.
  env: Record<string, string>;
  secrets: string[];
  replicas: number;                   // desired replica count (default: 1)
  autoscaling: AppAutoscaling | null; // HPA configuration (null = no autoscaling)
}

// Plan B v3.1 T-B.1 (2026-06-05) — `FRONTEND_FRAMEWORKS` array removed
// post LoopZeroKit-only cutover. Apps now declare `type` explicitly em
// `.loopzero/services.json` (or via the legacy `services_json` form-field
// path during transition). The framework-string heuristic no longer
// participates em type inference; absent type defaults to "server"
// (frontends are explicit em services manifest per ADR D5).

/**
 * normalizeApp resolves a raw LoopZeroYamlApp into a fully populated AppUnit.
 *
 * Defaults applied:
 *   - type: inferred from framework (frontend frameworks → "frontend", else "server")
 *   - build: "buildpack" when framework is recognized, "custom" when framework is "custom"
 *   - dockerfile: null unless build is "dockerfile" (then defaults to "Dockerfile")
 *   - rendering: explicit from loopzero.yaml, or "server" as safe default (MEET-009)
 *   - env: {} when absent
 *   - secrets: [] when absent
 */
export function normalizeApp(name: string, raw: LoopZeroYamlApp): AppUnit {
  // Resolve type
  let type: AppUnit['type'];
  if (raw.type === 'worker' || raw.type === 'frontend' || raw.type === 'server') {
    type = raw.type;
  } else {
    // Plan B v3.1 T-B.1 — framework-string heuristic removed; default "server"
    // (frontends declared explicitly em services.json post LoopZeroKit-only cutover).
    type = 'server';
  }

  // Resolve build strategy
  let build: BuildStrategy;
  if (raw.build) {
    build = raw.build;
  } else if (raw.framework === 'custom' || raw.framework === 'unknown') {
    build = 'dockerfile';
  } else {
    build = 'buildpack';
  }

  // Resolve dockerfile
  let dockerfile: string | null = null;
  if (build === 'dockerfile') {
    dockerfile = raw.dockerfile ?? 'Dockerfile';
  }

  // Resolve rendering mode (MEET-2026-03-26-009, updated 2026-04-08)
  // Frontends are ALWAYS static (zero pods, CDN delivery). No exceptions.
  // Server/worker are ALWAYS "server" (container pods).
  let rendering: RenderMode;
  if (type === 'frontend') {
    rendering = 'static'; // Always static — CDN delivery, zero pods
  } else {
    rendering = 'server'; // Always server — container pod
  }

  // Resolve replicas (default: 1)
  const replicas = raw.replicas !== undefined && raw.replicas >= 1 ? raw.replicas : 1;

  // Resolve autoscaling
  let autoscaling: AppAutoscaling | null = null;
  if (raw.autoscaling) {
    autoscaling = {
      min: raw.autoscaling.min ?? 1,
      max: raw.autoscaling.max ?? 3,
      targetCPU: raw.autoscaling.targetCPU ?? 80,
    };
  }

  return {
    name,
    type,
    path: raw.path || '.',
    framework: raw.framework || 'custom',
    port: (type === 'server' || type === 'frontend') ? (raw.port ?? (type === 'server' ? 8080 : 80)) : null,
    build,
    dockerfile,
    rendering,
    env: raw.env ?? {},
    secrets: raw.secrets ?? [],
    replicas,
    autoscaling,
  };
}

/**
 * normalizeApps converts all apps from a LoopZeroYaml into AppUnit[].
 * Optionally filters by selected app names.
 */
export function normalizeApps(
  apps: Record<string, LoopZeroYamlApp>,
  selectedNames?: string[],
): AppUnit[] {
  const names = selectedNames ?? Object.keys(apps);
  return names.map((name) => normalizeApp(name, apps[name]));
}

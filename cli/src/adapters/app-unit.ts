// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { LoopZeroYamlApp, BuildStrategy } from './loopzero-yaml';

/**
 * AppUnit is a fully resolved app — all fields required, ready for build plan generation.
 * This is the normalization boundary: before AppUnit, data comes from parsing and heuristics.
 * After AppUnit, everything is deterministic.
 */
export interface AppUnit {
  name: string;
  type: 'server' | 'worker' | 'frontend';
  path: string;
  framework: string;
  port: number | null;
  build: BuildStrategy;
  dockerfile: string | null;          // resolved Dockerfile path (relative to app path)
  env: Record<string, string>;
  secrets: string[];
}

const FRONTEND_FRAMEWORKS = ['nextjs', 'next', 'vite', 'remix', 'react', 'vue', 'svelte', 'astro'];

/**
 * normalizeApp resolves a raw LoopZeroYamlApp into a fully populated AppUnit.
 *
 * Defaults applied:
 *   - type: inferred from framework (frontend frameworks → "frontend", else "server")
 *   - build: "buildpack" when framework is recognized, "custom" when framework is "custom"
 *   - dockerfile: null unless build is "dockerfile" (then defaults to "Dockerfile")
 *   - env: {} when absent
 *   - secrets: [] when absent
 */
export function normalizeApp(name: string, raw: LoopZeroYamlApp): AppUnit {
  // Resolve type
  let type: AppUnit['type'];
  if (raw.type === 'worker' || raw.type === 'frontend' || raw.type === 'server') {
    type = raw.type;
  } else if (FRONTEND_FRAMEWORKS.includes(raw.framework)) {
    type = 'frontend';
  } else {
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

  return {
    name,
    type,
    path: raw.path || '.',
    framework: raw.framework || 'custom',
    port: type === 'server' ? (raw.port ?? 8080) : null,
    build,
    dockerfile,
    env: raw.env ?? {},
    secrets: raw.secrets ?? [],
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

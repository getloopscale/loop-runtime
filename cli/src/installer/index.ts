// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-30-INFRA-PARITY / T3.1 — installer/ é exclusivamente Helmfile L2 +
// Pulumi L1 + status query. Os antigos `bootstrap.ts`, `components.ts`,
// `prerequisites.ts` (legacy pré-Helmfile) foram deletados — continham strings
// com termos internos (Rule 3) que vazariam para o bundle final do CLI.

export { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
export { validateCluster } from './validator';

// Status query — neutro, sem termos internos.
export { getPlatformStatus } from './status';

// Helmfile-based runners (NEW — primary mechanism)
export {
  helmfileSync,
  helmfileDiff,
  helmfileDestroy,
  helmfileLint,
  helmfileList,
  ensureHelmfileAvailable,
  resolveHelmfilePath,
} from './helmfile-runner';
export type { HelmfileEnv, HelmfileResult } from './helmfile-runner';

export {
  pulumiUp,
  pulumiPreview,
  pulumiDestroy,
  pulumiOutput,
  pulumiOutputs,
  ensurePulumiAvailable,
  resolvePulumiProjectDir,
  resolveProjectRoot,
} from './pulumi-runner';
export type { PulumiContext, PulumiResult } from './pulumi-runner';

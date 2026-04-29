// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / T3.1 + T3.2.
//
// Most installer modules (bootstrap.ts, components.ts, prerequisites.ts) are
// LEGACY — replaced by Helmfile L2. They remain in the tree for backward
// compatibility while we migrate downstream test suites. New code should use
// `helmfile-runner` and `pulumi-runner`.

export { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
export { validateCluster } from './validator';

// Status-only API still used by `loopzero platform status`.
export { getPlatformStatus } from './components';

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

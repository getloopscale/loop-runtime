// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
export { validateCluster } from './validator';
export { installPrerequisites } from './prerequisites';
export {
  runMigrations,
  bootstrapGitea,
  bootstrapArgoCD,
  generateSecret,
  createKubeconfigs,
  createHydraClient,
  seedAdminUser,
  createAuthDatabases,
} from './bootstrap';
export {
  ensureImagePullSecret,
  installCRDs,
  installPlatform,
  upgradePlatform,
  uninstallPlatform,
  validateInstallation,
  waitForPods,
  getPlatformStatus,
} from './components';

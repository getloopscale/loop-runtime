// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export { PlatformConfig, PlatformStatus, ComponentStatus } from './types';
export { validateCluster } from './validator';
export {
  installPrerequisites,
  installCorePrerequisites,
  installAuthPrerequisites,
} from './prerequisites';
export type { BootstrapTokens } from './bootstrap';
export {
  runMigrations,
  bootstrapGitea,
  bootstrapArgoCD,
  patchArgocdInsecure,
  createArgoCDRepoCredentials,
  extractMinioCredentials,
  extractPostgresCredentials,
  generateSecret,
  createKubeconfigs,
  createHydraClient,
  createMinioCredentialsSecret,
  seedAdminUser,
  createAuthDatabases,
  createClusterIssuer,
  ensureMinioBucket,
  grantTableOwnership,
  createBuildSecrets,
  runMigrationsViaCnpgPod,
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

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { CommandDeps } from './types';
import { platformInstall, platformStatus, platformUpgrade, platformUninstall } from '../commands/platform';

export function registerPlatformCommands(deps: CommandDeps): void {
  const { program, writeTo, writeError } = deps;

  const platform = program
    .command('platform')
    .description('Manage LoopZero platform installation (self-hosted)');

  platform
    .command('install')
    .description('Install the LoopZero platform on the current Kubernetes cluster')
    .requiredOption('-f, --values <file>', 'Path to customer values YAML file')
    .option('--profile <profile>', 'Sizing profile: minimal or production', 'minimal')
    .action(async (opts) => {
      const code = await platformInstall({ writeTo, writeError }, opts.values, opts.profile);
      process.exit(code);
    });

  platform
    .command('status')
    .description('Show platform installation status')
    .action(async () => {
      const code = await platformStatus({ writeTo, writeError });
      process.exit(code);
    });

  platform
    .command('upgrade')
    .description('Upgrade the platform to a new version')
    .requiredOption('--version <version>', 'Target chart version')
    .option('-f, --values <file>', 'Path to customer values YAML file')
    .action(async (opts) => {
      const code = await platformUpgrade({ writeTo, writeError }, opts.version, opts.values);
      process.exit(code);
    });

  platform
    .command('uninstall')
    .description('Uninstall the LoopZero platform (preserves CRDs and data)')
    .action(async () => {
      const code = await platformUninstall({ writeTo, writeError });
      process.exit(code);
    });
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// CHG-2026-04-28-LAYERED-BOOTSTRAP / ADR D11 — `loopzero platform install` is the
// SOLE installation command. Optional `--provider` provisions cloud infra
// (Pulumi L1) before running Helmfile L2.

import { CommandDeps } from './types';
import {
  platformInstall,
  platformStatus,
  platformUpgrade,
  platformUninstall,
  platformSecretsBackup,
  platformSecretsRestore,
  platformSecretsList,
} from '../commands/platform';

export function registerPlatformCommands(deps: CommandDeps): void {
  const { program, writeTo, writeError } = deps;

  const platform = program
    .command('platform')
    .description('Manage LoopZero platform installation');

  platform
    .command('install')
    .description('Install the LoopZero platform (self-hosting or with provisioned infra)')
    .requiredOption('-f, --values <file>', 'Path to customer values YAML file')
    .option('--profile <profile>', 'Sizing profile: minimal or production', 'minimal')
    .option('--provider <provider>', 'Provision cloud infra: digitalocean | aws | k3d')
    .option('--dry-run', 'Show plan via helmfile diff without applying changes', false)
    .option('--skip-infra', 'Skip Pulumi L1 (use existing infrastructure outputs)', false)
    .action(async (opts) => {
      const code = await platformInstall({ writeTo, writeError }, {
        valuesFile: opts.values,
        profile: opts.profile,
        provider: opts.provider,
        dryRun: opts.dryRun,
        skipInfra: opts.skipInfra,
      });
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
    .description('Upgrade the platform via helmfile sync (idempotent)')
    .option('--version <version>', 'Target chart version (no-op — versions are pinned in helmfiles)', '')
    .option('-f, --values <file>', 'Path to customer values YAML file')
    .action(async (opts) => {
      const code = await platformUpgrade({ writeTo, writeError }, opts.version, opts.values);
      process.exit(code);
    });

  platform
    .command('uninstall')
    .description('Uninstall the platform (helmfile destroy + optional pulumi destroy)')
    .option('--provider <provider>', 'Cloud provider used during install (for L1 destroy)')
    .option('--delete-infra', 'Also run pulumi destroy on cloud infra', false)
    .action(async (opts) => {
      const code = await platformUninstall({ writeTo, writeError }, {
        provider: opts.provider,
        deleteInfra: opts.deleteInfra,
      });
      process.exit(code);
    });

  // CHG-2026-04-28-LAYERED-BOOTSTRAP iter 14 — Secret lifecycle commands.
  // Auto-backup happens on every `helmfile sync`. These commands are for
  // manual on-demand backup/restore (before destructive ops, after recovery).
  const secrets = platform
    .command('secrets')
    .description('Manage LoopZero platform Secret backups (~/.loopzero/backups/)');

  secrets
    .command('backup')
    .description('Back up all LoopZero-managed Secrets to a YAML file (default: ~/.loopzero/backups/secrets-manual-<ts>.yaml)')
    .option('-o, --output <file>', 'Output path (default: ~/.loopzero/backups/secrets-manual-<ts>.yaml)')
    .action(async (opts) => {
      const code = await platformSecretsBackup({ writeTo, writeError }, opts.output);
      process.exit(code);
    });

  secrets
    .command('restore')
    .description('Restore LoopZero Secrets from a backup file (e.g. after pulumi destroy or cluster recreation)')
    .requiredOption('-f, --file <file>', 'Backup YAML file produced by `loopzero platform secrets backup`')
    .action(async (opts) => {
      const code = await platformSecretsRestore({ writeTo, writeError }, opts.file);
      process.exit(code);
    });

  secrets
    .command('list')
    .description('List available Secret backups in ~/.loopzero/backups/')
    .action(async () => {
      const code = await platformSecretsList({ writeTo, writeError });
      process.exit(code);
    });
}

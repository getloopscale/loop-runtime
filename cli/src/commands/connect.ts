// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// loopzero connect — Link a GitHub repository to a LoopZero project for auto-deploy.
// loopzero disconnect — Unlink.

import { ApiError } from '../api/client';

export interface ConnectDeps {
  getToken: () => string | null;
  getGitRemote: () => string | null;
  getProjectName: () => string | null;
  connectRepo: (projectName: string, body: { repo: string; installation_id: number; branch: string }) => Promise<ConnectResponse>;
  disconnectRepo: (projectName: string) => Promise<{ ok: boolean }>;
  getConnection: (projectName: string) => Promise<ConnectionInfo | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

interface ConnectResponse {
  ok: boolean;
  project: string;
  repo: string;
  branch: string;
  auto_deploy: boolean;
}

interface ConnectionInfo {
  project: string;
  repo: string;
  branch: string;
  auto_deploy: boolean;
  provider: string;
  connected_at: string;
}

export interface ConnectOptions {
  repo?: string;
  branch?: string;
  installationId?: number;
  json?: boolean;
}

function exitWithError(
  deps: Pick<ConnectDeps, 'writeTo' | 'writeError'>,
  json: boolean | undefined,
  message: string,
  exitCode: number,
  code?: string,
): number {
  if (json) {
    const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
    if (code) payload.code = code;
    deps.writeTo(JSON.stringify(payload));
  } else {
    deps.writeError(message);
  }
  return exitCode;
}

// parseGitHubRepo extracts "owner/repo" from a git remote URL.
// Supports: git@github.com:owner/repo.git, https://github.com/owner/repo.git
export function parseGitHubRepo(remoteUrl: string): string | null {
  // SSH: git@github.com:owner/repo.git
  const sshMatch = remoteUrl.match(/github\.com[:/]([^/]+\/[^/]+?)(?:\.git)?$/);
  if (sshMatch) return sshMatch[1];

  // HTTPS: https://github.com/owner/repo.git
  const httpsMatch = remoteUrl.match(/github\.com\/([^/]+\/[^/]+?)(?:\.git)?$/);
  if (httpsMatch) return httpsMatch[1];

  return null;
}

export class ConnectCommand {
  constructor(private readonly deps: ConnectDeps) {}

  async execute(opts: ConnectOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    const projectName = this.deps.getProjectName();
    if (!projectName) {
      return exitWithError(this.deps, opts.json, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    // Detect repo from git remote if not provided
    let repo = opts.repo;
    if (!repo) {
      const remote = this.deps.getGitRemote();
      if (remote) {
        repo = parseGitHubRepo(remote) ?? undefined;
      }
    }
    if (!repo) {
      return exitWithError(this.deps, opts.json, 'Could not detect GitHub repository. Use --repo owner/repo', 1, 'MISSING_REPO');
    }

    const branch = opts.branch || 'main';
    const installationId = opts.installationId || 0;

    // If no installation_id, tell user to install the app
    if (!installationId) {
      // Try to connect anyway — the API will use the installation from the webhook
      // The user must have installed the GitHub App on their repo first
    }

    try {
      const result = await this.deps.connectRepo(projectName, {
        repo,
        installation_id: installationId,
        branch,
      });

      if (opts.json) {
        this.deps.writeTo(JSON.stringify(result));
        return 0;
      }

      this.deps.writeTo(`✓ Connected ${repo} → project ${result.project}`);
      this.deps.writeTo(`  Auto-deploy: ${result.auto_deploy ? 'enabled' : 'disabled'} (push to ${result.branch} → production)`);
      this.deps.writeTo('');
      this.deps.writeTo('  Every push to the default branch will deploy automatically.');
      this.deps.writeTo('  Make sure the LoopZero GitHub App is installed on your repository:');
      this.deps.writeTo('    https://github.com/apps/loopzero-platform');
      return 0;
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.statusCode === 409) {
          return exitWithError(this.deps, opts.json, `Repository ${repo} is already connected to another project.`, 3, 'REPO_ALREADY_LINKED');
        }
        return exitWithError(this.deps, opts.json, `Failed to connect: ${err.message}`, 3, err.code);
      }
      return exitWithError(this.deps, opts.json, `Failed to connect: ${err instanceof Error ? err.message : 'unknown error'}`, 3, 'UNEXPECTED_ERROR');
    }
  }
}

export interface DisconnectOptions {
  json?: boolean;
}

export class DisconnectCommand {
  constructor(private readonly deps: ConnectDeps) {}

  async execute(opts: DisconnectOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts.json, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    const projectName = this.deps.getProjectName();
    if (!projectName) {
      return exitWithError(this.deps, opts.json, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    try {
      await this.deps.disconnectRepo(projectName);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ok: true, project: projectName }));
        return 0;
      }

      this.deps.writeTo(`✓ Disconnected. Auto-deploy disabled for ${projectName}.`);
      return 0;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 404) {
        return exitWithError(this.deps, opts.json, 'No repository connected to this project.', 1, 'NOT_CONNECTED');
      }
      return exitWithError(this.deps, opts.json, `Failed to disconnect: ${err instanceof Error ? err.message : 'unknown error'}`, 3, 'UNEXPECTED_ERROR');
    }
  }
}

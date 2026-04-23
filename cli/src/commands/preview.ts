// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';

export interface PreviewListItem {
  id: string;
  pr_number?: number;
  branch: string;
  url?: string;
  status: string;
  created_at: string;
}

export interface PreviewListApiResponse {
  previews: PreviewListItem[];
}

export interface PreviewInspectApiResponse {
  id: string;
  pr_number?: number;
  branch: string;
  url?: string;
  status: string;
  created_at: string;
  updated_at: string;
  apps?: Array<{ name: string; status: string; url?: string }>;
  ttl_expires_at?: string;
}

export interface PreviewDeleteApiResponse {
  id: string;
  status: string;
}

export interface PreviewDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  listPreviews: (project: string) => Promise<PreviewListApiResponse>;
  inspectPreview: (project: string, id: string) => Promise<PreviewInspectApiResponse>;
  deletePreview: (project: string, id: string) => Promise<PreviewDeleteApiResponse>;
  confirm?: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI?: boolean;
}

export interface PreviewListOptions {
  json?: boolean;
}

export interface PreviewInspectOptions {
  json?: boolean;
}

export interface PreviewDeleteOptions {
  json?: boolean;
  yes?: boolean;
}

/**
 * PreviewCommand manages preview environments.
 *
 * Subcommands:
 *   list    — list preview environments for the current project
 *   inspect — show details of a specific preview environment
 *   delete  — tear down a preview environment
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (missing config, bad arguments)
 *   3 — operation failed (not found, API error)
 *   4 — auth error
 */
export class PreviewListCommand {
  private readonly deps: PreviewDeps;

  constructor(deps: PreviewDeps) {
    this.deps = deps;
  }

  async execute(opts: PreviewListOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const project = yamlResult.content.project;

    // 3. Call API
    let response: PreviewListApiResponse;
    try {
      response = await this.deps.listPreviews(project);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        previews: response.previews,
        count: response.previews.length,
      }));
      return 0;
    }

    if (response.previews.length === 0) {
      this.deps.writeTo('No preview environments found.');
      this.deps.writeTo('Preview environments are created automatically when you open a pull request.');
      return 0;
    }

    this.deps.writeTo(`Preview environments (${response.previews.length}):`);
    this.deps.writeTo('');

    for (const preview of response.previews) {
      const prLabel = preview.pr_number ? `PR #${preview.pr_number}` : preview.branch;
      const age = PreviewListCommand.formatAge(preview.created_at);
      this.deps.writeTo(`  ${preview.id}  ${prLabel}  ${preview.status}  ${age}`);
      if (preview.url) {
        this.deps.writeTo(`    ${preview.url}`);
      }
    }

    return 0;
  }

  static formatAge(isoDate: string): string {
    try {
      const diffMs = Date.now() - new Date(isoDate).getTime();
      const hours = Math.floor(diffMs / (1000 * 60 * 60));
      if (hours < 1) return 'just now';
      if (hours < 24) return `${hours}h ago`;
      const days = Math.floor(hours / 24);
      return `${days}d ago`;
    } catch {
      return isoDate;
    }
  }

  private exitWithError(opts: PreviewListOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) payload.code = code;
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: PreviewListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

export class PreviewInspectCommand {
  private readonly deps: PreviewDeps;

  constructor(deps: PreviewDeps) {
    this.deps = deps;
  }

  async execute(id: string, opts: PreviewInspectOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Validate ID
    if (!id || id.trim() === '') {
      return this.exitWithError(opts, 'Preview ID is required. Usage: loopzero preview inspect <id>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const project = yamlResult.content.project;

    // 4. Call API
    let response: PreviewInspectApiResponse;
    try {
      response = await this.deps.inspectPreview(project, id);
    } catch (err) {
      return this.handleError(opts, err, id);
    }

    // 5. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(response));
      return 0;
    }

    this.deps.writeTo(`Preview: ${response.id}`);
    if (response.pr_number) {
      this.deps.writeTo(`  PR:       #${response.pr_number}`);
    }
    this.deps.writeTo(`  Branch:   ${response.branch}`);
    this.deps.writeTo(`  Status:   ${response.status}`);
    this.deps.writeTo(`  Created:  ${response.created_at}`);
    if (response.url) {
      this.deps.writeTo(`  URL:      ${response.url}`);
    }
    if (response.ttl_expires_at) {
      this.deps.writeTo(`  Expires:  ${response.ttl_expires_at}`);
    }

    if (response.apps && response.apps.length > 0) {
      this.deps.writeTo('');
      this.deps.writeTo('  Apps:');
      for (const app of response.apps) {
        this.deps.writeTo(`    ${app.name}  ${app.status}${app.url ? `  ${app.url}` : ''}`);
      }
    }

    return 0;
  }

  private exitWithError(opts: PreviewInspectOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) payload.code = code;
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: PreviewInspectOptions, err: unknown, id: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Preview ${id} not found.`, 1, 'PREVIEW_NOT_FOUND');
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

export class PreviewDeleteCommand {
  private readonly deps: PreviewDeps;

  constructor(deps: PreviewDeps) {
    this.deps = deps;
  }

  async execute(id: string, opts: PreviewDeleteOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Validate ID
    if (!id || id.trim() === '') {
      return this.exitWithError(opts, 'Preview ID is required. Usage: loopzero preview delete <id>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const project = yamlResult.content.project;

    // 4. Confirmation required (unless --yes)
    if (!opts.yes) {
      if (this.deps.isCI) {
        return this.exitWithError(opts, 'Confirmation required. Use --yes in CI mode.', 1, 'CONFIRMATION_REQUIRED');
      }
      if (this.deps.confirm) {
        const confirmed = await this.deps.confirm(`Delete preview ${id}? This will tear down the environment. [y/N] `);
        if (!confirmed) {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ status: 'cancelled' }));
          } else {
            this.deps.writeTo('Delete cancelled.');
          }
          return 0;
        }
      }
    }

    // 5. Call API
    try {
      await this.deps.deletePreview(project, id);
    } catch (err) {
      return this.handleError(opts, err, id);
    }

    // 6. Success
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ ok: true, preview_id: id, status: 'deleting' }));
    } else {
      this.deps.writeTo(`Preview ${id} is being deleted.`);
      this.deps.writeTo('The environment will be fully removed shortly.');
    }
    return 0;
  }

  private exitWithError(opts: PreviewDeleteOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) payload.code = code;
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: PreviewDeleteOptions, err: unknown, id: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Preview ${id} not found.`, 1, 'PREVIEW_NOT_FOUND');
      }
      if (err.statusCode === 409) {
        return this.exitWithError(opts, `Preview ${id} is already being deleted.`, 3, 'ALREADY_DELETING');
      }
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

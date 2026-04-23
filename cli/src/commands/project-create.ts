// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

interface ProjectCreateResponse {
  id: string;
  name: string;
  team: string;
  created_at: string;
}

export interface ProjectCreateDeps {
  getToken: () => string | null;
  createProject: (name: string) => Promise<ProjectCreateResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface ProjectCreateOptions {
  json?: boolean;
}

/**
 * ProjectCreateCommand creates a new project.
 *
 * Handles 409 (already exists) as idempotent success.
 *
 * Exit codes:
 *   0 — success (including 409 idempotent)
 *   1 — usage error (missing name, API error)
 *   4 — auth error
 */
export class ProjectCreateCommand {
  private readonly deps: ProjectCreateDeps;

  constructor(deps: ProjectCreateDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: ProjectCreateOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Name required
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Project name is required. Usage: loopzero project create <name>', 1, 'MISSING_ARGUMENT');
    }

    // 3. RFC 1123 name validation
    if (name.length > 63) {
      return this.exitWithError(opts, `Project name must be 63 characters or fewer (got ${name.length}). Choose a shorter name.`, 1, 'INVALID_NAME');
    }
    if (!/^[a-z][a-z0-9-]*$/.test(name)) {
      return this.exitWithError(opts, 'Project name must start with a lowercase letter and contain only lowercase letters, digits, and hyphens. Example: my-api', 1, 'INVALID_NAME');
    }
    if (name.endsWith('-')) {
      return this.exitWithError(opts, 'Project name must not end with a hyphen. Example: my-api', 1, 'INVALID_NAME');
    }

    // 4. Call API
    try {
      const result = await this.deps.createProject(name);

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ ...result, status: 'created' }));
      } else {
        this.deps.writeTo(`Project ${result.name} created.`);
      }
      return 0;
    } catch (err) {
      return this.handleError(opts, err, name);
    }
  }

  private exitWithError(opts: ProjectCreateOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
      if (code) {
        payload.code = code;
      }
      this.deps.writeTo(JSON.stringify(payload));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: ProjectCreateOptions, err: unknown, name: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      // 409 = already exists — idempotent success
      if (err.statusCode === 409) {
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ name, status: 'already_exists' }));
        } else {
          this.deps.writeTo(`Project ${name} already exists.`);
        }
        return 0;
      }
      return this.exitWithError(opts, `Failed to create project: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to create project: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to create project. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';

interface ProjectItem {
  id: string;
  name: string;
  team_id: string;
  created_at: string;
}

interface ProjectListResponse {
  projects: ProjectItem[];
}

export interface ProjectListDeps {
  getToken: () => string | null;
  listProjects: () => Promise<ProjectListResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface ProjectListOptions {
  json?: boolean;
}

/**
 * ProjectListCommand lists all projects for the current team.
 *
 * Does NOT require loopzero.yaml.
 *
 * Exit codes:
 *   0 — success (including empty list)
 *   1 — usage error (API error)
 *   4 — auth error
 */
export class ProjectListCommand {
  private readonly deps: ProjectListDeps;

  constructor(deps: ProjectListDeps) {
    this.deps = deps;
  }

  async execute(opts: ProjectListOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Call API
    let response: ProjectListResponse;
    try {
      response = await this.deps.listProjects();
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 3. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ projects: response.projects, total: response.projects.length }));
      return 0;
    }

    if (response.projects.length === 0) {
      this.deps.writeTo('No projects found.');
      return 0;
    }

    this.deps.writeTo('Projects:');
    this.deps.writeTo('');
    this.deps.writeTo(`  ${pad('NAME', 16)}CREATED`);
    for (const p of response.projects) {
      const created = formatDate(p.created_at);
      this.deps.writeTo(`  ${pad(p.name, 16)}${created}`);
    }
    this.deps.writeTo('');
    this.deps.writeTo(`${response.projects.length} project${response.projects.length !== 1 ? 's' : ''}`);

    return 0;
  }

  private exitWithError(opts: ProjectListOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: ProjectListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      return this.exitWithError(opts, `Failed to list projects: ${err.message}. Try again.`, 1, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to list projects: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to list projects. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
}

function pad(str: string, len: number): string {
  return str.length >= len ? str + ' ' : str + ' '.repeat(len - str.length);
}

function formatDate(iso: string): string {
  try {
    const d = new Date(iso);
    const yyyy = d.getFullYear();
    const mm = String(d.getMonth() + 1).padStart(2, '0');
    const dd = String(d.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  } catch {
    return iso;
  }
}

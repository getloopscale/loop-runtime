// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import { classifyNetworkError } from '../output/error-message';

// CHG-2026-04-23-AUDIT-4: aligned with real API shape.
// api/internal/routes/projects.go returns `team` (string), not `team_id`.
// Prior interface used an invented field name; fixtures propagated the drift.
interface ProjectDetail {
  id: string;
  name: string;
  team: string;
  created_at: string;
  created_by?: string;
}

export interface ProjectInspectDeps {
  getToken: () => string | null;
  getProject: (name: string) => Promise<ProjectDetail>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface ProjectInspectOptions {
  json?: boolean;
}

/**
 * ProjectInspectCommand shows detailed information about a single project.
 *
 * Does NOT require loopzero.yaml — the project name is sufficient.
 *
 * Exit codes:
 *   0 — success
 *   1 — usage error (missing name, not found, API error)
 *   4 — auth error
 */
export class ProjectInspectCommand {
  private readonly deps: ProjectInspectDeps;

  constructor(deps: ProjectInspectDeps) {
    this.deps = deps;
  }

  async execute(name: string, opts: ProjectInspectOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Name required
    if (!name || name.trim() === '') {
      return this.exitWithError(opts, 'Project name is required. Usage: loopzero project inspect <name>', 1, 'MISSING_ARGUMENT');
    }

    // 3. Call API
    let project: ProjectDetail;
    try {
      project = await this.deps.getProject(name);
    } catch (err) {
      return this.handleError(opts, err, name);
    }

    // 4. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(project));
      return 0;
    }

    this.deps.writeTo(`Project: ${project.name}`);
    this.deps.writeTo('');
    this.deps.writeTo(`  Team:       ${project.team}`);
    this.deps.writeTo(`  Created:    ${formatDate(project.created_at)}`);
    if (project.created_by) {
      this.deps.writeTo(`  Created by: ${project.created_by}`);
    }
    this.deps.writeTo('');
    this.deps.writeTo(`Manage: loopzero project list | loopzero project delete ${project.name}`);

    return 0;
  }

  private exitWithError(opts: ProjectInspectOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: ProjectInspectOptions, err: unknown, name: string): number {
    if (err instanceof ApiError) {
      if (err.exitCode === 4 || err.statusCode === 401) {
        return this.exitWithError(opts, 'Authentication failed. Run: loopzero login', 4, 'AUTH_REQUIRED');
      }
      if (err.statusCode === 404) {
        return this.exitWithError(opts, `Project "${name}" not found.`, 1, 'PROJECT_NOT_FOUND');
      }
      return this.exitWithError(opts, `Failed to get project: ${err.message}. Try again.`, 1, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 1, 'API_UNREACHABLE');
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Failed to get project: ${err.message}. Try again.`, 1, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Failed to get project. Try again later.', 1, 'UNEXPECTED_ERROR');
  }
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

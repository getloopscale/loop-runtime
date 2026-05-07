// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';
import { classifyNetworkError } from '../output/error-message';

// CHG-2026-04-23-AUDIT-2: aligned with real API shape.
// API returns `last_build.id` (not build_id) and `active_release` (not release).
// Prior test fixtures used invented field names; this interface now matches
// what api/internal/routes/status.go actually produces.
// CHG-2026-04-23-AUDIT-33: active_release shape follows the API
// (api/internal/routes/status.go activeReleaseResponse): id,
// status, git_commit, created_at. Previous code read release_id /
// state / activated_at — all undefined at runtime, causing
// `state.toLowerCase()` to crash.
interface StatusApiResponse {
  environment: string;
  last_build?: {
    id: string;
    status: string;
    finished_at: string;
  };
  active_release?: {
    id: string;
    status: string;
    git_commit?: string;
    created_at: string;
  } | null;
  sync_status?: string;
  url?: string;
  scaling?: {
    replicas: number;
    min: number;
    max: number;
    scaled_to_zero: boolean;
  };
  // argo-rollouts-progressive-delivery-plan T4.3 — Canary rollout state.
  // Present when the env has progressive delivery enabled and the most
  // recent release is mid-canary or recently completed. Optional/null
  // for legacy Deployment-based deploys.
  rollout?: {
    phase: string;             // "Healthy" | "Progressing" | "Paused" | "Degraded" | "Aborted"
    current_step?: number;
    total_steps?: number;
    canary_weight?: number;    // current traffic percent on canary (0-100)
    analysis_status?: string;  // "running" | "successful" | "failed" | "inconclusive"
  } | null;
}

export interface StatusDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  getStatus: (project: string, environment: string) => Promise<StatusApiResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface StatusOptions {
  env?: string;
  json?: boolean;
}

export class StatusCommand {
  private readonly deps: StatusDeps;

  constructor(deps: StatusDeps) {
    this.deps = deps;
  }

  async execute(opts: StatusOptions = {}): Promise<number> {
    const environment = opts.env ?? 'production';

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
    let status: StatusApiResponse;
    try {
      status = await this.deps.getStatus(project, environment);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 4. Map internal sync_status to product language
    if (status.sync_status) {
      status = { ...status, sync_status: StatusCommand.mapSyncStatus(status.sync_status) };
    }

    // 5. Display result
    if (opts.json) {
      this.deps.writeTo(JSON.stringify(status));
      return 0;
    }

    this.deps.writeTo(`Environment: ${status.environment}`);

    if (!status.last_build) {
      this.deps.writeTo('No deploys yet. Run `loopzero deploy` to get started.');
      return 0;
    }

    const buildSummary = `${status.last_build.id}`;
    this.deps.writeTo(`Last deploy:  ${buildSummary} — ${status.last_build.status}`);

    if (status.active_release) {
      this.deps.writeTo(`Release:      ${status.active_release.id} (${status.active_release.status.toLowerCase()} since ${this.formatTime(status.active_release.created_at)})`);
    }

    if (status.scaling) {
      if (status.scaling.scaled_to_zero) {
        this.deps.writeTo(`Scaling:      sleeping (0/${status.scaling.max} replicas)`);
        this.deps.writeTo(`              First request wakes up in ~5-10s`);
      } else {
        this.deps.writeTo(`Scaling:      ${status.scaling.replicas}/${status.scaling.max} replicas`);
      }
    }

    // CHG-2026-04-23-AUDIT-2: API returns literal "unknown" when sync state is
    // not yet determined; surfacing that to the user is noise. Skip display.
    if (status.sync_status && status.sync_status !== 'unknown') {
      this.deps.writeTo(`Sync:         ${status.sync_status}`);
    }

    // argo-rollouts-progressive-delivery-plan T4.3 — Canary rollout state.
    // Product language only (Rule 3): never expose 'Rollout' / 'Argo' /
    // 'TraefikService'. Always render as 'Progressive deploy'.
    if (status.rollout) {
      this.deps.writeTo(`Progressive deploy: ${StatusCommand.formatRolloutLine(status.rollout)}`);
    }

    if (status.url) {
      this.deps.writeTo(`URL:          ${status.url}`);
    }

    return 0;
  }

  // formatRolloutLine renders the rollout state for human output.
  // Examples:
  //   "complete (100%)"
  //   "50% (step 3/5) — analyzing"
  //   "20% (step 1/5) — paused"
  //   "aborted at 20%"
  static formatRolloutLine(r: NonNullable<StatusApiResponse['rollout']>): string {
    const phase = (r.phase ?? '').toLowerCase();
    const weight = r.canary_weight ?? 0;
    if (phase === 'healthy') {
      return 'complete (100%)';
    }
    if (phase === 'aborted' || phase === 'degraded') {
      return `${phase} at ${weight}%`;
    }
    const stepLabel = (r.current_step && r.total_steps)
      ? ` (step ${r.current_step}/${r.total_steps})`
      : '';
    const analysis = r.analysis_status ? ` — ${r.analysis_status}` : '';
    const phaseSuffix = phase ? ` (${phase})` : '';
    if (analysis) {
      return `${weight}%${stepLabel}${analysis}`;
    }
    return `${weight}%${stepLabel}${phaseSuffix}`;
  }

  private static readonly SYNC_STATUS_MAP: Record<string, string> = {
    'Synced': 'Up to date',
    'OutOfSync': 'Update pending',
    'Progressing': 'Deploying',
    'Unknown': 'Unknown',
  };

  static mapSyncStatus(raw: string): string {
    return StatusCommand.SYNC_STATUS_MAP[raw] ?? raw;
  }

  private formatTime(isoDate: string): string {
    try {
      const date = new Date(isoDate);
      return date.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
    } catch {
      return isoDate;
    }
  }

  private exitWithError(opts: StatusOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: StatusOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    // CHG-2026-04-23-AUDIT-24: surface API_UNREACHABLE for network errors so
    // users know to check LOOPZERO_URL. Exit code stays 3 (contract) — only
    // the code label and message change.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

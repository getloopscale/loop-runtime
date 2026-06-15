// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';

export interface UploadResult {
  commit_sha: string;
  apps: string[];
}

// B15 (MEET-2026-04-23-059): scaling config sent to API.
// Either `replicas` OR `autoscaling` — never both (enforced by CLI validator
// and API orchestrator). Matches services.AppAutoscalingSpec field names.
export interface AppAutoscaling {
  min: number;
  max: number;
  target_cpu: number;
}

export interface AppInput {
  name: string;
  type: string;                        // "server" | "worker" | "frontend" (always resolved)
  port: number | null;
  path: string;
  framework: string;
  runtime?: string;                    // services.json runtime ("python" | "node") — the build-strategy key the API dispatches on (F-dom-2)
  build: string;                       // v2: "dockerfile" | "buildpack" | "custom"
  dockerfile: string | null;           // v2: Dockerfile path relative to app path
  rendering: string;                   // v2: "static" | "server" (Sprint 10)
  env: Record<string, string>;         // v2: build-time environment variables
  secrets: string[];
  replicas?: number;                   // B15: fixed replica count (omit when autoscaling is set)
  autoscaling?: AppAutoscaling;        // B15: HPA config (omit when replicas is set)
}

export interface SubmitBuildInput {
  project: string;
  environment: string;
  apps: AppInput[];
  idempotencyKey: string;
  commitSha: string;
  noCache?: boolean;
}

export interface SubmitBuildResult {
  build_id: string;
  stream_url: string;
}

export interface SSEEvent {
  type: string;
  data: DeployCompletedPayload | Record<string, unknown>;
}

export interface SSEConnection {
  close: () => void;
}

export interface AppResult {
  name: string;
  status: string;
  url?: string;
  custom_domain_urls?: string[]; // MEET-2026-04-05-033: Active custom domain URLs
  build_duration_seconds: number;
  error?: string;
}

export interface DeployCompletedPayload {
  deploy_id: string;
  status: string;
  duration_seconds: number;
  apps: AppResult[];
}

export interface BuildStatus {
  status: string;
  error_message?: string;
}

export interface DeployDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  /**
   * Plan v1.1 T1.2 (loopzero-cli-vercel-like-ux) — locates the
   * `.loopzero/services.json` artifact for the upload payload. Optional in the
   * interface so `register/promote.ts`, `register/rollback.ts`, and
   * `register/validate.ts` keep compiling without injecting this dep (EC-9
   * absorbed — they don't upload bundles). The deploy command runtime-checks
   * presence and throws `IllegalStateError` when missing from the deploy
   * registration site. `overridePath` is the value of the `--services-json`
   * CLI flag (EC-4 absorbed).
   */
  findServicesJson?: (overridePath?: string) => { path: string } | null;
  /**
   * Plan v1.1 T3.1 — auto-build trigger. When `.loopzero/services.json` is
   * missing AND `skipBuild` is false, the deploy command invokes this to run
   * `loopzerokit build --target loopzero-cloud` as a subprocess. Optional in the
   * interface so promote/rollback/validate registrations continue to compile
   * without injecting it.
   */
  runLoopZerokitBuild?: (opts: { cwd: string; target?: string }) => Promise<void>;
  hasGitRepo: () => boolean;
  hasUncommittedChanges: () => boolean;
  hasLockfile: () => boolean;
  getSourceIdentifier: () => string;
  createSourceBundle: () => Promise<string>;
  checkSensitiveFiles?: () => string[];
  uploadSource: (bundlePath: string, servicesJsonPath: string) => Promise<UploadResult>;
  submitBuild: (input: SubmitBuildInput) => Promise<SubmitBuildResult>;
  connectSSE: (buildId: string, onEvent: (event: SSEEvent) => void) => SSEConnection;
  getSecrets: (project: string, environment: string) => Promise<string[]>;
  cleanupFile: (filePath: string) => void;
  generateIdempotencyKey: (commitSha: string, project: string, environment: string, apps: string[]) => string;
  /** Check build state as fallback when SSE stream breaks. */
  getBuildStatus?: (buildId: string) => Promise<BuildStatus>;
  /**
   * Beta Logs Gate Criterion 4 (UX) — best-effort auto-tail of the
   * last ~20 log lines when a deploy fails. The CLI invokes this in
   * the build.failed / deploy.sync_failed / deploy.delivery_failed
   * SSE branches BEFORE exiting, so the operator sees recent context
   * inline instead of having to run `loopzero logs --build <id>` as a
   * second step.
   *
   * Implementation MUST swallow errors and return null on failure —
   * auto-tail is opportunistic; the deploy command still exits with
   * the canonical reason + hint when the tail is unavailable.
   *
   * Optional: when undefined, the deploy command falls back to the
   * pre-Beta hint-only behavior.
   */
  fetchRecentBuildLogs?: (buildId: string) => Promise<import('./logs').LogsApiResponse | null>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  isTTY?: boolean;
}

export interface DeployOptions {
  env?: string;
  app?: string[];
  wait?: boolean;
  json?: boolean;
  noCache?: boolean;
  /**
   * Plan v1.1 T1.1 (EC-4) — explicit path to `.loopzero/services.json`. When set,
   * bypasses the walk-up discovery. Required when `loopzero.config.ts` declares
   * a custom `distDir` (e.g. emit lands at `out/services.json`).
   */
  servicesJson?: string;
  /**
   * Plan v1.1 T3.1 (and Phase 5 `--prebuilt` alias). Skip the auto-build
   * branch. Used by CI pipelines that produce the artifact in a prior stage.
   */
  skipBuild?: boolean;
}

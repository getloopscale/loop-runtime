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
  hasGitRepo: () => boolean;
  hasUncommittedChanges: () => boolean;
  hasLockfile: () => boolean;
  getSourceIdentifier: () => string;
  createSourceBundle: () => Promise<string>;
  checkSensitiveFiles?: () => string[];
  uploadSource: (bundlePath: string, loopzeroYamlPath: string) => Promise<UploadResult>;
  submitBuild: (input: SubmitBuildInput) => Promise<SubmitBuildResult>;
  connectSSE: (buildId: string, onEvent: (event: SSEEvent) => void) => SSEConnection;
  getSecrets: (project: string, environment: string) => Promise<string[]>;
  cleanupFile: (filePath: string) => void;
  generateIdempotencyKey: (commitSha: string, project: string, environment: string, apps: string[]) => string;
  /** Check build state as fallback when SSE stream breaks. */
  getBuildStatus?: (buildId: string) => Promise<BuildStatus>;
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
}

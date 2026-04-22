// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { ApiError } from '../api/client';
import { MAX_ENVIRONMENT_NAME_LENGTH } from '../config/constants';
import type { DeployCompletedPayload, DeployDeps, DeployOptions } from './deploy-types';

/**
 * Resolve the CLI exit code from a deploy.completed payload.
 */
export function resolveExitCode(payload: DeployCompletedPayload, requestedAppCount: number): number {
  if (payload.status === 'success') {
    return 0;
  }

  if (payload.status === 'partial_failure') {
    return 5;
  }

  if (payload.status === 'deploy_failed') {
    return 3;
  }

  // status === 'failed' — build failure or deploy failure
  // DA-3: partial failure (exit 5) only with 2+ apps
  const failedApps = payload.apps.filter((a) => a.status === 'failed');
  const deployedApps = payload.apps.filter((a) => a.status === 'deployed');

  if (requestedAppCount >= 2 && failedApps.length > 0 && deployedApps.length > 0) {
    return 5;
  }

  return 2;
}

/**
 * Validate environment name. Returns an error message or null if valid.
 */
export function validateEnvironment(env: string): string | null {
  if (!env || env.length === 0) {
    return 'Environment name cannot be empty.';
  }
  if (env !== env.toLowerCase()) {
    return `Environment names must be lowercase. Example: --env ${env.toLowerCase()}`;
  }
  if (!/^[a-z]/.test(env)) {
    return 'Environment name must start with a lowercase letter. Example: --env staging';
  }
  if (env.length > MAX_ENVIRONMENT_NAME_LENGTH) {
    return `Environment name too long (${env.length} chars, max ${MAX_ENVIRONMENT_NAME_LENGTH}). Use a shorter name.`;
  }
  if (!/^[a-z][a-z0-9-]*$/.test(env)) {
    return 'Environment name can only contain lowercase letters, numbers, and hyphens. Example: --env staging';
  }
  return null;
}

/**
 * Map internal sync failure reasons to user-facing messages (Rule 3).
 */
export function mapSyncFailureReason(reason: string, buildId: string): string {
  const knownReasons: Record<string, string> = {
    'HEALTH_PROBE_FAILED': "Service didn't become healthy in time. Check your app's /health endpoint and logs.",
    'SYNC_TIMEOUT': 'Deploy timed out waiting for service to start. Check service logs for startup errors.',
    'IMAGE_PULL_FAILED': 'Could not pull the built image. This is usually a temporary issue — try deploying again.',
    'RESOURCE_QUOTA_EXCEEDED': 'Deployment exceeds resource limits. Reduce resource requests in your configuration.',
    'CRASH_LOOP': 'Service keeps crashing on startup. Check your app logs for errors.',
  };

  const message = knownReasons[reason]
    ?? `Deploy failed: ${reason.toLowerCase().replace(/_/g, ' ')}. Check service logs for details.`;

  return `${message} Run: loopzero logs --build ${buildId}`;
}

/**
 * Sanitize build error messages by replacing internal terms with product language (Rule 3).
 */
export function sanitizeBuildError(raw: string): string {
  let msg = raw;
  msg = msg.replace(/Workflow\s+(Failed|Error|Timed\s*Out)/gi, 'Build $1');
  msg = msg.replace(/\bin\s+build\s+cluster\b/gi, '');
  msg = msg.replace(/\bworkflow\b/gi, 'build');
  msg = msg.replace(/\bArgo\s*CD\b/gi, 'deployment sync');
  msg = msg.replace(/\bGitea\b/gi, 'repository');
  msg = msg.replace(/\bMinIO\b/gi, 'storage');
  msg = msg.replace(/\bKaniko\b/gi, 'image builder');
  msg = msg.replace(/\bCRD\b/g, 'service configuration');
  msg = msg.replace(/\btenant_id\b/gi, 'account');
  msg = msg.replace(/\badvisory\s*lock\b/gi, '');
  msg = msg.replace(/\bpg_advisory[_a-z]*/gi, '');
  msg = msg.replace(/\bgitops[-_]push\b/gi, 'deploy');
  msg = msg.replace(/\bkubebuilder\b/gi, '');
  msg = msg.replace(/\benvtest\b/gi, '');
  // Clean up extra whitespace
  msg = msg.replace(/\s{2,}/g, ' ').trim();
  return msg || 'unknown error';
}

/**
 * Format and emit an error, returning the exit code. Handles both JSON and human output.
 */
export function exitWithError(
  deps: DeployDeps,
  opts: DeployOptions,
  message: string,
  exitCode: number,
  code?: string,
): number {
  if (opts.json) {
    const payload: Record<string, unknown> = { error: message, exit_code: exitCode };
    if (code) {
      payload.code = code;
    }
    deps.writeTo(JSON.stringify(payload));
  } else {
    deps.writeError(message);
  }
  return exitCode;
}

/**
 * Handle an unknown error (ApiError or generic Error), returning the appropriate exit code.
 */
export function handleError(
  deps: DeployDeps,
  opts: DeployOptions,
  err: unknown,
  context?: string,
): number {
  if (err instanceof ApiError) {
    const message = context ? `${context}: ${err.message}` : err.message;
    return exitWithError(deps, opts, message, err.exitCode, err.code);
  }
  if (err instanceof Error) {
    return exitWithError(deps, opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
  }
  return exitWithError(deps, opts, 'Unexpected error. Wait for it to complete, or try again later.', 3, 'UNEXPECTED_ERROR');
}

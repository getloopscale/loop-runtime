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
 *
 * Beta Logs Gate Criterion 4 — error messages must be actionable.
 * Each entry maps a reason code to: (1) clear description of what
 * went wrong, (2) a concrete next step the operator can take.
 *
 * The reason strings here cover the union of:
 *   - Original 5 (audit baseline 2026-04-23)
 *   - Argo Rollouts auto-abort reasons (rollout_events.go: analysis-
 *     failed, pod-unhealthy, user-aborted)
 *   - ArgoCD sync error patterns (sync_client.go InvalidSpecError)
 *   - Policy-controller image rejection
 *   - cert-manager Certificate states
 *   - Build pipeline stages (Dockerfile lint, deps, sign, push, SBOM)
 *   - Plan enforcement rejections
 *   - ESO secret sync failures
 *   - Network policy denials
 *
 * Adding a reason here is preferred over relying on the fallback,
 * because the fallback produces lowercase-with-spaces output that
 * looks like a generic shrug to the operator. Reason keys MUST match
 * the canonical UPPER_SNAKE form emitted by the API services that
 * raise deploy.sync_failed events.
 */
export function mapSyncFailureReason(reason: string, buildId: string): string {
  const knownReasons: Record<string, string> = {
    // ─── Original 5 (audit baseline 2026-04-23) ──────────────────
    'HEALTH_PROBE_FAILED': "Service didn't become healthy in time. Check your app's /health endpoint and that it responds 200 within 30s of startup.",
    'SYNC_TIMEOUT': 'Deploy timed out waiting for service to become available. Check service logs for startup errors or slow init paths.',
    'IMAGE_PULL_FAILED': 'Could not pull the built image. This is usually a transient registry issue — retry the deploy. If persistent, check registry credentials.',
    'RESOURCE_QUOTA_EXCEEDED': 'Deployment exceeds your plan resource limits. Reduce CPU/memory requests in `loopzero.yaml` or upgrade your plan.',
    'CRASH_LOOP': 'Service keeps crashing on startup. Check your app logs for startup errors (uncaught exceptions, missing env vars, port conflicts).',

    // ─── Build pipeline failures ─────────────────────────────────
    'BUILD_DOCKERFILE_INVALID': 'Your Dockerfile failed validation. LoopZero enforces an allow-list of base images and refuses --privileged/--security=insecure flags. Review the build logs for the specific rejection rule.',
    'BUILD_DEPENDENCY_FAILED': 'Dependency installation failed during build (npm/pip/go mod). Check your lockfile is committed and that all packages resolve.',
    'BUILD_SIGN_FAILED': 'Image signing failed. This is usually a transient cosign infrastructure issue — retry the deploy. If persistent, contact support.',
    'BUILD_PUSH_FAILED': 'Could not push the built image to the registry. Usually transient — retry. If persistent, registry quota may be exhausted.',
    'BUILD_SBOM_FAILED': 'SBOM generation failed during build. Non-blocking for the deploy itself, but the build is recorded as FAILED. Retry or contact support if persistent.',

    // ─── Sync / deploy failures ──────────────────────────────────
    'ACTIVATION_FAILED': 'The deploy could not activate the new release. Check that no concurrent deploy is in flight, then retry.',
    'POLICY_VIOLATION': 'Your image was rejected by the security policy (unsigned, missing SBOM, or unapproved base image). Build logs identify the specific policy.',
    'INVALID_SPEC': 'The deployment manifest is malformed. This is usually a LoopZero-side issue — please contact support with the build ID.',
    'INGRESS_NOT_READY': 'Public route could not be provisioned. Check that your custom domain DNS is correctly configured (run `loopzero domains list`).',
    'TLS_CERT_PENDING': 'TLS certificate is still being issued by Let\'s Encrypt. This usually completes within 5min — wait and check `loopzero status` again.',
    'NETWORK_POLICY_BLOCKED': 'Network policy denied a required connection during startup. Contact support — this is a platform-side configuration issue.',

    // ─── Progressive delivery (Argo Rollouts) ────────────────────
    'ANALYSIS_FAILED': 'Canary analysis failed — your new version did not meet the success-rate or latency thresholds. The deploy auto-rolled-back to the previous version.',
    'POD_UNHEALTHY': 'Canary pods did not reach healthy state within the progress deadline (5min). The deploy auto-rolled-back. Check the canary pod logs.',
    'USER_ABORTED': 'A user (or automation) aborted the rollout via `loopzero rollback`. The deploy reverted to the previous version.',
    'ROLLOUT_ABORTED': 'Canary rollout was aborted before completion. Check `loopzero status` for the abort reason.',

    // ─── Resource / quota failures ───────────────────────────────
    'PLAN_QUOTA_EXCEEDED': 'You\'ve reached your plan\'s resource limit (projects, builds, environments, or seats). Run `loopzero status` to see usage, or upgrade your plan.',
    'SECRET_SYNC_FAILED': 'A secret your app depends on could not be made available to the running pod. Verify the secret exists via `loopzero secrets list`.',
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

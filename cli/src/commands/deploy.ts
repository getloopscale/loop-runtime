// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { DeployRenderer, DeployResult } from '../output/deploy-renderer';
import {
  DEPLOY_TIMEOUT_MS,
  MAX_BUNDLE_SIZE_BYTES,
} from '../config/constants';
import { normalizeApps } from '../adapters/app-unit';
import { validateConfig } from '../adapters/validate-config';
import { getTracer, SpanStatusCode } from '../tracing';
import * as fs from 'fs';
import * as path from 'path';

import type {
  AppInput,
  DeployCompletedPayload,
  SSEConnection,
  SSEEvent,
} from './deploy-types';

import {
  resolveExitCode,
  validateEnvironment,
  mapSyncFailureReason,
  sanitizeBuildError,
  exitWithError,
  handleError,
} from './deploy-helpers';

// Re-export types so existing consumers (tests, index.ts) keep working
export type { DeployDeps, DeployOptions } from './deploy-types';

/** Result of validatePrerequisites — carries validated state needed by subsequent phases. */
interface PrerequisiteResult {
  config: { project: string; apps: Record<string, unknown> };
  yamlPath: string;
  environment: string;
  selectedApps: string[];
  appUnits: ReturnType<typeof normalizeApps>;
}

export class DeployCommand {
  private readonly deps: import('./deploy-types').DeployDeps;

  constructor(deps: import('./deploy-types').DeployDeps) {
    this.deps = deps;
  }

  async execute(opts: import('./deploy-types').DeployOptions): Promise<number> {
    const tracer = getTracer();
    const environment = opts.env ?? 'production';
    const shouldWait = opts.wait !== false;

    // Phase 1: Validate all prerequisites (auth, config, env, apps, secrets)
    const prereqResult = await this.validatePrerequisites(opts, environment);
    if (typeof prereqResult === 'number') {
      return prereqResult;
    }

    const { config, yamlPath, selectedApps, appUnits } = prereqResult;

    // Phase 2: Create source bundle and upload (traced)
    const uploadResult = await tracer.startActiveSpan('loopzero.cli.deploy.bundle', async (span) => {
      try {
        const result = await this.createAndUploadBundle(opts, config, yamlPath);
        if (typeof result === 'number') {
          span.setStatus({ code: SpanStatusCode.ERROR, message: `exit ${result}` });
        }
        return result;
      } catch (err) {
        span.setStatus({ code: SpanStatusCode.ERROR, message: String(err) });
        throw err;
      } finally {
        span.end();
      }
    });
    if (typeof uploadResult === 'number') {
      return uploadResult;
    }

    // Phase 3: Submit build and optionally stream SSE events (traced)
    return tracer.startActiveSpan('loopzero.cli.deploy.stream', async (span) => {
      try {
        const code = await this.submitAndStreamBuild(opts, config, environment, selectedApps, appUnits, uploadResult, shouldWait);
        if (code !== 0) {
          span.setStatus({ code: SpanStatusCode.ERROR, message: `exit ${code}` });
        }
        return code;
      } catch (err) {
        span.setStatus({ code: SpanStatusCode.ERROR, message: String(err) });
        throw err;
      } finally {
        span.end();
      }
    });
  }

  /**
   * Phase 1: Validate auth, loopzero.yaml, environment, app selection, and secrets.
   * Returns a PrerequisiteResult on success or an exit code on failure.
   */
  private async validatePrerequisites(
    opts: import('./deploy-types').DeployOptions,
    environment: string,
  ): Promise<PrerequisiteResult | number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return exitWithError(this.deps, opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 3. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return exitWithError(this.deps, opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    // 3b. Check for YAML parse errors
    if (yamlResult.parseError) {
      return exitWithError(this.deps, opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const { content: config } = yamlResult;

    // 4. Validate loopzero.yaml — run full validation BEFORE upload (instant feedback).
    const projectRoot = path.dirname(yamlResult.path);
    const skipFs = !fs.existsSync(projectRoot);
    const { errors: validationErrors, warnings: validationWarnings } = validateConfig(config, projectRoot, { skipFilesystemChecks: skipFs });
    if (validationErrors.length > 0) {
      const errorList = validationErrors.map((e, i) => `  ${i + 1}. ${e.message}`).join('\n');
      return exitWithError(
        this.deps,
        opts,
        `Invalid configuration:\n\n${errorList}\n\nFix the errors above and try again.`,
        1,
        'INVALID_CONFIG',
      );
    }
    // Show warnings but continue
    for (const w of validationWarnings) {
      this.deps.writeError(`Warning: ${w}`);
    }

    // 5. Validate apps exist
    const appNames = Object.keys(config.apps);
    if (appNames.length === 0) {
      return exitWithError(this.deps, opts, 'No apps defined in loopzero.yaml. Add at least one app to deploy.', 1, 'INVALID_CONFIG');
    }

    // 6. Validate --env (DA-10)
    const envError = validateEnvironment(environment);
    if (envError) {
      return exitWithError(this.deps, opts, envError, 1, 'INVALID_ENVIRONMENT');
    }

    // 7. Validate --app flag (DA-19) — fail fast before any upload
    const selectedApps = opts.app ?? appNames;
    for (const app of selectedApps) {
      if (!appNames.includes(app)) {
        return exitWithError(this.deps, opts, `App "${app}" not found in loopzero.yaml. Available apps: ${appNames.join(', ')}`, 1, 'APP_NOT_FOUND');
      }
    }

    // 6b. Normalize apps into AppUnits (M6: declarative contract)
    const appUnits = normalizeApps(config.apps, selectedApps);

    // 7. Validate secrets (pre-deploy check) — scoped to selected apps only (F-02)
    const secretsError = await this.validateSecrets(opts, config, environment, appUnits);
    if (secretsError !== null) {
      return secretsError;
    }

    return { config, yamlPath: yamlResult.path, environment, selectedApps, appUnits };
  }

  /**
   * Validate that all declared secrets are configured on the server.
   * Returns null on success or an exit code on failure.
   */
  private async validateSecrets(
    opts: import('./deploy-types').DeployOptions,
    config: { project: string },
    environment: string,
    appUnits: ReturnType<typeof normalizeApps>,
  ): Promise<number | null> {
    const declaredSecrets = [...new Set(appUnits.flatMap(u => u.secrets))];
    if (declaredSecrets.length === 0) {
      return null;
    }

    let configuredSecrets: string[];
    try {
      configuredSecrets = await this.deps.getSecrets(config.project, environment);
    } catch {
      // MOD-6: Network error during secrets check — warn and continue
      if (!opts.json) {
        this.deps.writeTo('Warning: Could not verify secrets. Deploy will continue but may fail if secrets are missing.');
      }
      return null;
    }
    const missingSecrets = declaredSecrets.filter((s) => !configuredSecrets.includes(s));
    if (missingSecrets.length > 0) {
      const msg = `Missing secrets for env "${environment}": ${missingSecrets.join(', ')}`;
      const hint = 'Set secrets before deploying. See: https://docs.loopzero.example/secrets';
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ code: 'MISSING_SECRETS', error: `${msg}. ${hint}`, exit_code: 1 }));
      } else {
        this.deps.writeError(msg);
        this.deps.writeError(hint);
      }
      return 1;
    }
    return null;
  }

  /**
   * Phase 2: Emit pre-upload warnings, create source bundle, validate size, and upload.
   * Returns the UploadResult on success or an exit code on failure.
   */
  private async createAndUploadBundle(
    opts: import('./deploy-types').DeployOptions,
    config: { apps: Record<string, unknown> },
    yamlPath: string,
  ): Promise<import('./deploy-types').UploadResult | number> {
    // 8. Warn about uncommitted changes (only relevant for git projects)
    if (this.deps.hasGitRepo() && this.deps.hasUncommittedChanges()) {
      if (!opts.json) {
        this.deps.writeTo('Warning: You have uncommitted changes. Only committed code will be deployed.');
      }
    }

    // 8b. Warn about missing lockfile (F-03: fail-fast before upload)
    if (!this.deps.hasLockfile()) {
      const appPaths = Object.values(config.apps).map((a) => (a as Record<string, unknown>).path as string).filter(p => p && p !== '.');
      const hasAppLockfile = appPaths.some(appPath => {
        return ['package-lock.json', 'yarn.lock', 'pnpm-lock.yaml'].some(
          f => fs.existsSync(path.join(appPath, f))
        );
      });
      if (!hasAppLockfile && !opts.json) {
        this.deps.writeTo('Warning: No lockfile found (package-lock.json, yarn.lock, or pnpm-lock.yaml). Build may fail. Run: npm install');
      }
    }

    // 9. Validate source is deployable (has files)
    let sourceVersion: string;
    try {
      sourceVersion = this.deps.getSourceIdentifier();
    } catch {
      return exitWithError(this.deps, opts,
        'Failed to identify source version. Ensure your project has files to deploy.',
        1, 'SOURCE_ID_FAILED');
    }

    // 9a. Show deploy source feedback
    if (!opts.json) {
      if (this.deps.hasGitRepo()) {
        this.deps.writeTo(`Deploying from git commit ${sourceVersion.substring(0, 12)}`);
      } else {
        this.deps.writeTo(`Deploying all files in current directory (version: ${sourceVersion.substring(0, 15)})`);
      }
    }

    // 9b. Warn about potentially sensitive files
    const sensitiveFiles = this.deps.checkSensitiveFiles?.() ?? [];
    if (sensitiveFiles.length > 0 && !opts.json) {
      this.deps.writeTo(`Warning: Potentially sensitive files detected: ${sensitiveFiles.join(', ')}`);
      this.deps.writeTo('These files will be included in the deploy bundle.');
    }

    // 10. Create source bundle (git bundle or tar.gz)
    let bundlePath: string;
    try {
      bundlePath = await this.deps.createSourceBundle();
    } catch (err) {
      return exitWithError(this.deps, opts, `Failed to create source bundle. ${(err as Error).message}`, 1, 'BUNDLE_FAILED');
    }

    // 10b. Validate bundle size before upload (MOD-5)
    try {
      const stat = fs.statSync(bundlePath);
      if (stat.size > MAX_BUNDLE_SIZE_BYTES) {
        this.deps.cleanupFile(bundlePath);
        const sizeMB = Math.ceil(stat.size / (1024 * 1024));
        return exitWithError(this.deps, opts,
          `Source bundle too large (${sizeMB}MB, max 500MB). Reduce repository size or use .gitignore to exclude large files.`,
          1, 'BUNDLE_TOO_LARGE');
      }
    } catch {
      // Can't stat — proceed anyway, server will reject if too large
    }

    // 11. Upload source
    let uploadResult: import('./deploy-types').UploadResult;
    try {
      uploadResult = await this.deps.uploadSource(bundlePath, yamlPath);
    } catch (err) {
      this.deps.cleanupFile(bundlePath);
      return handleError(this.deps, opts, err, 'Upload rejected');
    }

    // 12. Cleanup temp file
    this.deps.cleanupFile(bundlePath);

    return uploadResult;
  }

  /**
   * Phase 3: Submit the build and optionally connect to SSE stream for completion.
   * Returns the CLI exit code.
   */
  private async submitAndStreamBuild(
    opts: import('./deploy-types').DeployOptions,
    config: { project: string },
    environment: string,
    selectedApps: string[],
    appUnits: ReturnType<typeof normalizeApps>,
    uploadResult: import('./deploy-types').UploadResult,
    shouldWait: boolean,
  ): Promise<number> {
    // 13. Generate deterministic Idempotency-Key (A-01)
    const idempotencyKey = this.deps.generateIdempotencyKey(
      uploadResult.commit_sha,
      config.project,
      environment,
      selectedApps,
    );
    // M6: Build app inputs from normalized AppUnits (no inference at deploy time).
    // B15 (MEET-2026-04-23-059): forward scaling config so the API can enforce
    // plan caps and render the correct Deployment / HPA. Mutual exclusion is
    // already validated by validate-config — here we trust the AppUnit.
    const appInputs: AppInput[] = appUnits.map((unit) => {
      const input: AppInput = {
        name: unit.name,
        type: unit.type,
        port: unit.port,
        path: unit.path,
        framework: unit.framework,
        build: unit.build,
        dockerfile: unit.dockerfile,
        rendering: unit.rendering,
        env: unit.env,
        secrets: unit.secrets,
      };
      // Send replicas only when autoscaling is absent. Default (1) is not forwarded
      // so the API keeps backward-compat with payloads from older CLIs.
      if (unit.autoscaling) {
        input.autoscaling = {
          min: unit.autoscaling.min,
          max: unit.autoscaling.max,
          target_cpu: unit.autoscaling.targetCPU,
        };
      } else if (unit.replicas !== undefined && unit.replicas !== 1) {
        input.replicas = unit.replicas;
      }
      return input;
    });
    let buildResult: import('./deploy-types').SubmitBuildResult;
    try {
      buildResult = await this.deps.submitBuild({
        project: config.project,
        environment,
        apps: appInputs,
        idempotencyKey,
        commitSha: uploadResult.commit_sha,
        noCache: opts.noCache,
      });
    } catch (err) {
      return handleError(this.deps, opts, err, 'Deploy rejected');
    }

    // 14. --no-wait: return immediately (DA-2)
    if (!shouldWait) {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          build_id: buildResult.build_id,
          stream_url: buildResult.stream_url,
          status: 'submitted',
        }));
      } else {
        this.deps.writeTo(`Deploy submitted (${buildResult.build_id}). The deploy is running in the background.\nTrack with: loopzero logs --build ${buildResult.build_id}`);
      }
      return 0;
    }

    // 15. Connect SSE stream and wait for completion
    return this.streamBuildEvents(opts, buildResult, selectedApps);
  }

  /**
   * Connect to SSE stream and wait for build completion, handling all event types.
   * Returns the CLI exit code.
   */
  private streamBuildEvents(
    opts: import('./deploy-types').DeployOptions,
    buildResult: import('./deploy-types').SubmitBuildResult,
    selectedApps: string[],
  ): Promise<number> {
    const deployStartTime = Date.now();

    const renderer = new DeployRenderer({
      isCI: this.deps.isCI,
      isJSON: opts.json ?? false,
      isTTY: this.deps.isTTY ?? !this.deps.isCI,
    });

    return new Promise<number>((resolve) => {
      let resolved = false;
      let sseConnection: SSEConnection | null = null;
      let activityTimeout: ReturnType<typeof setTimeout>;

      const resetTimeout = (): void => {
        if (activityTimeout) clearTimeout(activityTimeout);
        activityTimeout = setTimeout(() => {
          if (!resolved) {
            if (opts.json) {
              this.deps.writeTo(JSON.stringify({ code: 'DEPLOY_TIMEOUT', error: 'Deploy timed out waiting for completion. Wait for it to complete, or try again later.', exit_code: 3 }));
            } else {
              this.deps.writeError('Deploy timed out waiting for completion. Wait for it to complete, or try again later.');
            }
            finish(3);
          }
        }, DEPLOY_TIMEOUT_MS);
      };

      const finish = async (exitCode: number): Promise<void> => {
        if (resolved) return;
        resolved = true;
        clearTimeout(activityTimeout);

        // BLOCKER-1: On stream-related failures, verify actual build state
        if (exitCode === 3 && this.deps.getBuildStatus) {
          try {
            const buildStatus = await this.deps.getBuildStatus(buildResult.build_id);
            if (buildStatus.status === 'SUCCEEDED') {
              exitCode = 0;
            } else if (buildStatus.status === 'FAILED') {
              exitCode = 2;
              if (!opts.json) {
                this.deps.writeError(`Build failed: ${buildStatus.error_message ?? 'unknown error'}`);
              }
            }
          } catch {
            // Can't reach API either — keep original exit code
          }
        }

        if (sseConnection) {
          sseConnection.close();
        }
        resolve(exitCode);
      };

      // Start the activity-based timeout
      resetTimeout();

      sseConnection = this.deps.connectSSE(buildResult.build_id, (event: SSEEvent) => {
        if (resolved) {
          return;
        }

        // Reset activity timeout on every received event (A-04)
        resetTimeout();

        const handled = this.handleSSEEvent(event, opts, buildResult, selectedApps, deployStartTime, renderer, finish);
        if (!handled) {
          // Render intermediate progress events via DeployRenderer
          const progressLine = renderer.onEvent({ type: event.type, data: event.data as Record<string, unknown> });
          if (progressLine) {
            this.deps.writeTo(progressLine);
          }
        }
      });
    });
  }

  /**
   * Handle a single SSE event. Returns true if the event was a terminal event
   * (deploy.completed, build.failed, etc.), false if it was a progress event.
   */
  private handleSSEEvent(
    event: SSEEvent,
    opts: import('./deploy-types').DeployOptions,
    buildResult: import('./deploy-types').SubmitBuildResult,
    selectedApps: string[],
    deployStartTime: number,
    renderer: DeployRenderer,
    finish: (exitCode: number) => void,
  ): boolean {
    if (event.type === 'deploy.completed') {
      const payload = event.data as DeployCompletedPayload;

      // F-04: Use wall-clock time when server reports 0 duration
      if (!payload.duration_seconds) {
        payload.duration_seconds = Math.ceil((Date.now() - deployStartTime) / 1000);
      }

      // F-08: Normalize empty/missing URLs to null for JSON output
      for (const app of payload.apps) {
        if (!app.url) {
          app.url = undefined;
        }
      }

      if (opts.json) {
        const jsonPayload = {
          ...payload,
          apps: payload.apps.map((app) => {
            const entry: Record<string, unknown> = {
              ...app,
              url: app.url ?? null,
            };
            if (app.status === 'failed') {
              entry.build_log = `loopzero logs --build ${buildResult.build_id}`;
            }
            return entry;
          }),
        };
        this.deps.writeTo(JSON.stringify(jsonPayload));
      } else {
        const result: DeployResult = {
          deploy_id: payload.deploy_id,
          status: payload.status,
          duration_seconds: payload.duration_seconds,
          apps: payload.apps,
        };
        this.deps.writeTo(renderer.renderResult(result));
      }
      finish(resolveExitCode(payload, selectedApps.length));
      return true;
    }

    if (event.type === 'build.failed') {
      const data = event.data as Record<string, unknown>;
      const rawReason = String(data.error ?? data.status ?? 'unknown error');
      const reason = sanitizeBuildError(rawReason);
      const suggestion = `Run: loopzero logs --build ${buildResult.build_id}`;
      const errorMsg = /^Build\s+(failed|error)/i.test(reason) ? reason : `Build failed: ${reason}`;
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'BUILD_FAILED',
          error: errorMsg,
          exit_code: 2,
          build_id: buildResult.build_id,
          suggestion,
        }));
      } else {
        this.deps.writeError(`${errorMsg}. ${suggestion}`);
      }
      finish(2);
      return true;
    }

    if (event.type === 'deploy.sync_failed') {
      const data = event.data as Record<string, unknown>;
      const rawReason = String(data.reason ?? 'sync failed');
      const userMessage = mapSyncFailureReason(rawReason, buildResult.build_id);
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'DEPLOY_FAILED',
          error: userMessage,
          reason: rawReason,
          exit_code: 3,
          deploy_id: data.deploy_id ?? null,
          build_id: buildResult.build_id,
        }));
      } else {
        this.deps.writeError(userMessage);
      }
      finish(3);
      return true;
    }

    if (event.type === 'stream.error') {
      const data = event.data as Record<string, unknown>;
      const reason = String(data.error ?? 'Stream connection failed');
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'STREAM_ERROR',
          error: `Deploy stream error: ${reason}`,
          exit_code: 3,
          build_id: buildResult.build_id,
        }));
      } else {
        this.deps.writeError(`Deploy stream error: ${reason}`);
      }
      finish(3);
      return true;
    }

    if (event.type === 'stream.closed') {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'STREAM_CLOSED',
          error: 'Deploy stream closed before completion. The build may still be running.',
          exit_code: 3,
          build_id: buildResult.build_id,
        }));
      } else {
        this.deps.writeError('Deploy stream closed before completion. The build may still be running.');
      }
      finish(3);
      return true;
    }

    return false;
  }
}

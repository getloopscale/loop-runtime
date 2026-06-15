// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { DeployRenderer, DeployResult } from '../output/deploy-renderer';
import {
  DEPLOY_TIMEOUT_MS,
  MAX_BUNDLE_SIZE_BYTES,
} from '../config/constants';
import { normalizeApps } from '../adapters/app-unit';
import { parseServicesRuntimes } from '../adapters/services-json';
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
import { maybeRenderAutoTail } from './deploy-auto-tail';
import { EXIT_CODES, buildStateToExitCode } from '../lib/exit-codes';

// Re-export types so existing consumers (tests, index.ts) keep working
export type { DeployDeps, DeployOptions } from './deploy-types';

/** Result of validatePrerequisites — carries validated state needed by subsequent phases. */
interface PrerequisiteResult {
  config: { project: string; apps: Record<string, unknown> };
  servicesJsonPath: string;
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

    const { config, servicesJsonPath, selectedApps, appUnits } = prereqResult;

    // Phase 2: Create source bundle and upload (traced)
    const uploadResult = await tracer.startActiveSpan('loopzero.cli.deploy.bundle', async (span) => {
      try {
        const result = await this.createAndUploadBundle(opts, config, servicesJsonPath);
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

    // Plan v1.1 T1.2 — discover `.loopzero/services.json` for the upload payload.
    // Pre-v1.1 the code passed `yamlResult.path` here, so the adapter was
    // shipping YAML bytes inside the `services_json` form field — Plan B v3.1
    // cutover gap. T3.1 wires auto-build into the null branch; for now we
    // surface an actionable error.
    if (!this.deps.findServicesJson) {
      return exitWithError(
        this.deps,
        opts,
        'Internal: findServicesJson dep missing (deploy registration regression).',
        2,
        'INTERNAL_DEPENDENCY_MISSING',
      );
    }
    let servicesJsonResult: { path: string } | null;
    try {
      servicesJsonResult = this.deps.findServicesJson(opts.servicesJson);
    } catch (err) {
      return exitWithError(this.deps, opts, (err as Error).message, 1, 'SERVICES_JSON_OVERRIDE_INVALID');
    }
    // Plan v1.1 T3.1 — auto-build branch. When services.json is missing AND
    // --skip-build / --prebuilt was NOT passed AND a runLoopZerokitBuild dep was
    // injected (deploy registration site), invoke it and re-discover.
    if (servicesJsonResult === null && opts.skipBuild !== true && this.deps.runLoopZerokitBuild) {
      if (!opts.json) {
        this.deps.writeTo('Building project with loopzerokit...');
      }
      try {
        await this.deps.runLoopZerokitBuild({ cwd: process.cwd(), target: 'loopzero-cloud' });
      } catch (err) {
        return handleError(this.deps, opts, err, 'loopzerokit build failed');
      }
      try {
        servicesJsonResult = this.deps.findServicesJson(opts.servicesJson);
      } catch (err) {
        return exitWithError(this.deps, opts, (err as Error).message, 1, 'SERVICES_JSON_OVERRIDE_INVALID');
      }
      // EC-10 absorbed — loopzerokit build returned 0 but emitted nothing.
      if (servicesJsonResult === null) {
        return exitWithError(
          this.deps,
          opts,
          'loopzerokit build exit 0 but no .loopzero/services.json was written. This is a bug in loopzerokit — file with output of: loopzerokit doctor',
          1,
          'LOOPZEROKIT_BUILD_EMPTY',
        );
      }
    }
    if (servicesJsonResult === null) {
      const hint = opts.skipBuild === true
        ? 'No `.loopzero/services.json` found and --skip-build/--prebuilt was passed. Run `loopzerokit build --target loopzero-cloud` first.'
        : 'No `.loopzero/services.json` found. Run `loopzerokit build --target loopzero-cloud` first.';
      return exitWithError(this.deps, opts, hint, 1, 'SERVICES_JSON_NOT_FOUND');
    }

    // T3.1a (F-dom-2): the build pipeline dispatches the build strategy on the
    // services.json `runtime` (python|node), not the legacy `framework`. Carry
    // the declared runtime onto each AppUnit from the discovered services.json
    // (matched by name) so it reaches the build submit payload. A local parse
    // failure is non-fatal — the API re-validates services.json on upload, and
    // an empty runtime fails closed clearly at build time (D4).
    try {
      const runtimeByName = parseServicesRuntimes(fs.readFileSync(servicesJsonResult.path, 'utf-8'));
      const unmatched: string[] = [];
      for (const unit of appUnits) {
        const rt = runtimeByName[unit.name];
        if (rt) {
          unit.runtime = rt;
        } else {
          unmatched.push(unit.name);
        }
      }
      // F-dom-1 (review polish): warn EARLY when an app has no matching
      // services.json runtime — otherwise the empty runtime is only caught after
      // a full upload + build (fail-closed). The CLI holds both name spaces, so
      // surface the mismatch in <1s. Suppressed in --json mode (machine output).
      if (unmatched.length > 0 && !opts.json) {
        const available = Object.keys(runtimeByName).sort().join(', ') || '(none)';
        this.deps.writeError(
          `Warning: no services.json runtime for app(s) [${unmatched.join(', ')}]. ` +
            `Available services: [${available}]. The build will fail unless each app ` +
            `has a matching service in .loopzero/services.json (run \`loopzerokit build\`).`,
        );
      }
    } catch {
      // non-fatal — services.json was re-validated on upload; empty runtime
      // fails closed clearly at build time (D4)
    }

    return {
      config,
      servicesJsonPath: servicesJsonResult.path,
      environment,
      selectedApps,
      appUnits,
    };
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
    servicesJsonPath: string,
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
      uploadResult = await this.deps.uploadSource(bundlePath, servicesJsonPath);
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
        runtime: unit.runtime || undefined, // F-dom-2: the build-strategy key the API dispatches on
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

        // BLOCKER-1: On stream-related failures, verify actual build state.
        // Plan T1.2 D3 — use EXIT_CODES + state-mapper instead of literals.
        if (exitCode === EXIT_CODES.DEPLOY_FAILED && this.deps.getBuildStatus) {
          try {
            const buildStatus = await this.deps.getBuildStatus(buildResult.build_id);
            exitCode = buildStateToExitCode(buildStatus.status);
            if (exitCode === EXIT_CODES.BUILD_FAILED && !opts.json) {
              this.deps.writeError(`Build failed: ${buildStatus.error_message ?? 'unknown error'}`);
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

        // handleSSEEvent is async because terminal failure branches
        // may await the best-effort auto-tail (Beta Logs Gate Criterion 4 UX).
        // Fire-and-forget: SSE callback is sync, so we kick off the
        // promise and let it resolve in the background. The activity
        // timeout is reset above so the auto-tail's 5s window doesn't
        // trigger a false timeout.
        void this.handleSSEEvent(event, opts, buildResult, selectedApps, deployStartTime, renderer, finish)
          .then((handled) => {
            if (!handled) {
              // Render intermediate progress events via DeployRenderer
              const progressLine = renderer.onEvent({ type: event.type, data: event.data as Record<string, unknown> });
              if (progressLine) {
                this.deps.writeTo(progressLine);
              }
            }
          });
      });
    });
  }

  /**
   * Handle a single SSE event. Returns true if the event was a terminal event
   * (deploy.completed, build.failed, etc.), false if it was a progress event.
   *
   * Async because failure branches may await the best-effort auto-tail
   * (Beta Logs Gate Criterion 4 UX) — bounded by AUTO_TAIL_TIMEOUT_MS.
   */
  private async handleSSEEvent(
    event: SSEEvent,
    opts: import('./deploy-types').DeployOptions,
    buildResult: import('./deploy-types').SubmitBuildResult,
    selectedApps: string[],
    deployStartTime: number,
    renderer: DeployRenderer,
    finish: (exitCode: number) => void,
  ): Promise<boolean> {
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
      // Beta Logs Gate Criterion 4 (UX) — best-effort auto-tail.
      // Non-blocking 5s timeout; if Loki has indexed any output for
      // this build, the operator sees recent context inline.
      const tailLines = await maybeRenderAutoTail(
        this.deps.fetchRecentBuildLogs,
        buildResult.build_id,
        this.deps.writeTo,
        { jsonMode: opts.json },
      );
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'BUILD_FAILED',
          error: errorMsg,
          exit_code: 2,
          build_id: buildResult.build_id,
          suggestion,
          recent_logs: tailLines,
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
      const tailLines = await maybeRenderAutoTail(
        this.deps.fetchRecentBuildLogs,
        buildResult.build_id,
        this.deps.writeTo,
        { jsonMode: opts.json },
      );
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'DEPLOY_FAILED',
          error: userMessage,
          reason: rawReason,
          exit_code: 3,
          deploy_id: data.deploy_id ?? null,
          build_id: buildResult.build_id,
          recent_logs: tailLines,
        }));
      } else {
        this.deps.writeError(userMessage);
      }
      finish(3);
      return true;
    }

    // T4.1 of deep-review-2026-05-10-remediation-plan (find_cli_missing_event_handler HIGH):
    // The API emits `deploy.delivery_failed` when manifests are built and
    // signed but cannot be delivered (Gitea push fail, ArgoCD App create
    // fail, etc). Pre-T4.1 the CLI ignored the event and hung waiting for
    // a `deploy.completed` that never came. Now we surface it with exit 3
    // (deploy failure per Rule 4) and a product-language message.
    if (event.type === 'deploy.delivery_failed') {
      const data = event.data as Record<string, unknown>;
      const reason = String(data.reason ?? 'manifests could not be delivered to the cluster');
      const userMessage = `Deploy failed: ${reason}. Build artifacts were created but the cluster could not be updated.`;
      const tailLines = await maybeRenderAutoTail(
        this.deps.fetchRecentBuildLogs,
        buildResult.build_id,
        this.deps.writeTo,
        { jsonMode: opts.json },
      );
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'DEPLOY_FAILED',
          error: userMessage,
          reason,
          exit_code: 3,
          deploy_id: data.deploy_id ?? null,
          build_id: buildResult.build_id,
          recent_logs: tailLines,
        }));
      } else {
        this.deps.writeError(userMessage);
      }
      finish(3);
      return true;
    }

    // argo-rollouts-progressive-delivery-plan T4.2 — Canary progress events.
    //
    // rollout.step is a progress event (NON-terminal). Stream stays open;
    // CLI shows current canary weight/step. The renderer uses product
    // language only (Rule 3): "Canary deployment" / "Progressive deploy" —
    // never "Rollout", "Argo Rollouts", "TraefikService".
    if (event.type === 'rollout.step') {
      const data = event.data as Record<string, unknown>;
      const weight = typeof data.weight === 'number' ? data.weight : 0;
      const step = typeof data.step === 'number' ? data.step : 0;
      const totalSteps = typeof data.total_steps === 'number' ? data.total_steps : 0;
      const status = typeof data.status === 'string' ? data.status : 'progressing';
      // JSON mode: stream a progress event line per Rule 5.
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          event: 'canary.progress',
          weight,
          step,
          total_steps: totalSteps,
          status,
        }));
      } else {
        // Human mode: spinner-friendly progress line.
        const stepLabel = totalSteps > 0 ? `step ${step}/${totalSteps}` : `step ${step}`;
        this.deps.writeTo(`Canary deployment: ${weight}% (${stepLabel}) — ${status}`);
      }
      return false; // non-terminal: keep stream open.
    }

    // rollout.aborted is TERMINAL: canary was auto-aborted (analysis failed,
    // pod unhealthy, or user-aborted). CLI exits with code 3 (deploy failure).
    if (event.type === 'rollout.aborted') {
      const data = event.data as Record<string, unknown>;
      const reason = typeof data.reason === 'string' ? data.reason : 'unknown';
      const message = typeof data.message === 'string' ? data.message : '';
      const weight = typeof data.weight === 'number' ? data.weight : 0;
      const userMessage = message
        ? `Canary deployment aborted at ${weight}%: ${message}`
        : `Canary deployment aborted at ${weight}% (reason: ${reason})`;
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'CANARY_ABORTED',
          error: userMessage,
          reason,
          weight,
          exit_code: 3,
          build_id: buildResult.build_id,
        }));
      } else {
        this.deps.writeError(userMessage);
        this.deps.writeError(`Run: loopzero logs --build ${buildResult.build_id}`);
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
      // CHG-2026-04-23-AUDIT-32: point the operator at the canonical
      // source of truth for build state instead of making a claim
      // about unknown state ("may still be running"). The CLI already
      // knows build_id — surface it in the actionable hint.
      const actionable = `Deploy stream closed before completion. Check \`loopzero build inspect ${buildResult.build_id}\` for current state.`;
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          code: 'STREAM_CLOSED',
          error: actionable,
          exit_code: 3,
          build_id: buildResult.build_id,
        }));
      } else {
        this.deps.writeError(actionable);
      }
      finish(3);
      return true;
    }

    return false;
  }
}

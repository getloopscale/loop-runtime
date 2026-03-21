import { ApiError } from '../api/client';
import { DeployRenderer, DeployResult } from '../output/deploy-renderer';
import {
  DEPLOY_TIMEOUT_MS,
  MAX_ENVIRONMENT_NAME_LENGTH,
  MAX_BUNDLE_SIZE_BYTES,
} from '../config/constants';
import * as fs from 'fs';
import * as path from 'path';

interface LoopZeroYamlApp {
  path: string;
  framework: string;
  type?: string;  // "server" | "worker" | "frontend"
  port: number | null;
  secrets?: string[];
}

interface LoopZeroYaml {
  project: string;
  apps: Record<string, LoopZeroYamlApp>;
}

interface LoopZeroYamlResult {
  path: string;
  content: LoopZeroYaml;
  parseError?: string;
}

interface UploadResult {
  commit_sha: string;
  apps: string[];
}

interface AppInput {
  name: string;
  type?: string;  // "server" | "worker" | "frontend"
  port?: number;
  path?: string;
  framework?: string;
  secrets?: string[];  // declared secret names from loopzero.yaml
}

interface SubmitBuildInput {
  project: string;
  environment: string;
  apps: AppInput[];
  idempotencyKey: string;
  commitSha: string;
}

interface SubmitBuildResult {
  build_id: string;
  stream_url: string;
}

interface SSEEvent {
  type: string;
  data: DeployCompletedPayload | Record<string, unknown>;
}

interface SSEConnection {
  close: () => void;
}

interface AppResult {
  name: string;
  status: string;
  url?: string;
  build_duration_seconds: number;
  error?: string;
}

interface DeployCompletedPayload {
  deploy_id: string;
  status: string;
  duration_seconds: number;
  apps: AppResult[];
}

interface BuildStatus {
  state: string;
  error_message?: string;
}

export interface DeployDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  hasGitRepo: () => boolean;
  hasUncommittedChanges: () => boolean;
  hasLockfile: () => boolean;
  getHeadCommit: () => string;
  createGitBundle: () => Promise<string>;
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
}

export class DeployCommand {
  private readonly deps: DeployDeps;

  constructor(deps: DeployDeps) {
    this.deps = deps;
  }

  async execute(opts: DeployOptions): Promise<number> {
    const environment = opts.env ?? 'production';
    const shouldWait = opts.wait !== false;

    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Validate .git exists
    if (!this.deps.hasGitRepo()) {
      return this.exitWithError(opts, 'Not a git repository. Run this command from your project directory.', 1, 'NOT_GIT_REPO');
    }

    // 3. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    // 3b. Check for YAML parse errors
    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1, 'INVALID_CONFIG');
    }

    const { content: config } = yamlResult;

    // 4. Validate apps exist
    const appNames = Object.keys(config.apps);
    if (appNames.length === 0) {
      return this.exitWithError(opts, 'No apps defined in loopzero.yaml. Add at least one app to deploy.', 1, 'INVALID_CONFIG');
    }

    // 5. Validate --env (DA-10)
    const envError = this.validateEnvironment(environment);
    if (envError) {
      return this.exitWithError(opts, envError, 1, 'INVALID_ENVIRONMENT');
    }

    // 6. Validate --app flag (DA-19) — fail fast before any upload
    const selectedApps = opts.app ?? appNames;
    for (const app of selectedApps) {
      if (!appNames.includes(app)) {
        return this.exitWithError(opts, `App "${app}" not found in loopzero.yaml. Available apps: ${appNames.join(', ')}`, 1, 'APP_NOT_FOUND');
      }
    }

    // 7. Validate secrets (pre-deploy check) — scoped to selected apps only (F-02)
    const declaredSecrets = this.collectDeclaredSecrets(config, selectedApps);
    if (declaredSecrets.length > 0) {
      let configuredSecrets: string[];
      try {
        configuredSecrets = await this.deps.getSecrets(config.project, environment);
      } catch {
        // MOD-6: Network error during secrets check — warn and continue
        // The build will fail if secrets are truly missing.
        if (!opts.json) {
          this.deps.writeTo('Warning: Could not verify secrets. Deploy will continue but may fail if secrets are missing.');
        }
        configuredSecrets = declaredSecrets; // skip validation
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
    }

    // 8. Warn about uncommitted changes
    if (this.deps.hasUncommittedChanges()) {
      if (!opts.json) {
        this.deps.writeTo('Warning: You have uncommitted changes. Only committed code will be deployed.');
      }
    }

    // 8b. Warn about missing lockfile (F-03: fail-fast before upload)
    if (!this.deps.hasLockfile()) {
      // F-04: Also check inside app paths before warning (lockfile may be in app directory, not root)
      const appPaths = Object.values(config.apps).map(a => a.path).filter(p => p && p !== '.');
      const hasAppLockfile = appPaths.some(appPath => {
        return ['package-lock.json', 'yarn.lock', 'pnpm-lock.yaml'].some(
          f => fs.existsSync(path.join(appPath, f))
        );
      });
      if (!hasAppLockfile && !opts.json) {
        this.deps.writeTo('Warning: No lockfile found (package-lock.json, yarn.lock, or pnpm-lock.yaml). Build may fail. Run: npm install');
      }
    }

    // 9. Validate git has at least one commit (HEAD must exist for bundle creation)
    try {
      this.deps.getHeadCommit();
    } catch {
      return this.exitWithError(opts,
        'No commits found. Commit your changes first: git add . && git commit -m "initial commit"',
        1, 'NO_COMMITS');
    }

    // 9b. Warn about potentially sensitive files in git (F-04)
    const sensitiveFiles = this.deps.checkSensitiveFiles?.() ?? [];
    if (sensitiveFiles.length > 0 && !opts.json) {
      this.deps.writeTo(`Warning: Potentially sensitive files detected in git: ${sensitiveFiles.join(', ')}`);
      this.deps.writeTo('These files will be included in the deploy bundle.');
    }

    // 10. Create git bundle
    let bundlePath: string;
    try {
      bundlePath = await this.deps.createGitBundle();
    } catch (err) {
      return this.exitWithError(opts, `Failed to create source bundle. ${(err as Error).message}`, 1, 'BUNDLE_FAILED');
    }

    // 10b. Validate bundle size before upload (MOD-5)
    try {
      const stat = fs.statSync(bundlePath);
      if (stat.size > MAX_BUNDLE_SIZE_BYTES) {
        this.deps.cleanupFile(bundlePath);
        const sizeMB = Math.ceil(stat.size / (1024 * 1024));
        return this.exitWithError(opts,
          `Source bundle too large (${sizeMB}MB, max 500MB). Reduce repository size or use .gitignore to exclude large files.`,
          1, 'BUNDLE_TOO_LARGE');
      }
    } catch {
      // Can't stat — proceed anyway, server will reject if too large
    }

    // 11. Upload source
    let uploadResult: UploadResult;
    try {
      uploadResult = await this.deps.uploadSource(bundlePath, yamlResult.path);
    } catch (err) {
      this.deps.cleanupFile(bundlePath);
      return this.handleError(opts, err, 'Upload rejected');
    }

    // 12. Cleanup temp file
    this.deps.cleanupFile(bundlePath);

    // 13. Generate deterministic Idempotency-Key (A-01)
    const idempotencyKey = this.deps.generateIdempotencyKey(
      uploadResult.commit_sha,
      config.project,
      environment,
      selectedApps,
    );
    const appInputs: AppInput[] = selectedApps.map((name) => ({
      name,
      type: this.resolveAppType(config.apps[name]),
      port: config.apps[name]?.port ?? undefined,
      path: config.apps[name]?.path ?? undefined,
      framework: config.apps[name]?.framework ?? undefined,
      secrets: config.apps[name]?.secrets ?? undefined,
    }));
    let buildResult: SubmitBuildResult;
    try {
      buildResult = await this.deps.submitBuild({
        project: config.project,
        environment,
        apps: appInputs,
        idempotencyKey,
        commitSha: uploadResult.commit_sha,
      });
    } catch (err) {
      return this.handleError(opts, err, 'Deploy rejected');
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
        this.deps.writeTo(`Deploy submitted (${buildResult.build_id}). The deploy is running in the background.`);
      }
      return 0;
    }

    // 15. Connect SSE stream and wait for completion
    // DEPLOY_TIMEOUT_MS imported from config/constants
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

        // BLOCKER-1: On stream-related failures, verify actual build state
        // before reporting failure. The build may have completed successfully
        // even though the stream broke.
        if (exitCode === 3 && this.deps.getBuildStatus) {
          try {
            const status = await this.deps.getBuildStatus(buildResult.build_id);
            if (status.state === 'SUCCEEDED') {
              exitCode = 0;
            } else if (status.state === 'FAILED') {
              exitCode = 2;
              if (!opts.json) {
                this.deps.writeError(`Build failed: ${status.error_message ?? 'unknown error'}`);
              }
            }
            // If still RUNNING/QUEUED, keep exit code 3 (unknown)
          } catch {
            // Can't reach API either — keep original exit code
          }
        }

        resolved = true;
        clearTimeout(activityTimeout);
        if (sseConnection) {
          sseConnection.close();
        }
        resolve(exitCode);
      };

      // Start the activity-based timeout — resets on every SSE event
      resetTimeout();

      sseConnection = this.deps.connectSSE(buildResult.build_id, (event: SSEEvent) => {
        if (resolved) {
          return;
        }

        // Reset activity timeout on every received event (A-04)
        resetTimeout();

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
                // F-04: Add build_log pointer for failed apps per spec §0.3
                if (app.status === 'failed') {
                  entry.build_log = `loopzero logs --build ${buildResult.build_id}`;
                }
                return entry;
              }),
            };
            this.deps.writeTo(JSON.stringify(jsonPayload));
            finish(this.resolveExitCode(payload, selectedApps.length));
            return;
          }

          // LEVE-2: Use DeployRenderer for final output (single renderer, no duplication)
          const result: DeployResult = {
            deploy_id: payload.deploy_id,
            status: payload.status,
            duration_seconds: payload.duration_seconds,
            apps: payload.apps,
          };
          this.deps.writeTo(renderer.renderResult(result));
          finish(this.resolveExitCode(payload, selectedApps.length));
          return;
        }

        if (event.type === 'build.failed') {
          const data = event.data as Record<string, unknown>;
          const rawReason = String(data.error ?? data.status ?? 'unknown error');
          const reason = this.sanitizeBuildError(rawReason);
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ code: 'BUILD_FAILED', error: `Build failed: ${reason}`, exit_code: 2 }));
          } else {
            this.deps.writeError(`Build failed: ${reason}`);
          }
          finish(2);
          return;
        }

        if (event.type === 'deploy.sync_failed') {
          const data = event.data as Record<string, unknown>;
          const rawReason = String(data.reason ?? 'sync failed');
          const userMessage = this.mapSyncFailureReason(rawReason, buildResult.build_id);
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
          finish(3); // exit code 3 = deploy failure
          return;
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
          return;
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
          return;
        }

        // Render intermediate progress events via DeployRenderer
        const progressLine = renderer.onEvent({ type: event.type, data: event.data as Record<string, unknown> });
        if (progressLine) {
          this.deps.writeTo(progressLine);
        }
      });
    });
  }

  private resolveExitCode(payload: DeployCompletedPayload, requestedAppCount: number): number {
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

  private collectDeclaredSecrets(config: LoopZeroYaml, selectedApps: string[]): string[] {
    const secretsSet = new Set<string>();
    for (const appName of selectedApps) {
      const app = config.apps[appName];
      if (app?.secrets) {
        for (const secret of app.secrets) {
          secretsSet.add(secret);
        }
      }
    }
    return Array.from(secretsSet);
  }

  private resolveAppType(app: LoopZeroYamlApp | undefined): string {
    if (!app) return 'server';
    // Use explicit type if set
    if (app.type) return app.type;
    // Infer type from framework/port as fallback
    const framework = (app.framework || '').toLowerCase();
    if (framework === 'next' || framework === 'react' || framework === 'vue' || framework === 'svelte') {
      return 'frontend';
    }
    if (app.port === null || app.port === undefined) {
      return 'worker';
    }
    return 'server';
  }

  private validateEnvironment(env: string): string | null {
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

  private mapSyncFailureReason(reason: string, buildId: string): string {
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

  private sanitizeBuildError(raw: string): string {
    // Map internal infrastructure terms to user-facing language (Rule 3)
    let msg = raw;
    msg = msg.replace(/Workflow\s+(Failed|Error|Timed\s*Out)/gi, 'Build $1');
    msg = msg.replace(/\bin\s+build\s+cluster\b/gi, '');
    msg = msg.replace(/\bworkflow\b/gi, 'build');
    msg = msg.replace(/\bArgo\s*CD\b/gi, 'deployment sync');
    msg = msg.replace(/\bGitea\b/gi, 'repository');
    msg = msg.replace(/\bMinIO\b/gi, 'storage');
    msg = msg.replace(/\bKaniko\b/gi, 'image builder');
    // Clean up extra whitespace
    msg = msg.replace(/\s{2,}/g, ' ').trim();
    return msg || 'unknown error';
  }

  private exitWithError(opts: DeployOptions, message: string, exitCode: number, code?: string): number {
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

  private handleError(opts: DeployOptions, err: unknown, context?: string): number {
    if (err instanceof ApiError) {
      const message = context ? `${context}: ${err.message}` : err.message;
      return this.exitWithError(opts, message, err.exitCode, err.code);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3, 'UNEXPECTED_ERROR');
    }
    return this.exitWithError(opts, 'Unexpected error. Wait for it to complete, or try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

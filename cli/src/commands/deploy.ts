import { ApiError } from '../api/client';
import { DeployRenderer, DeployRendererOptions } from '../output/deploy-renderer';

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

export interface DeployDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  hasGitRepo: () => boolean;
  hasUncommittedChanges: () => boolean;
  getHeadCommit: () => string;
  createGitBundle: () => Promise<string>;
  uploadSource: (bundlePath: string, loopzeroYamlPath: string) => Promise<UploadResult>;
  submitBuild: (input: SubmitBuildInput) => Promise<SubmitBuildResult>;
  connectSSE: (buildId: string, onEvent: (event: SSEEvent) => void) => SSEConnection;
  getSecrets: (project: string, environment: string) => Promise<string[]>;
  cleanupFile: (filePath: string) => void;
  generateIdempotencyKey: () => string;
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
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4);
    }

    // 2. Validate .git exists
    if (!this.deps.hasGitRepo()) {
      return this.exitWithError(opts, 'Run from inside a git repository.', 1);
    }

    // 3. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1);
    }

    // 3b. Check for YAML parse errors
    if (yamlResult.parseError) {
      return this.exitWithError(opts, `Invalid loopzero.yaml: ${yamlResult.parseError}`, 1);
    }

    const { content: config } = yamlResult;

    // 4. Validate apps exist
    const appNames = Object.keys(config.apps);
    if (appNames.length === 0) {
      return this.exitWithError(opts, 'No apps defined in loopzero.yaml. Add at least one app to deploy.', 1);
    }

    // 5. Validate --env (DA-10)
    const envError = this.validateEnvironment(environment);
    if (envError) {
      return this.exitWithError(opts, envError, 1);
    }

    // 6. Validate --app flag (DA-19) — fail fast before any upload
    const selectedApps = opts.app ?? appNames;
    for (const app of selectedApps) {
      if (!appNames.includes(app)) {
        return this.exitWithError(opts, `App "${app}" not found in loopzero.yaml. Available apps: ${appNames.join(', ')}`, 1);
      }
    }

    // 7. Validate secrets (pre-deploy check)
    const declaredSecrets = this.collectDeclaredSecrets(config);
    if (declaredSecrets.length > 0) {
      const configuredSecrets = await this.deps.getSecrets(config.project, environment);
      const missingSecrets = declaredSecrets.filter((s) => !configuredSecrets.includes(s));
      if (missingSecrets.length > 0) {
        const msg = `Missing secrets for env "${environment}": ${missingSecrets.join(', ')}`;
        const hint = 'Set secrets before deploying. See: https://docs.loopzero.example/secrets';
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ error: `${msg}. ${hint}`, exit_code: 1 }));
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

    // 9. Get HEAD commit
    const commit = this.deps.getHeadCommit();

    // 10. Create git bundle
    let bundlePath: string;
    try {
      bundlePath = await this.deps.createGitBundle();
    } catch (err) {
      return this.exitWithError(opts, `Failed to create source bundle. ${(err as Error).message}`, 1);
    }

    // 11. Upload source
    let uploadResult: UploadResult;
    try {
      uploadResult = await this.deps.uploadSource(bundlePath, yamlResult.path);
    } catch (err) {
      this.deps.cleanupFile(bundlePath);
      return this.handleError(opts, err);
    }

    // 12. Cleanup temp file
    this.deps.cleanupFile(bundlePath);

    // 13. Generate Idempotency-Key and submit build
    const idempotencyKey = this.deps.generateIdempotencyKey();
    const appInputs: AppInput[] = selectedApps.map((name) => ({
      name,
      type: this.resolveAppType(config.apps[name]),
      port: config.apps[name]?.port ?? undefined,
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
      return this.handleError(opts, err);
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
        this.deps.writeTo(`Deploy started (${buildResult.build_id}). Wait for it to complete, or try again later.`);
      }
      return 0;
    }

    // 15. Connect SSE stream and wait for completion
    const DEPLOY_TIMEOUT_MS = 5 * 60 * 1000; // 5 minutes
    const deployStartTime = Date.now();

    const renderer = new DeployRenderer({
      isCI: this.deps.isCI,
      isJSON: opts.json ?? false,
      isTTY: this.deps.isTTY ?? !this.deps.isCI,
    });

    return new Promise<number>((resolve) => {
      let resolved = false;
      let sseConnection: SSEConnection | null = null;

      const finish = (exitCode: number): void => {
        if (resolved) return;
        resolved = true;
        if (sseConnection) {
          sseConnection.close();
        }
        resolve(exitCode);
      };

      const timeoutId = setTimeout(() => {
        if (!resolved) {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ error: 'Deploy timed out waiting for completion. Wait for it to complete, or try again later.', exit_code: 3 }));
          } else {
            this.deps.writeError('Deploy timed out waiting for completion. Wait for it to complete, or try again later.');
          }
          finish(3);
        }
      }, DEPLOY_TIMEOUT_MS);

      sseConnection = this.deps.connectSSE(buildResult.build_id, (event: SSEEvent) => {
        if (event.type === 'deploy.completed') {
          clearTimeout(timeoutId);
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
              apps: payload.apps.map((app) => ({
                ...app,
                url: app.url ?? null,
              })),
            };
            this.deps.writeTo(JSON.stringify(jsonPayload));
            finish(this.resolveExitCode(payload, selectedApps.length));
            return;
          }

          this.displayResult(payload);
          finish(this.resolveExitCode(payload, selectedApps.length));
          return;
        }

        if (event.type === 'build.failed') {
          clearTimeout(timeoutId);
          const data = event.data as Record<string, unknown>;
          const reason = String(data.error ?? data.status ?? 'unknown error');
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ error: `Build failed: ${reason}`, exit_code: 2 }));
          } else {
            this.deps.writeError(`Build failed: ${reason}`);
          }
          finish(2);
          return;
        }

        if (event.type === 'deploy.sync_failed') {
          clearTimeout(timeoutId);
          const data = event.data as Record<string, unknown>;
          const reason = String(data.reason ?? 'sync failed');
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({
              error: `Deploy failed: ${reason}`,
              exit_code: 3,
              deploy_id: data.deploy_id ?? null,
            }));
          } else {
            this.deps.writeError(`Deploy failed: ${reason}`);
          }
          finish(3); // exit code 3 = deploy failure
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

  private displayResult(payload: DeployCompletedPayload): void {
    if (payload.status === 'success') {
      this.deps.writeTo(`Deploy completed in ${payload.duration_seconds}s`);
      for (const app of payload.apps) {
        if (app.url) {
          this.deps.writeTo(`  ${app.name}: ${app.url}`);
        } else {
          this.deps.writeTo(`  ${app.name}: deployed`);
        }
      }
    } else {
      this.deps.writeError(`Deploy ${payload.status}`);
      for (const app of payload.apps) {
        if (app.status === 'failed' && app.error) {
          this.deps.writeError(`  ${app.name}: ${app.error}`);
        } else if (app.url) {
          this.deps.writeTo(`  ${app.name}: ${app.url}`);
        } else {
          this.deps.writeTo(`  ${app.name}: deployed`);
        }
      }
    }
  }

  private collectDeclaredSecrets(config: LoopZeroYaml): string[] {
    const secretsSet = new Set<string>();
    for (const app of Object.values(config.apps)) {
      if (app.secrets) {
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
      return 'Environment names must be lowercase.';
    }
    if (!/^[a-z]/.test(env)) {
      return 'Environment name must start with a lowercase letter.';
    }
    if (env.length > 63) {
      return `Environment name too long (${env.length} chars, max 63).`;
    }
    if (!/^[a-z][a-z0-9-]*$/.test(env)) {
      return 'Environment name can only contain lowercase letters, numbers, and hyphens.';
    }
    return null;
  }

  private exitWithError(opts: DeployOptions, message: string, exitCode: number): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: DeployOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode);
    }
    if (err instanceof Error) {
      return this.exitWithError(opts, `Unexpected error: ${err.message}`, 3);
    }
    return this.exitWithError(opts, 'Unexpected error. Wait for it to complete, or try again later.', 3);
  }
}

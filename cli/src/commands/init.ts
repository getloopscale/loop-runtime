// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as path from 'path';
import { ApiError } from '../api/client';
import { FRONTEND_FRAMEWORKS } from '../adapters/app-unit';

interface MonorepoResult {
  tool: string;
  workspaces: string[];
}

interface FrameworkResult {
  framework: string;
  port: number | null;
}

interface CreateProjectResult {
  id: string;
  name: string;
  team: string;
}

export interface InitDeps {
  getToken: () => string | null;
  getTeam: () => string | null;
  hasGitRepo?: () => boolean;
  detectMonorepo: (dir: string) => MonorepoResult | null;
  detectFramework: (dir: string) => FrameworkResult;
  createProject: (input: { name: string; apps: AppSpec[] }) => Promise<CreateProjectResult>;
  // F-05 (CHG-2026-04-29-DOGFOOD T1.3) — fail-fast: validar token contra a API
  // antes de escrever loopzero.yaml. Se 401/403 → exit 4 sem artifacts órfãos.
  // Se network/5xx → exit 5 (partial) — usuário pode forçar local-only com
  // `--offline` flag. Quando offline, esta dep não é chamada.
  preflightAuth?: () => Promise<{ ok: boolean; status?: number; reason?: string }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  cwd: string;
  confirm?: (question: string) => Promise<boolean>;
  promptText?: (question: string, defaultValue: string) => Promise<string>;
}

interface AppSpec {
  name: string;
  framework: string;
  port: number | null;
  path: string;
  type?: 'server' | 'worker' | 'frontend';
}

interface DetectedAppInfo {
  path: string;
  framework: string;
  type: string;
  port: number | null;
}

interface DetectedInfo {
  tool: string;
  shared_packages: string[];
  apps: DetectedAppInfo[];
}

// Frameworks that are frontends — they serve static assets, not APIs
// MEET-2026-03-29-016: Frontends are ALWAYS static delivery (zero pods).
// Remix is SSR-first — classified as server, not frontend.
// FRONTEND_FRAMEWORKS imported from adapters/app-unit.ts (single source of truth)

// Well-known directory names for shared packages (not deployable)
const PACKAGE_DIR_PATTERNS = ['packages/', 'libs/', 'shared/', 'common/', 'config/'];

export interface InitOptions {
  yes?: boolean;
  force?: boolean;
  name?: string;
  json?: boolean;
  // F-05 (CHG-2026-04-29-DOGFOOD T1.3) — opt-in para criar loopzero.yaml sem
  // validar token contra a API. Útil em CI bootstrap onde a API ainda não
  // está reachable. Em modo offline, init não chama preflightAuth.
  offline?: boolean;
}

export class InitCommand {
  private readonly deps: InitDeps;

  constructor(deps: InitDeps) {
    this.deps = deps;
  }

  async execute(opts: InitOptions): Promise<number> {
    // Auth check (token presence)
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // F-05 (CHG-2026-04-29-DOGFOOD T1.3) — validar que o token É aceito pela
    // API ANTES de escrever loopzero.yaml. Anti-pattern silent-success (Rule 8):
    // sem este check, `init` com token expirado completava com warning e
    // exit 5; usuário pensava que projeto fora criado e o `loopzero deploy`
    // seguinte falhava com erro críptico de project_not_found.
    if (!opts.offline && this.deps.preflightAuth) {
      const auth = await this.deps.preflightAuth();
      if (!auth.ok) {
        if (auth.status === 401 || auth.status === 403) {
          const msg = 'Authentication failed. Your token may be expired, revoked, or missing required scopes.\n' +
            'Run: loopzero login --token <new-token>\n' +
            'Or use --offline to create loopzero.yaml without validating against the API.';
          return this.exitWithError(opts, msg, 4, 'AUTH_FAILED');
        }
        // 5xx, network, timeout → partial. Sugere --offline.
        const msg = (auth.reason || 'API unreachable.') +
          ' Use --offline to create loopzero.yaml without server validation.';
        return this.exitWithError(opts, msg, 5, 'PREFLIGHT_FAILED');
      }
    }

    // Git repo check — warn if not a git repository
    const noGitWarning = (this.deps.hasGitRepo && !this.deps.hasGitRepo())
      ? 'No git repository detected. Deploys will track file changes instead of commits. Run `git init` to enable git-based versioning.'
      : undefined;

    // CI mode requires --yes
    if (this.deps.isCI && !opts.yes) {
      return this.exitWithError(opts, 'CI mode requires --yes flag. Run: loopzero init --yes', 1, 'CI_REQUIRES_YES');
    }

    // Check if loopzero.yaml already exists
    const yamlPath = path.join(this.deps.cwd, 'loopzero.yaml');
    if (fs.existsSync(yamlPath) && !opts.force) {
      return this.exitWithError(opts, 'loopzero.yaml already exists. Use --force to overwrite.', 1, 'FILE_EXISTS');
    }

    // Phase 1: Detect monorepo structure and build app list
    let projectName = opts.name || path.basename(this.deps.cwd);
    const originalName = projectName;
    projectName = this.sanitizeProjectName(projectName);

    const sanitizationWarning = this.buildSanitizationWarning(projectName, originalName);
    if (sanitizationWarning && opts.name && !opts.json) {
      this.deps.writeTo(sanitizationWarning);
    }

    const { apps, detected } = this.detectAndBuildApps(opts, projectName);

    // Interactive wizard: ask for confirmation when not using --yes
    if (!opts.yes && this.deps.confirm) {
      const confirmed = await this.deps.confirm('Create project? (Y/n)');
      if (!confirmed) {
        return 0;
      }
    }

    // Interactive wizard: prompt for project name when not provided
    if (!opts.yes && !opts.name && this.deps.promptText) {
      projectName = await this.deps.promptText(
        `Project name (default: ${path.basename(this.deps.cwd)}):`,
        path.basename(this.deps.cwd)
      );
    }

    // Phase 2: Generate config, register project, emit output
    return this.createProjectAndEmitOutput(opts, yamlPath, projectName, originalName, apps, detected, sanitizationWarning, noGitWarning);
  }

  /**
   * Build a sanitization warning message if the project name was modified.
   * Returns undefined if no sanitization occurred.
   */
  private buildSanitizationWarning(sanitized: string, original: string): string | undefined {
    if (sanitized === original) {
      return undefined;
    }
    if (original.length > 63) {
      return `Project name truncated to 63 characters: "${original}" → "${sanitized}"`;
    }
    return `Project name sanitized: "${original}" → "${sanitized}" (RFC 1123: lowercase, letters/numbers/hyphens, max 63 chars)`;
  }

  /**
   * Detect monorepo structure and scan for deployable apps.
   * Returns the app list and optional detection metadata for JSON output.
   */
  private detectAndBuildApps(
    opts: InitOptions,
    projectName: string,
  ): { apps: AppSpec[]; detected?: DetectedInfo } {
    const monorepo = this.deps.detectMonorepo(this.deps.cwd);

    if (monorepo) {
      return this.detectMonorepoApps(opts, monorepo);
    }
    return this.detectSingleProjectApps(projectName);
  }

  /**
   * Detect apps from a monorepo workspace structure.
   */
  private detectMonorepoApps(
    opts: InitOptions,
    monorepo: MonorepoResult,
  ): { apps: AppSpec[]; detected: DetectedInfo } {
    const apps: AppSpec[] = [];
    const deployable: string[] = [];
    const skipped: string[] = [];

    for (const workspace of monorepo.workspaces) {
      const isPackage = PACKAGE_DIR_PATTERNS.some(p => workspace.startsWith(p));
      if (isPackage) {
        skipped.push(workspace);
      } else {
        deployable.push(workspace);
      }
    }

    const detected: DetectedInfo = {
      tool: monorepo.tool,
      shared_packages: skipped.map(s => path.basename(s)),
      apps: [],
    };

    if (!opts.json) {
      this.deps.writeTo(`Detected: ${monorepo.tool} monorepo`);
      if (skipped.length > 0) {
        this.deps.writeTo(`  Shared packages (not deployed): ${skipped.map(s => path.basename(s)).join(', ')}`);
      }
    }

    for (const workspace of deployable) {
      const wsDir = path.join(this.deps.cwd, workspace);
      const fw = this.deps.detectFramework(wsDir);
      const appName = path.basename(workspace);
      const appType = this.inferAppType(appName, fw.framework, fw.port);

      apps.push({
        name: appName,
        framework: fw.framework,
        port: appType === 'server' ? fw.port : null,
        path: workspace,
        type: appType,
      });

      detected.apps.push({
        path: workspace,
        framework: fw.framework,
        type: appType,
        port: appType === 'server' ? fw.port : null,
      });

      if (!opts.json) {
        const label = appType === 'server'
          ? `${fw.framework} (server, port ${fw.port})`
          : `${fw.framework} (${appType})`;
        this.deps.writeTo(`  ${workspace} → ${label}`);
      }
    }

    return { apps, detected };
  }

  /**
   * Detect apps from a single-project directory (no monorepo).
   */
  private detectSingleProjectApps(projectName: string): { apps: AppSpec[] } {
    const apps: AppSpec[] = [];
    const fw = this.deps.detectFramework(this.deps.cwd);

    if (fw.framework !== 'custom' || fw.port !== null) {
      apps.push({
        name: projectName,
        framework: fw.framework,
        port: fw.port,
        path: '.',
      });
      return { apps };
    }

    // Root has no framework — scan first-level subdirectories for apps
    const subdirApps = this.scanSubdirectories(this.deps.cwd);
    for (const sub of subdirApps) {
      const subFw = this.deps.detectFramework(sub.dir);
      if (subFw.framework === 'custom' && subFw.port === null) continue;
      const appName = sub.name;
      const appType = this.inferAppType(appName, subFw.framework, subFw.port);
      apps.push({
        name: appName,
        framework: subFw.framework,
        port: appType === 'server' ? subFw.port : null,
        path: `./${sub.name}`,
        type: appType,
      });
    }

    // Fallback: if no subdirectory apps found, use root as custom
    if (apps.length === 0) {
      apps.push({
        name: projectName,
        framework: fw.framework,
        port: fw.port ?? 3000,
        path: '.',
      });
    }

    return { apps };
  }

  /**
   * Generate loopzero.yaml, register project with API, and emit output.
   */
  private async createProjectAndEmitOutput(
    opts: InitOptions,
    yamlPath: string,
    projectName: string,
    originalName: string,
    apps: AppSpec[],
    detected: DetectedInfo | undefined,
    sanitizationWarning: string | undefined,
    noGitWarning: string | undefined,
  ): Promise<number> {
    // Generate loopzero.yaml
    const yamlContent = this.generateYaml(projectName, apps);
    fs.writeFileSync(yamlPath, yamlContent);

    // Update .gitignore
    this.ensureGitignore(this.deps.cwd);

    // Call API
    let projectId: string | undefined;
    let warning: string | undefined;
    // CHG-2026-04-23-AUDIT-36: exit 5 (partial failure per Rule 4)
    // when createProject returns a confirmed-bad status (4xx ≠ 409).
    // loopzero.yaml is still generated so the operator can inspect/edit
    // before retrying, but the exit code tells `loopzero init && loopzero
    // deploy` scripts to stop. 409 is idempotent (project already
    // exists) → exit 0. ECONNREFUSED and 5xx are transient → exit 0
    // (deploy retry will likely succeed or fail for same reason).
    let partialFailure = false;

    try {
      const result = await this.deps.createProject({ name: projectName, apps });
      projectId = result.id;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 409) {
        warning = 'Project already registered.';
      } else if (err instanceof ApiError && err.statusCode >= 400 && err.statusCode < 500) {
        warning = err.message;
        partialFailure = true;
        if (!opts.json) {
          this.deps.writeError(warning);
        }
      } else {
        warning = 'Could not reach server. Run "loopzero deploy" to retry.';
        if (!opts.json) {
          this.deps.writeError(warning);
        }
      }
    }

    // Emit output
    if (opts.json) {
      const jsonOutput: Record<string, unknown> = {
        project: projectName,
        apps: apps.map(a => ({ name: a.name, framework: a.framework, port: a.port })),
      };
      if (projectId) {
        jsonOutput.id = projectId;
      }
      if (detected) {
        jsonOutput.detected = detected;
      }
      if (projectName !== originalName) {
        jsonOutput.sanitized_from = originalName;
      }
      if (sanitizationWarning || warning || noGitWarning) {
        jsonOutput.warning = [sanitizationWarning, warning, noGitWarning].filter(Boolean).join('. ');
      }
      this.deps.writeTo(JSON.stringify(jsonOutput));
    } else {
      this.deps.writeTo(`\nGenerated loopzero.yaml with ${apps.length} app${apps.length > 1 ? 's' : ''}.`);
      if (partialFailure) {
        // CHG-2026-04-23-AUDIT-36: tell operator the backend rejected
        // the project. loopzero.yaml remains for inspection; deploy will
        // fail until the cause is addressed (plan limit, validation).
        this.deps.writeTo('Review loopzero.yaml. Address the error above before `loopzero deploy`.');
      } else {
        this.deps.writeTo('Review and edit if needed, then run: loopzero deploy');
      }
    }

    return partialFailure ? 5 : 0;
  }

  /**
   * Infers the app type from its name, framework, and port.
   * - Frontend frameworks (next, vite, remix) → "frontend"
   * - Names containing "worker", "job", "queue", "cron" → "worker"
   * - Everything else → "server"
   */
  private inferAppType(name: string, framework: string, _port: number | null): 'server' | 'worker' | 'frontend' {
    // Frontend frameworks
    if (FRONTEND_FRAMEWORKS.includes(framework)) {
      return 'frontend';
    }

    // Worker detection by name
    const workerPatterns = ['worker', 'job', 'queue', 'cron', 'consumer', 'processor'];
    const lowerName = name.toLowerCase();
    if (workerPatterns.some(p => lowerName.includes(p))) {
      return 'worker';
    }

    return 'server';
  }

  /**
   * Scans first-level subdirectories for potential app directories.
   * Skips hidden dirs, node_modules, and common non-app directories.
   */
  private scanSubdirectories(rootDir: string): Array<{ name: string; dir: string }> {
    const SKIP = new Set(['node_modules', '.git', 'dist', 'build', 'coverage', '.next', '.cache', 'tmp']);
    const CONVENTIONAL_APP_DIRS = ['apps', 'services', 'microservices'];
    const results: Array<{ name: string; dir: string }> = [];
    try {
      const entries = fs.readdirSync(rootDir, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        if (entry.name.startsWith('.')) continue;
        if (SKIP.has(entry.name)) continue;

        // Check if this is a conventional container directory — scan its children instead
        if (CONVENTIONAL_APP_DIRS.includes(entry.name)) {
          const containerDir = path.join(rootDir, entry.name);
          try {
            const subEntries = fs.readdirSync(containerDir, { withFileTypes: true });
            for (const subEntry of subEntries) {
              if (!subEntry.isDirectory()) continue;
              if (subEntry.name.startsWith('.')) continue;
              if (SKIP.has(subEntry.name)) continue;
              results.push({
                name: subEntry.name,
                dir: path.join(containerDir, subEntry.name),
              });
            }
          } catch {
            // Ignore read errors on container dir
          }
        } else {
          results.push({ name: entry.name, dir: path.join(rootDir, entry.name) });
        }
      }
    } catch {
      // Ignore read errors
    }
    return results;
  }

  /**
   * Sanitizes a directory name into a valid RFC 1123 label for use as project name.
   * Lowercases, replaces invalid chars with hyphens, trims hyphens, truncates to 63 chars.
   */
  private sanitizeProjectName(name: string): string {
    let sanitized = name
      .toLowerCase()
      .replace(/[^a-z0-9-]/g, '-')  // replace invalid chars with hyphens
      .replace(/-+/g, '-')          // collapse consecutive hyphens
      .replace(/^-+|-+$/g, '')      // trim leading/trailing hyphens
      .slice(0, 63);

    // Must start with a letter; if it doesn't, prefix with 'p-'
    if (sanitized && !/^[a-z]/.test(sanitized)) {
      sanitized = 'p-' + sanitized;
      sanitized = sanitized.slice(0, 63);
    }

    return sanitized || 'project';
  }

  private generateYaml(projectName: string, apps: AppSpec[]): string {
    const lines: string[] = [];
    // v2: Always include version for schema evolution
    lines.push('version: 1');
    lines.push(`project: ${this.quoteYaml(projectName)}`);
    lines.push('');
    lines.push('apps:');
    for (const app of apps) {
      lines.push(`  ${this.quoteYaml(app.name)}:`);
      lines.push(`    path: ${this.quoteYaml(app.path)}`);
      lines.push(`    framework: ${this.quoteYaml(app.framework)}`);
      // v2: Always include explicit type (no inference at deploy time)
      const appType = app.type ?? 'server';
      lines.push(`    type: ${appType}`);
      // v2: Always include build strategy
      const build = (app.framework === 'custom' || app.framework === 'unknown') ? 'dockerfile' : 'buildpack';
      lines.push(`    build: ${build}`);
      // MEET-2026-03-29-016: Frontends are ALWAYS static delivery (zero pods, served from CDN).
      if (appType === 'frontend') {
        lines.push('    render: static');
      }
      if (appType === 'server' && app.port !== null) {
        lines.push(`    port: ${app.port}`);
      }
      lines.push('');
    }
    return lines.join('\n');
  }

  /** Quotes a YAML scalar value if it contains special characters. */
  private quoteYaml(value: string): string {
    if (/[:#\[\]{}&*!|>'"%@`,?]/.test(value) || value.includes(' ')) {
      return `"${value.replace(/\\/g, '\\\\').replace(/"/g, '\\"')}"`;
    }
    return value;
  }

  private exitWithError(opts: InitOptions, message: string, exitCode: number, code?: string): number {
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

  private ensureGitignore(cwd: string): void {
    const gitignorePath = path.join(cwd, '.gitignore');
    let content = '';
    if (fs.existsSync(gitignorePath)) {
      content = fs.readFileSync(gitignorePath, 'utf-8');
    }

    if (!content.includes('.env.local')) {
      const addition = content.endsWith('\n') || content === '' ? '.env.local\n' : '\n.env.local\n';
      fs.writeFileSync(gitignorePath, content + addition);
    }
  }
}

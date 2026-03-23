import * as fs from 'fs';
import * as path from 'path';
import { ApiError } from '../api/client';

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
const FRONTEND_FRAMEWORKS = ['nextjs', 'vite', 'remix'];

// Well-known directory names for shared packages (not deployable)
const PACKAGE_DIR_PATTERNS = ['packages/', 'libs/', 'shared/', 'common/', 'config/'];

export interface InitOptions {
  yes?: boolean;
  force?: boolean;
  name?: string;
  json?: boolean;
}

export class InitCommand {
  private readonly deps: InitDeps;

  constructor(deps: InitDeps) {
    this.deps = deps;
  }

  async execute(opts: InitOptions): Promise<number> {
    // Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4);
    }

    // Git repo check — loopzero requires a git repository
    if (this.deps.hasGitRepo && !this.deps.hasGitRepo()) {
      return this.exitWithError(opts, 'Not a git repository. Run: git init', 1);
    }

    // CI mode requires --yes
    if (this.deps.isCI && !opts.yes) {
      return this.exitWithError(opts, 'CI mode requires --yes flag. Run: loopzero init --yes', 1);
    }

    // Check if loopzero.yaml already exists
    const yamlPath = path.join(this.deps.cwd, 'loopzero.yaml');
    if (fs.existsSync(yamlPath) && !opts.force) {
      return this.exitWithError(opts, 'loopzero.yaml already exists. Use --force to overwrite.', 1);
    }

    // Detect monorepo and frameworks
    const monorepo = this.deps.detectMonorepo(this.deps.cwd);
    let projectName = opts.name || path.basename(this.deps.cwd);

    // F-01 fix: ALWAYS sanitize project name — whether auto-detected or user-provided.
    // RFC 1123 compliance is mandatory for K8s namespaces. Unsanitized names cause
    // downstream deploy failures with cryptic namespace errors.
    const originalName = projectName;
    projectName = this.sanitizeProjectName(projectName);

    // Warn user if name was changed by sanitization
    if (opts.name && projectName !== originalName) {
      const warning = `Project name sanitized: "${originalName}" → "${projectName}" (RFC 1123: lowercase, letters/numbers/hyphens, max 63 chars)`;
      if (!opts.json) {
        this.deps.writeTo(warning);
      }
    }

    const apps: AppSpec[] = [];
    let detected: DetectedInfo | undefined;

    if (monorepo) {
      // Separate deployable apps from shared packages
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

      // Build detection info for JSON output
      detected = {
        tool: monorepo.tool,
        shared_packages: skipped.map(s => path.basename(s)),
        apps: [],
      };

      // Human-only detection messages
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
    } else {
      const fw = this.deps.detectFramework(this.deps.cwd);
      if (fw.framework !== 'custom' || fw.port !== null) {
        // Root directory has a recognizable framework
        apps.push({
          name: projectName,
          framework: fw.framework,
          port: fw.port,
          path: '.',
        });
      } else {
        // Root has no framework — scan first-level subdirectories for apps
        const subdirApps = this.scanSubdirectories(this.deps.cwd);
        if (subdirApps.length > 0) {
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
      }
    }

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

    // Generate loopzero.yaml
    const yamlContent = this.generateYaml(projectName, apps);
    fs.writeFileSync(yamlPath, yamlContent);

    // Update .gitignore
    this.ensureGitignore(this.deps.cwd);

    // Call API
    let projectId: string | undefined;
    let warning: string | undefined;

    try {
      const result = await this.deps.createProject({ name: projectName, apps });
      projectId = result.id;
    } catch (err) {
      if (err instanceof ApiError && err.statusCode === 409) {
        // 409 = project already exists, this is normal for re-init
        warning = 'Project already registered.';
      } else if (err instanceof ApiError && err.statusCode >= 400 && err.statusCode < 500) {
        // Client errors (400 invalid name, 422 validation, etc.) — show the actual message
        warning = err.message;
        if (!opts.json) {
          this.deps.writeError(warning);
        }
      } else {
        // Network errors (connection refused, 500, etc.)
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
      if (warning) {
        jsonOutput.warning = warning;
      }
      this.deps.writeTo(JSON.stringify(jsonOutput));
    } else {
      this.deps.writeTo(`\nGenerated loopzero.yaml with ${apps.length} app${apps.length > 1 ? 's' : ''}.`);
      this.deps.writeTo('Review and edit if needed, then run: loopzero deploy');
    }

    return 0;
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
    lines.push(`project: ${this.quoteYaml(projectName)}`);
    lines.push('');
    lines.push('apps:');
    for (const app of apps) {
      lines.push(`  ${this.quoteYaml(app.name)}:`);
      lines.push(`    path: ${this.quoteYaml(app.path)}`);
      lines.push(`    framework: ${this.quoteYaml(app.framework)}`);
      if (app.type === 'worker') {
        lines.push('    type: worker');
      } else if (app.type === 'frontend') {
        lines.push('    type: frontend');
      } else if (app.port !== null) {
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

  private exitWithError(opts: InitOptions, message: string, exitCode: number): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode }));
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

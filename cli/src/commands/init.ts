import * as fs from 'fs';
import * as path from 'path';

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
  detectMonorepo: (dir: string) => MonorepoResult | null;
  detectFramework: (dir: string) => FrameworkResult;
  createProject: (input: { name: string; apps: AppSpec[] }) => Promise<CreateProjectResult>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  cwd: string;
}

interface AppSpec {
  name: string;
  framework: string;
  port: number | null;
  path: string;
}

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
      this.deps.writeError('Not authenticated. Run: loopzero login');
      return 4;
    }

    // CI mode requires --yes
    if (this.deps.isCI && !opts.yes) {
      this.deps.writeError('CI mode requires --yes flag. Run: loopzero init --yes');
      return 1;
    }

    // Check if loopzero.yaml already exists
    const yamlPath = path.join(this.deps.cwd, 'loopzero.yaml');
    if (fs.existsSync(yamlPath) && !opts.force) {
      this.deps.writeError('loopzero.yaml already exists. Use --force to overwrite.');
      return 1;
    }

    // Detect monorepo and frameworks
    const monorepo = this.deps.detectMonorepo(this.deps.cwd);
    const projectName = opts.name || path.basename(this.deps.cwd);

    const apps: AppSpec[] = [];
    if (monorepo) {
      this.deps.writeTo(`Detected: ${monorepo.tool} monorepo`);
      for (const workspace of monorepo.workspaces) {
        const wsDir = path.join(this.deps.cwd, workspace);
        const fw = this.deps.detectFramework(wsDir);
        const appName = path.basename(workspace);
        apps.push({
          name: appName,
          framework: fw.framework,
          port: fw.port,
          path: workspace,
        });
        this.deps.writeTo(`  ${workspace} → ${fw.framework} (port ${fw.port})`);
      }
    } else {
      const fw = this.deps.detectFramework(this.deps.cwd);
      apps.push({
        name: projectName,
        framework: fw.framework,
        port: fw.port,
        path: '.',
      });
    }

    // Generate loopzero.yaml
    const yamlContent = this.generateYaml(projectName, apps);
    fs.writeFileSync(yamlPath, yamlContent);

    // Update .gitignore
    this.ensureGitignore(this.deps.cwd);

    // Call API
    try {
      const result = await this.deps.createProject({ name: projectName, apps });

      if (opts.json) {
        this.deps.writeTo(JSON.stringify({
          project: projectName,
          id: result.id,
          apps: apps.map(a => ({ name: a.name, framework: a.framework, port: a.port })),
        }));
      } else {
        this.deps.writeTo(`\nGenerated loopzero.yaml with ${apps.length} app${apps.length > 1 ? 's' : ''}.`);
        this.deps.writeTo('Review and edit if needed, then run: loopzero deploy');
      }
    } catch {
      // API may not be available yet — still generate loopzero.yaml
      this.deps.writeTo(`\nGenerated loopzero.yaml with ${apps.length} app${apps.length > 1 ? 's' : ''}.`);
      this.deps.writeTo('Review and edit if needed, then run: loopzero deploy');
    }

    return 0;
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
      if (app.port !== null) {
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

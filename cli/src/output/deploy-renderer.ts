export interface DeployEvent {
  type: string;
  data: Record<string, unknown>;
}

export interface AppResult {
  name: string;
  status: string;
  url?: string;
  build_duration_seconds?: number;
  error?: string;
}

export interface DeployResult {
  deploy_id: string;
  status: string;
  duration_seconds: number;
  apps: AppResult[];
}

export interface DeployRendererOptions {
  isCI: boolean;
  isJSON: boolean;
  isTTY: boolean;
}

export class DeployRenderer {
  constructor(private options: DeployRendererOptions) {}

  /**
   * Process an SSE event and return a formatted string for display.
   * Returns null in JSON mode (no intermediate output).
   */
  onEvent(event: DeployEvent): string | null {
    if (this.options.isJSON) {
      return null;
    }

    if (this.options.isCI) {
      return this.renderCIEvent(event);
    }

    return this.renderTTYEvent(event);
  }

  /**
   * Render the final deploy result for human-readable output (TTY or CI).
   */
  renderResult(result: DeployResult): string {
    if (this.options.isCI) {
      return this.renderCIResult(result);
    }

    return this.renderTTYResult(result);
  }

  /**
   * Render the final deploy result as a JSON string for --json mode.
   */
  renderJSON(result: DeployResult): string {
    const output: Record<string, unknown> = {
      deploy_id: result.deploy_id,
      status: result.status,
      duration_seconds: result.duration_seconds,
      apps: result.apps.map((app) => {
        const entry: Record<string, unknown> = {
          name: app.name,
          status: app.status,
        };
        // F-08: Use null for absent/empty URLs (workers), never empty string
        entry.url = app.url || null;
        if (app.build_duration_seconds !== undefined) {
          entry.build_duration_seconds = app.build_duration_seconds;
        }
        if (app.error !== undefined) {
          entry.error = app.error;
        }
        return entry;
      }),
    };

    return JSON.stringify(output, null, 2);
  }

  private renderTTYEvent(event: DeployEvent): string | null {
    const app = String(event.data.app ?? '');

    switch (event.type) {
      case 'build.started': {
        // F-01: build.started includes apps[] array with all app names being built
        const apps = event.data.apps as string[] | undefined;
        if (apps && apps.length > 0) {
          return apps.map((a) => `\x1b[36m⠋\x1b[0m Building ${a}...`).join('\n');
        }
        return `\x1b[36m⠋\x1b[0m Building${app ? ` ${app}` : ''}...`;
      }

      case 'build.completed': {
        const duration = Number(event.data.duration_seconds ?? 0);
        return `\x1b[32m✓\x1b[0m Building ${app} [${duration}s]`;
      }

      case 'build.failed': {
        const error = String(event.data.error ?? 'unknown error');
        return `\x1b[31m✗\x1b[0m Building ${app} [${error}]`;
      }

      case 'deploy.app_live': {
        const url = String(event.data.url ?? '');
        return `\x1b[32m✓\x1b[0m ${app} deployed ${url}`;
      }

      case 'health.checking':
        return `\x1b[36m⠋\x1b[0m Checking service health...`;

      case 'health.passed': {
        const duration = Number(event.data.duration_seconds ?? 0);
        return `\x1b[32m✓\x1b[0m Service healthy [${duration}s]`;
      }

      default:
        return null;
    }
  }

  private renderCIEvent(event: DeployEvent): string | null {
    const app = String(event.data.app ?? '');

    switch (event.type) {
      case 'build.started': {
        // F-01: build.started includes apps[] array with all app names being built
        const apps = event.data.apps as string[] | undefined;
        if (apps && apps.length > 0) {
          return apps.map((a) => `[build] ${a}: building...`).join('\n');
        }
        return `[build] ${app || 'all'}: building...`;
      }

      case 'build.completed': {
        const duration = Number(event.data.duration_seconds ?? 0);
        return `[build] ${app}: complete [${duration}s]`;
      }

      case 'build.failed': {
        const error = String(event.data.error ?? 'unknown error');
        return `[build] ${app}: failed [${error}]`;
      }

      case 'deploy.app_live': {
        const url = String(event.data.url ?? '');
        return `[deploy] ${app}: deployed ${url}`;
      }

      case 'health.checking':
        return `[health] ${app}: checking...`;

      case 'health.passed': {
        const duration = Number(event.data.duration_seconds ?? 0);
        return `[health] ${app}: healthy [${duration}s]`;
      }

      default:
        return null;
    }
  }

  private renderTTYResult(result: DeployResult): string {
    const lines: string[] = [];

    if (result.status === 'success') {
      lines.push(`\x1b[32m✓\x1b[0m Deploy completed in ${result.duration_seconds}s`);
    } else {
      lines.push(`\x1b[31m✗\x1b[0m Deploy ${result.status} (${result.duration_seconds}s)`);
    }

    lines.push('');
    lines.push('Live:');

    for (const app of result.apps) {
      if (app.status === 'failed') {
        lines.push(`  \x1b[31m✗\x1b[0m ${app.name}: ${app.error ?? 'failed'}`);
      } else if (app.url) {
        lines.push(`  ${app.name} → ${app.url}`);
      } else {
        lines.push(`  ${app.name} (no URL — background worker)`);
      }
    }

    return lines.join('\n');
  }

  private renderCIResult(result: DeployResult): string {
    const lines: string[] = [];

    if (result.status === 'success') {
      lines.push(`Deploy completed in ${result.duration_seconds}s`);
    } else {
      lines.push(`Deploy ${result.status} (${result.duration_seconds}s)`);
    }

    for (const app of result.apps) {
      if (app.status === 'failed') {
        lines.push(`  ${app.name}: ${app.error ?? 'failed'}`);
      } else if (app.url) {
        lines.push(`  ${app.name}: ${app.url}`);
      } else {
        lines.push(`  ${app.name}: (no URL — background worker)`);
      }
    }

    return lines.join('\n');
  }
}

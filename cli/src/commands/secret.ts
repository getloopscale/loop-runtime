import { ApiError } from '../api/client';
import type { LoopZeroYamlResult } from '../adapters/loopzero-yaml';
import { classifyNetworkError } from '../output/error-message';

interface SecretListResponse {
  secrets: Array<{ name: string; environment: string; app: string }>;
}

interface SecretSetResponse {
  name: string;
  environment: string;
  app: string;
  created: boolean;
}

interface SecretDeleteResponse {
  name: string;
  deleted: boolean;
}

export interface SecretDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  setSecret: (project: string, name: string, value: string, env: string, app: string) => Promise<SecretSetResponse>;
  listSecrets: (project: string, env: string) => Promise<SecretListResponse>;
  deleteSecret: (project: string, name: string, env: string, app: string) => Promise<SecretDeleteResponse>;
  confirm?: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI?: boolean;
}

export interface SecretSetOptions {
  nameValue?: string; // "KEY=value" format
  env?: string;
  app?: string;
  json?: boolean;
}

export interface SecretListOptions {
  env?: string;
  json?: boolean;
}

export interface SecretDeleteOptions {
  name?: string;
  env?: string;
  app?: string;
  yes?: boolean;
  json?: boolean;
}

export class SecretSetCommand {
  private readonly deps: SecretDeps;

  constructor(deps: SecretDeps) {
    this.deps = deps;
  }

  async execute(opts: SecretSetOptions = {}): Promise<number> {
    // 1. Auth check
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    // 2. Find loopzero.yaml
    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    // 3. Parse name=value
    if (!opts.nameValue || !opts.nameValue.includes('=')) {
      return this.exitWithError(opts, 'Usage: loopzero secret set NAME=value', 1, 'INVALID_FORMAT');
    }

    const eqIdx = opts.nameValue.indexOf('=');
    const name = opts.nameValue.substring(0, eqIdx);
    const value = opts.nameValue.substring(eqIdx + 1);

    // 4. Validate name format
    if (!/^[A-Z][A-Z0-9_]*$/.test(name)) {
      return this.exitWithError(
        opts,
        `Secret name must be uppercase with underscores (e.g., DATABASE_URL). Got: '${name}'.`,
        1,
        'INVALID_SECRET_NAME',
      );
    }

    const env = opts.env ?? 'production';
    const app = opts.app ?? Object.keys(yamlResult.content.apps)[0] ?? '';
    const project = yamlResult.content.project;

    // 5. Call API — value NEVER in output
    let response: SecretSetResponse;
    try {
      response = await this.deps.setSecret(project, name, value, env, app);
    } catch (err) {
      return this.handleError(opts, err);
    }

    // 6. Display result — value NEVER shown
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        name: response.name,
        environment: response.environment,
        app: response.app,
        created: response.created,
      }));
      return 0;
    }

    this.deps.writeTo(`Secret ${response.name} set for ${response.environment}/${response.app}.`);
    return 0;
  }

  private exitWithError(opts: SecretSetOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: SecretSetOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

export class SecretListCommand {
  private readonly deps: SecretDeps;

  constructor(deps: SecretDeps) {
    this.deps = deps;
  }

  async execute(opts: SecretListOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    const env = opts.env ?? 'production';
    const project = yamlResult.content.project;

    let response: SecretListResponse;
    try {
      response = await this.deps.listSecrets(project, env);
    } catch (err) {
      return this.handleError(opts, err);
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify(response));
      return 0;
    }

    this.deps.writeTo(`Environment: ${env}`);
    this.deps.writeTo('');

    if (response.secrets.length === 0) {
      this.deps.writeTo('No secrets configured.');
      return 0;
    }

    for (const secret of response.secrets) {
      this.deps.writeTo(`  ${secret.name.padEnd(20)} ${secret.app}`);
    }

    return 0;
  }

  private exitWithError(opts: SecretListOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: SecretListOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

export class SecretDeleteCommand {
  private readonly deps: SecretDeps;

  constructor(deps: SecretDeps) {
    this.deps = deps;
  }

  async execute(opts: SecretDeleteOptions = {}): Promise<number> {
    if (!this.deps.getToken()) {
      return this.exitWithError(opts, 'Not authenticated. Run: loopzero login', 4, 'AUTH_REQUIRED');
    }

    const yamlResult = this.deps.findLoopZeroYaml();
    if (!yamlResult) {
      return this.exitWithError(opts, 'No loopzero.yaml found. Run: loopzero init', 1, 'MISSING_CONFIG');
    }

    if (!opts.name) {
      return this.exitWithError(opts, 'Secret name is required.', 1, 'MISSING_NAME');
    }

    const env = opts.env ?? 'production';
    const app = opts.app ?? Object.keys(yamlResult.content.apps)[0] ?? '';
    const project = yamlResult.content.project;

    // Confirmation required (unless --yes)
    if (!opts.yes) {
      if (this.deps.isCI || opts.json) {
        return this.exitWithError(opts, 'Confirmation required. Use --yes to skip confirmation. Example: loopzero secret delete KEY --yes --json', 1, 'CONFIRMATION_REQUIRED');
      }
      if (this.deps.confirm) {
        const confirmed = await this.deps.confirm(
          `Delete secret "${opts.name}" from ${env}/${app}? [y/N] `
        );
        if (!confirmed) {
          if (opts.json) {
            this.deps.writeTo(JSON.stringify({ status: 'cancelled', name: opts.name }));
          } else {
            this.deps.writeTo('Delete cancelled.');
          }
          return 0;
        }
      }
    }

    let response: SecretDeleteResponse;
    try {
      response = await this.deps.deleteSecret(project, opts.name, env, app);
    } catch (err) {
      return this.handleError(opts, err);
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify(response));
      return 0;
    }

    this.deps.writeTo(`Secret ${response.name} deleted from ${env}/${app}.`);
    return 0;
  }

  private exitWithError(opts: SecretDeleteOptions, message: string, exitCode: number, code?: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }

  private handleError(opts: SecretDeleteOptions, err: unknown): number {
    if (err instanceof ApiError) {
      return this.exitWithError(opts, err.message, err.exitCode, err.code);
    }
    // CHG-2026-04-23-AUDIT-25: surface API_UNREACHABLE for network errors.
    const net = classifyNetworkError(err);
    if (net.isNetwork) {
      return this.exitWithError(opts, net.message, 3, 'API_UNREACHABLE');
    }
    return this.exitWithError(opts, 'Unexpected error. Try again later.', 3, 'UNEXPECTED_ERROR');
  }
}

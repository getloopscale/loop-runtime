import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

interface LoopZeroConfig {
  token: string;
  email: string;
  team: string;
}

export class ConfigStore {
  readonly configDir: string;
  private readonly configPath: string;

  constructor(baseDir: string, xdgMode = false) {
    this.configDir = xdgMode
      ? path.join(baseDir, 'loopzero')
      : path.join(baseDir, '.loopzero');
    this.configPath = path.join(this.configDir, 'config.json');
  }

  static default(): ConfigStore {
    const xdg = process.env.XDG_CONFIG_HOME;
    if (xdg) {
      return new ConfigStore(xdg, true);
    }
    return new ConfigStore(os.homedir());
  }

  saveToken(token: string, email: string, team: string): void {
    fs.mkdirSync(this.configDir, { recursive: true });

    const config: LoopZeroConfig = { token, email, team };
    const fd = fs.openSync(this.configPath, 'w', 0o600);
    fs.writeFileSync(fd, JSON.stringify(config, null, 2));
    fs.closeSync(fd);
    // Ensure permissions are 0600 even if the file pre-existed with broader perms
    fs.chmodSync(this.configPath, 0o600);
  }

  getConfig(): LoopZeroConfig | null {
    try {
      const raw = fs.readFileSync(this.configPath, 'utf-8');
      return JSON.parse(raw) as LoopZeroConfig;
    } catch {
      return null;
    }
  }

  getToken(): string | null {
    // LOOPZERO_TOKEN env var takes precedence over config file (CI/CD support)
    const envToken = process.env.LOOPZERO_TOKEN;
    if (envToken) {
      return envToken;
    }
    return this.getConfig()?.token ?? null;
  }

  getEmail(): string | null {
    return this.getConfig()?.email ?? null;
  }

  getTeam(): string | null {
    return this.getConfig()?.team ?? null;
  }

  clearToken(): void {
    try {
      fs.unlinkSync(this.configPath);
    } catch {
      // File may not exist — that's fine
    }
  }
}

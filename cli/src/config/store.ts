// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

interface LoopZeroConfig {
  token: string;
  email: string;
  team: string;
  refresh_token?: string;
  token_expires_at?: number; // Unix timestamp (seconds)
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

  saveToken(token: string, email: string, team: string, refreshToken?: string, expiresIn?: number): void {
    fs.mkdirSync(this.configDir, { recursive: true, mode: 0o700 });

    const config: LoopZeroConfig = { token, email, team };
    if (refreshToken) {
      config.refresh_token = refreshToken;
    }
    if (expiresIn) {
      config.token_expires_at = Math.floor(Date.now() / 1000) + expiresIn;
    }
    // Atomic write: write to temp file with correct permissions, then rename.
    // This avoids a window where the file exists with stale permissions
    // and token content is world-readable.
    const tmpPath = this.configPath + '.tmp';
    fs.writeFileSync(tmpPath, JSON.stringify(config, null, 2), { mode: 0o600 });
    fs.renameSync(tmpPath, this.configPath);
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

  getRefreshToken(): string | null {
    return this.getConfig()?.refresh_token ?? null;
  }

  isTokenExpired(): boolean {
    const config = this.getConfig();
    if (!config?.token_expires_at) return false; // PAT tokens don't expire
    // Consider expired 60 seconds before actual expiration (buffer for clock skew)
    return Math.floor(Date.now() / 1000) >= config.token_expires_at - 60;
  }

  clearToken(): void {
    try {
      fs.unlinkSync(this.configPath);
    } catch {
      // File may not exist — that's fine
    }
  }
}

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export interface LogoutDeps {
  clearToken: () => void;
  writeTo: (msg: string) => void;
  confirm: (message: string) => Promise<boolean>;
  isCI: boolean;
}

export interface LogoutOptions {
  json?: boolean;
  yes?: boolean;
}

export class LogoutCommand {
  private readonly deps: LogoutDeps;

  constructor(deps: LogoutDeps) {
    this.deps = deps;
  }

  async execute(opts: LogoutOptions = {}): Promise<number> {
    if (!opts.yes && !this.deps.isCI) {
      const confirmed = await this.deps.confirm(
        'Log out? This will remove your stored credentials. [y/N]'
      );
      if (!confirmed) {
        if (opts.json) {
          this.deps.writeTo(JSON.stringify({ status: 'cancelled' }));
        } else {
          this.deps.writeTo('Logout cancelled.');
        }
        return 0;
      }
    }

    this.deps.clearToken();

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ authenticated: false }));
    } else {
      this.deps.writeTo('Logged out');
    }

    return 0;
  }
}

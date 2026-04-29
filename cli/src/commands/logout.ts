// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export interface LogoutDeps {
  clearToken: () => void;
  // F-06 (CHG-2026-04-29-DOGFOOD T1.2) — detecta se há sessão ativa para
  // emitir mensagem coerente quando logout é chamado sem auth prévia.
  hasToken?: () => boolean;
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
    // F-06 (CHG-2026-04-29-DOGFOOD T1.2) — quando não há sessão, emitir
    // "Already logged out." em vez de "Logout cancelled." (que implicaria
    // que houve um prompt e o usuário disse não — confunde quem nunca
    // executou `loopzero login`). Não pula o clearToken para manter
    // idempotência (limpa qualquer config residual).
    const alreadyLoggedOut = this.deps.hasToken !== undefined && !this.deps.hasToken();
    if (alreadyLoggedOut) {
      this.deps.clearToken();
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ authenticated: false, status: 'already_logged_out' }));
      } else {
        this.deps.writeTo('Already logged out.');
      }
      return 0;
    }

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

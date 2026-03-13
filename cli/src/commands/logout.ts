export interface LogoutDeps {
  clearToken: () => void;
  writeTo: (msg: string) => void;
}

export interface LogoutOptions {
  json?: boolean;
}

export class LogoutCommand {
  private readonly deps: LogoutDeps;

  constructor(deps: LogoutDeps) {
    this.deps = deps;
  }

  async execute(opts: LogoutOptions = {}): Promise<number> {
    this.deps.clearToken();

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ authenticated: false }));
    } else {
      this.deps.writeTo('Logged out');
    }

    return 0;
  }
}

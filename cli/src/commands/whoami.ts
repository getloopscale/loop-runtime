export interface WhoamiDeps {
  getEmail: () => string | null;
  getTeam: () => string | null;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface WhoamiOptions {
  json?: boolean;
}

export class WhoamiCommand {
  private readonly deps: WhoamiDeps;

  constructor(deps: WhoamiDeps) {
    this.deps = deps;
  }

  async execute(opts: WhoamiOptions = {}): Promise<number> {
    const email = this.deps.getEmail();
    const team = this.deps.getTeam();

    if (!email) {
      if (opts.json) {
        this.deps.writeTo(JSON.stringify({ authenticated: false }));
      } else {
        this.deps.writeError('Not authenticated. Run: loopzero login');
      }
      return 4;
    }

    if (opts.json) {
      this.deps.writeTo(
        JSON.stringify({
          authenticated: true,
          email,
          team,
        })
      );
    } else {
      this.deps.writeTo(`Authenticated as: ${email}`);
      if (team) {
        this.deps.writeTo(`Team: ${team}`);
      }
    }

    return 0;
  }
}

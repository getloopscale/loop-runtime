export interface FormatterOptions {
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  isJSON: boolean;
}

const VALID_EXIT_CODES = new Set([0, 1, 2, 3, 4, 5]);

export class Formatter {
  private readonly opts: FormatterOptions;

  constructor(opts: FormatterOptions) {
    this.opts = opts;
  }

  success(msg: string): void {
    if (this.opts.isCI) {
      this.opts.writeTo(msg);
    } else {
      this.opts.writeTo(`\x1b[32m✓\x1b[0m ${msg}`);
    }
  }

  error(msg: string): void {
    if (this.opts.isCI) {
      this.opts.writeError(msg);
    } else {
      this.opts.writeError(`\x1b[31m✗\x1b[0m ${msg}`);
    }
  }

  info(msg: string): void {
    if (this.opts.isCI) {
      this.opts.writeTo(msg);
    } else {
      this.opts.writeTo(`\x1b[36mℹ\x1b[0m ${msg}`);
    }
  }

  json(data: Record<string, unknown>): void {
    this.opts.writeTo(JSON.stringify(data, null, 2));
  }

  table(headers: string[], rows: string[][]): void {
    const widths = headers.map((h, i) =>
      Math.max(h.length, ...rows.map((r) => (r[i] || '').length))
    );

    const formatRow = (cells: string[]): string =>
      cells.map((c, i) => c.padEnd(widths[i])).join('  ');

    this.opts.writeTo(formatRow(headers));
    this.opts.writeTo(widths.map((w) => '─'.repeat(w)).join('  '));
    for (const row of rows) {
      this.opts.writeTo(formatRow(row));
    }
  }

  static detectCI(): boolean {
    return !!(
      process.env.CI === 'true' ||
      process.env.GITHUB_ACTIONS ||
      process.env.GITLAB_CI ||
      process.env.CIRCLECI ||
      process.env.JENKINS_URL
    );
  }

  static isValidExitCode(code: number): boolean {
    return VALID_EXIT_CODES.has(code);
  }
}

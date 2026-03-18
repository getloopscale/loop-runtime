export interface FormatterOptions {
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI: boolean;
  isJSON: boolean;
  isTTY?: boolean;
}

const VALID_EXIT_CODES = new Set([0, 1, 2, 3, 4, 5]);

export class Formatter {
  private readonly opts: FormatterOptions;

  constructor(opts: FormatterOptions) {
    this.opts = opts;
  }

  private shouldUseColor(): boolean {
    if (this.opts.isCI) return false;
    // When isTTY is explicitly provided, use it; otherwise check stdout
    const tty = this.opts.isTTY ?? process.stdout.isTTY ?? false;
    return tty;
  }

  success(msg: string): void {
    if (!this.shouldUseColor()) {
      this.opts.writeTo(msg);
    } else {
      this.opts.writeTo(`\x1b[32m✓\x1b[0m ${msg}`);
    }
  }

  error(msg: string): void {
    if (!this.shouldUseColor()) {
      this.opts.writeError(msg);
    } else {
      this.opts.writeError(`\x1b[31m✗\x1b[0m ${msg}`);
    }
  }

  info(msg: string): void {
    if (!this.shouldUseColor()) {
      this.opts.writeTo(msg);
    } else {
      this.opts.writeTo(`\x1b[36mℹ\x1b[0m ${msg}`);
    }
  }

  json(data: Record<string, unknown>): void {
    this.opts.writeTo(JSON.stringify(data));
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

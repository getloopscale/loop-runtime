// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

export interface BillingInfo {
  plan: string;
  team: string;
  usage: Record<string, { used: number; limit: number }>;
  message?: string;
}

export interface PortalInfo {
  url: string;
  message?: string;
}

export interface BillingDeps {
  getBilling: () => Promise<BillingInfo | null>;
  getPortalURL: () => Promise<PortalInfo | null>;
  openBrowser: (url: string) => Promise<void>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}

export interface BillingOptions {
  json?: boolean;
  subcommand?: string; // "portal" or undefined
}

export class BillingCommand {
  private readonly deps: BillingDeps;

  constructor(deps: BillingDeps) {
    this.deps = deps;
  }

  async execute(opts: BillingOptions): Promise<number> {
    if (opts.subcommand === 'portal') {
      return this.portal(opts);
    }
    return this.show(opts);
  }

  private async show(opts: BillingOptions): Promise<number> {
    const billing = await this.deps.getBilling();
    if (!billing) {
      return this.exitWithError(opts, 'Failed to retrieve account information. Run: loopzero login', 4, 'AUTH_FAILED');
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({
        plan: billing.plan,
        team: billing.team,
        usage: billing.usage,
      }));
    } else {
      this.deps.writeTo(`Plan: ${billing.plan}`);
      this.deps.writeTo(`Team: ${billing.team}`);

      const usageEntries = Object.entries(billing.usage);
      if (usageEntries.length > 0) {
        this.deps.writeTo('');
        this.deps.writeTo('Usage:');
        for (const [metric, { used, limit }] of usageEntries) {
          const limitStr = limit >= 999999 ? 'unlimited' : String(limit);
          this.deps.writeTo(`  ${metric}: ${used} / ${limitStr}`);
        }
      }

      this.deps.writeTo('');
      this.deps.writeTo('Manage your plan: loopzero billing portal');
    }

    return 0;
  }

  private async portal(opts: BillingOptions): Promise<number> {
    const portal = await this.deps.getPortalURL();
    if (!portal) {
      return this.exitWithError(opts, 'Failed to open subscription portal. Run: loopzero login', 4, 'AUTH_FAILED');
    }

    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ url: portal.url }));
    } else {
      this.deps.writeTo('Opening subscription portal...');
      try {
        await this.deps.openBrowser(portal.url);
      } catch {
        this.deps.writeTo(`Open this URL: ${portal.url}`);
      }
    }

    return 0;
  }

  private exitWithError(opts: BillingOptions, message: string, exitCode: number, code: string): number {
    if (opts.json) {
      this.deps.writeTo(JSON.stringify({ error: message, exit_code: exitCode, code }));
    } else {
      this.deps.writeError(message);
    }
    return exitCode;
  }
}

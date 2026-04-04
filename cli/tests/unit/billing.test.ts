// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { BillingCommand, BillingDeps, BillingOptions } from '../../src/commands/billing';

describe('BillingCommand', () => {
  function createDeps(overrides: Partial<BillingDeps> = {}): BillingDeps {
    return {
      getBilling: jest.fn().mockResolvedValue({
        plan: 'Starter',
        team: 'acme',
        usage: { builds: { used: 47, limit: 100 } },
      }),
      getPortalURL: jest.fn().mockResolvedValue({ url: 'https://billing.example.com/portal' }),
      openBrowser: jest.fn().mockResolvedValue(undefined),
      writeTo: jest.fn(),
      writeError: jest.fn(),
      ...overrides,
    };
  }

  it('should show plan and usage', async () => {
    const deps = createDeps();
    const cmd = new BillingCommand(deps);
    const code = await cmd.execute({});

    expect(code).toBe(0);
    expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('Starter'));
    expect(deps.writeTo).toHaveBeenCalledWith(expect.stringContaining('acme'));
  });

  it('should output JSON when --json', async () => {
    const deps = createDeps();
    const cmd = new BillingCommand(deps);
    const code = await cmd.execute({ json: true });

    expect(code).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
    const parsed = JSON.parse(output);
    expect(parsed.plan).toBe('Starter');
    expect(parsed.team).toBe('acme');
    expect(parsed.usage).toBeDefined();
  });

  it('should return exit code 4 when not authenticated', async () => {
    const deps = createDeps({ getBilling: jest.fn().mockResolvedValue(null) });
    const cmd = new BillingCommand(deps);
    const code = await cmd.execute({});

    expect(code).toBe(4);
    expect(deps.writeError).toHaveBeenCalled();
  });

  it('should open portal', async () => {
    const deps = createDeps();
    const cmd = new BillingCommand(deps);
    const code = await cmd.execute({ subcommand: 'portal' });

    expect(code).toBe(0);
    expect(deps.openBrowser).toHaveBeenCalledWith('https://billing.example.com/portal');
  });

  it('should output portal URL in JSON mode', async () => {
    const deps = createDeps();
    const cmd = new BillingCommand(deps);
    const code = await cmd.execute({ subcommand: 'portal', json: true });

    expect(code).toBe(0);
    const output = (deps.writeTo as jest.Mock).mock.calls[0][0];
    const parsed = JSON.parse(output);
    expect(parsed.url).toBe('https://billing.example.com/portal');
  });

  it('should not contain internal terms in output', async () => {
    const deps = createDeps();
    const cmd = new BillingCommand(deps);
    await cmd.execute({});

    const allOutput = (deps.writeTo as jest.Mock).mock.calls.map((c: string[]) => c[0]).join(' ');
    const internalTerms = ['Stripe', 'stripe', 'subscription', 'invoice', 'webhook'];
    for (const term of internalTerms) {
      expect(allOutput).not.toContain(term);
    }
  });
});

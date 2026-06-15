import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import {
  DomainTroubleshootSheet,
  __test_TIPS,
  __test_GENERIC_TIP,
} from './domain-troubleshoot-sheet';

describe('DomainTroubleshootSheet (J-11 v1.1 T2.4)', () => {
  it('test_troubleshoot_sheet_renders_cert_manager_tip', () => {
    render(
      <DomainTroubleshootSheet
        open={true}
        onOpenChange={vi.fn()}
        domainName="loopzero-test.example"
        failureMode="cert-manager error"
      />,
    );
    expect(screen.getByText(/Certificate issuance failed/i)).toBeInTheDocument();
    expect(screen.getByText(/DNS propagation hasn't completed yet/)).toBeInTheDocument();
  });

  it('test_troubleshoot_sheet_renders_cf_api_tip', () => {
    render(
      <DomainTroubleshootSheet
        open={true}
        onOpenChange={vi.fn()}
        domainName="loopzero-test.example"
        failureMode="CF API error"
      />,
    );
    expect(screen.getByText(/Cloudflare API temporarily unavailable/i)).toBeInTheDocument();
  });

  it('test_troubleshoot_sheet_renders_generic_tip_for_unknown_failure', () => {
    render(
      <DomainTroubleshootSheet
        open={true}
        onOpenChange={vi.fn()}
        domainName="loopzero-test.example"
        failureMode="some-unknown-mode"
      />,
    );
    expect(screen.getByText(/Need help/i)).toBeInTheDocument();
    expect(screen.getByText(/Contact support with the domain name/)).toBeInTheDocument();
  });

  it('test_troubleshoot_sheet_renders_generic_tip_when_failure_mode_undefined', () => {
    render(
      <DomainTroubleshootSheet
        open={true}
        onOpenChange={vi.fn()}
        domainName="loopzero-test.example"
      />,
    );
    expect(screen.getByText(/Need help/i)).toBeInTheDocument();
  });

  it('test_canonical_tip_keys_documented', () => {
    // ADR D4 lists 3 canonical failure_mode keys. Hermetic guard against
    // silent drift — adding new tips MUST update plan + this test.
    expect(Object.keys(__test_TIPS).sort()).toEqual(
      ['CF API error', 'CF API timeout', 'cert-manager error'].sort(),
    );
    expect(__test_GENERIC_TIP.title).toBe('Need help?');
  });
});

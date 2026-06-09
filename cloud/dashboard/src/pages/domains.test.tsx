import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'test', traits: { email: 'u@x.com' } },
  }),
}));

const mockUseEngineApi = vi.fn();
vi.mock('@/hooks/use-api', () => ({
  useEngineApi: () => mockUseEngineApi(),
  useCloudApi: () => mockUseEngineApi(),
}));

import { DomainsPage } from './domains';

describe('DomainsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_domains_renders_list_with_status', () => {
    mockUseEngineApi.mockReturnValue({
      data: {
        domains: [
          {
            id: 'd1',
            domain: 'app.example.com',
            project: 'web',
            app: 'frontend',
            environment: 'prod',
            status: 'active',
            cname_target: 'loopzero.app',
            created_at: '2026-05-20',
            tls: { status: 'issued' },
          },
        ],
        total: 1,
      },
      loading: false,
      error: false,
    });
    render(<DomainsPage />);
    expect(screen.getByText('app.example.com')).toBeInTheDocument();
    expect(screen.getByText('Active')).toBeInTheDocument();
  });

  it('test_domains_add_domain_opens_form', () => {
    mockUseEngineApi.mockReturnValue({
      data: { domains: [], total: 0 },
      loading: false,
      error: false,
    });
    render(<DomainsPage />);
    // PageShell primary + EmptyState CTA both surface an Add Domain button.
    expect(
      screen.getAllByRole('button', { name: /add domain/i }).length,
    ).toBeGreaterThanOrEqual(1);
  });

  it('test_domains_dns_record_copy_button_works', () => {
    // Post 2026-05-26 migration (sponsor directive "all tables like /environments"):
    // DataTable expandable → Table primitive with always-visible DNS records as a
    // colSpan row directly below each pending_verification row. No click needed —
    // the information is actionable so it stays visible. CopyButton instances
    // (Brief #3 primitive) for CNAME + TXT values.
    mockUseEngineApi.mockReturnValue({
      data: {
        domains: [
          {
            id: 'd1',
            domain: 'pending.example.com',
            project: 'web',
            app: 'frontend',
            environment: 'prod',
            status: 'pending_verification',
            cname_target: 'loopzero.app',
            verification: { txt_name: '_loopzero.pending', txt_value: 'verify=abc123' },
            created_at: '2026-05-23',
          },
        ],
        total: 1,
      },
      loading: false,
      error: false,
    });
    render(<DomainsPage />);
    // DNS records visible immediately (no expand click)
    expect(screen.getByText('CNAME')).toBeInTheDocument();
    expect(screen.getByText('TXT')).toBeInTheDocument();
    const copyButtons = screen.getAllByRole('button', { name: /copy/i });
    expect(copyButtons.length).toBeGreaterThanOrEqual(2);
  });

  it('test_domains_verification_state_displays_correctly', () => {
    // Post Brief #5: Verify button lives in DataTable rowActions column.
    mockUseEngineApi.mockReturnValue({
      data: {
        domains: [
          {
            id: 'd1',
            domain: 'pending.example.com',
            project: 'web',
            app: 'frontend',
            environment: 'prod',
            status: 'pending_verification',
            cname_target: 'loopzero.app',
            verification: { txt_name: '_loopzero.pending', txt_value: 'verify=abc' },
            created_at: '2026-05-23',
          },
        ],
        total: 1,
      },
      loading: false,
      error: false,
    });
    render(<DomainsPage />);
    expect(screen.getByText('Pending DNS')).toBeInTheDocument();
    // DataTable rowActions cell renders Verify button when status = pending_verification
    expect(screen.getByRole('button', { name: /^verify$/i })).toBeInTheDocument();
  });
});

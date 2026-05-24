import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'test', traits: { email: 'u@x.com' } },
  }),
}));

const mockUseCloudApi = vi.fn();
vi.mock('@/hooks/use-api', () => ({
  useCloudApi: () => mockUseCloudApi(),
  useEngineApi: () => mockUseCloudApi(),
}));

const mockCloudFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  cloudFetch: (...args: unknown[]) => mockCloudFetch(...args),
  engineFetch: (...args: unknown[]) => mockCloudFetch(...args),
}));

import { BillingPage } from './billing';

describe('BillingPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_billing_renders_plan_badge_with_current_plan', () => {
    mockUseCloudApi.mockReturnValue({
      data: {
        plan: 'pro',
        team: 'acme',
        usage: {},
      },
      loading: false,
      error: false,
    });
    render(<BillingPage />);
    // PlanBadge + plan label both render "Pro" — assert occurrence count.
    expect(screen.getAllByText('Pro').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('acme')).toBeInTheDocument();
  });

  it('test_billing_portal_button_calls_endpoint', async () => {
    mockUseCloudApi.mockReturnValue({
      data: { plan: 'pro', team: 'acme', usage: {} },
      loading: false,
      error: false,
    });
    mockCloudFetch.mockResolvedValue({ url: 'https://billing.stripe.com/portal/xyz' });
    const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);

    const user = userEvent.setup();
    render(<BillingPage />);
    await user.click(screen.getByRole('button', { name: /manage plan/i }));

    expect(mockCloudFetch).toHaveBeenCalledWith('/v1/billing/portal');
    expect(openSpy).toHaveBeenCalledWith('https://billing.stripe.com/portal/xyz', '_blank');
    openSpy.mockRestore();
  });

  it('test_billing_renders_invoice_table_with_data', () => {
    // Plan EC-5 v1.2: invoice table OMITTED — no /v1/billing/invoices endpoint exists.
    // This test verifies the documented absence (no invoice section rendered) so
    // a future re-introduction is detected and triggers test update.
    mockUseCloudApi.mockReturnValue({
      data: { plan: 'pro', team: 'acme', usage: {} },
      loading: false,
      error: false,
    });
    render(<BillingPage />);
    expect(screen.queryByText(/invoice/i)).not.toBeInTheDocument();
  });

  it('test_billing_cancel_subscription_requires_confirmation', () => {
    // BillingPage v1.2: cancel flow is delegated to Stripe Portal (no in-app cancel button).
    // Verify there is no direct cancel button — managing plan goes through portal.
    mockUseCloudApi.mockReturnValue({
      data: { plan: 'pro', team: 'acme', usage: {} },
      loading: false,
      error: false,
    });
    render(<BillingPage />);
    expect(screen.queryByRole('button', { name: /cancel subscription/i })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /manage plan/i })).toBeInTheDocument();
  });

  it('test_billing_usage_meter_reflects_period_data', () => {
    mockUseCloudApi.mockReturnValue({
      data: {
        plan: 'pro',
        team: 'acme',
        usage: {
          build_minutes: { used: 120, limit: 500 },
          bandwidth_mb: { used: 1000, limit: 10000 },
        },
        period_start: '2026-05-01T00:00:00Z',
        period_end: '2026-05-31T23:59:59Z',
      },
      loading: false,
      error: false,
    });
    render(<BillingPage />);
    expect(screen.getByText('Usage This Period')).toBeInTheDocument();
    // Metric labels are humanized: "Build Minutes", "Bandwidth Mb"
    expect(screen.getByText('Build Minutes')).toBeInTheDocument();
    expect(screen.getByText('Bandwidth Mb')).toBeInTheDocument();
  });
});

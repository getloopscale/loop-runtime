import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'test', traits: { email: 'u@x.com' } },
  }),
}));

const mockUseProjects = vi.fn();
vi.mock('@/hooks/use-projects', () => ({
  useProjects: () => mockUseProjects(),
}));

const mockUseCloudApi = vi.fn();
vi.mock('@/hooks/use-api', () => ({
  useCloudApi: () => mockUseCloudApi(),
  useEngineApi: () => mockUseCloudApi(),
}));

import { OverviewPage } from './overview';

describe('OverviewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseProjects.mockReturnValue({ projects: [], loading: false });
    // Default: billing returns null (covers fallback path)
    mockUseCloudApi.mockReturnValue({ data: null, loading: false, error: false });
  });

  it('test_overview_renders_action_bar_with_add_new', () => {
    render(<OverviewPage />);
    expect(screen.getByRole('button', { name: /add new/i })).toBeInTheDocument();
  });

  it('test_overview_renders_usage_meter_with_3_metrics', () => {
    render(<OverviewPage />);
    expect(screen.getByText('Fast Data Transfer')).toBeInTheDocument();
    expect(screen.getByText('Fast Origin Transfer')).toBeInTheDocument();
    expect(screen.getByText('Edge Requests')).toBeInTheDocument();
  });

  it('test_overview_renders_alerts_empty_state_with_cta', () => {
    render(<OverviewPage />);
    expect(screen.getByText('Get alerted for anomalies')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /upgrade to pro/i })).toBeInTheDocument();
  });

  it('test_overview_renders_projects_empty_when_no_data', () => {
    mockUseProjects.mockReturnValue({ projects: [], loading: false });
    render(<OverviewPage />);
    expect(screen.getByText('No projects yet')).toBeInTheDocument();
  });

  it('test_overview_renders_project_cards_when_data_present', () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'alpha', apps: [], last_deploy: '2026-05-23' },
        { name: 'beta', apps: [], last_deploy: '2026-05-22' },
      ],
      loading: false,
    });
    render(<OverviewPage />);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('beta')).toBeInTheDocument();
  });

  it('test_overview_renders_recent_previews_empty', () => {
    render(<OverviewPage />);
    expect(screen.getByText('No recent previews')).toBeInTheDocument();
  });

  it('test_overview_search_input_filters_projects', async () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'alpha', apps: [] },
        { name: 'beta', apps: [] },
      ],
      loading: false,
    });
    const user = userEvent.setup();
    render(<OverviewPage />);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('beta')).toBeInTheDocument();
    const search = screen.getByPlaceholderText('Search projects...');
    await user.type(search, 'alp');
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.queryByText('beta')).not.toBeInTheDocument();
  });

  // ---- v1.1 T-A.2 (B-2 + EC-5 + EC-6) ----

  it('test_overview_usage_reflects_billing_data_when_loaded', () => {
    mockUseCloudApi.mockReturnValue({
      data: {
        plan: 'pro',
        team: 'acme',
        usage: {
          'Fast Data Transfer': { used: 412, limit: 1000 },
          'Fast Origin Transfer': { used: 87, limit: 250 },
          'Edge Requests': { used: 1_240_000, limit: 10_000_000 },
        },
      },
      loading: false,
      error: false,
    });
    render(<OverviewPage />);
    // Real "used" numbers from billing fixture must appear (412 instead of 0)
    expect(screen.getByText(/412 \/ 1,000/)).toBeInTheDocument();
    expect(screen.getByText(/87 \/ 250/)).toBeInTheDocument();
    // Edge Requests gets compact-millions formatter
    expect(screen.getByText(/1,240,000 \/ 10M/)).toBeInTheDocument();
  });

  it('test_overview_usage_falls_back_to_zero_when_billing_null', () => {
    mockUseCloudApi.mockReturnValue({ data: null, loading: false, error: false });
    render(<OverviewPage />);
    // No billing → fallback to defaults (used=0, max=defaultLimit per pickMetric)
    // Anchor regex to end-of-string ($) to avoid "0 / 10" matching "0 / 100"
    expect(screen.getByText(/^0 \/ 100$/)).toBeInTheDocument();
    expect(screen.getByText(/^0 \/ 10$/)).toBeInTheDocument();
    expect(screen.getByText(/^0 \/ 1M$/)).toBeInTheDocument();
  });

  it('test_overview_usage_renders_unlimited_when_limit_null', () => {
    // EC-5: Enterprise tier returns limit:null → "Unlimited" per ADR-D1.bis
    mockUseCloudApi.mockReturnValue({
      data: {
        plan: 'enterprise',
        team: 'acme',
        usage: {
          'Fast Data Transfer': { used: 5000, limit: null },
          'Fast Origin Transfer': { used: 1500, limit: null },
          'Edge Requests': { used: 50_000_000, limit: null },
        },
      },
      loading: false,
      error: false,
    });
    render(<OverviewPage />);
    // All 3 rows show "/ Unlimited" via fmtMetric() when max===null
    const unlimitedLabels = screen.getAllByText(/\/ Unlimited/);
    expect(unlimitedLabels.length).toBe(3);
  });

  it('test_overview_usage_renders_over_quota', () => {
    // EC-6: used > limit. UsageMeter primitive natively flips intent="warning"
    // and adds text-warning class per .llms.txt over-quota detection.
    mockUseCloudApi.mockReturnValue({
      data: {
        plan: 'pro',
        team: 'acme',
        usage: {
          'Fast Data Transfer': { used: 1200, limit: 1000 },
          'Fast Origin Transfer': { used: 87, limit: 250 },
          'Edge Requests': { used: 500_000, limit: 1_000_000 },
        },
      },
      loading: false,
      error: false,
    });
    const { container } = render(<OverviewPage />);
    // Over-quota row shows real numeric (not clamped to limit)
    expect(screen.getByText(/1,200 \/ 1,000/)).toBeInTheDocument();
    // UsageMeter primitive applies text-warning class to over-quota value
    expect(container.querySelector('.text-warning')).toBeInTheDocument();
  });
});

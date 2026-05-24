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

import { OverviewPage } from './overview';

describe('OverviewPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUseProjects.mockReturnValue({ projects: [], loading: false });
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
});

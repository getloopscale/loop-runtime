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

import { ProjectsPage } from './projects';

describe('ProjectsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_projects_renders_grid_of_cards', () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'alpha', apps: [], last_deploy: '2026-05-23' },
        { name: 'beta', apps: [], last_deploy: '2026-05-22' },
      ],
      loading: false,
    });
    render(<ProjectsPage />);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('beta')).toBeInTheDocument();
  });

  it('test_projects_empty_state_when_no_data', () => {
    mockUseProjects.mockReturnValue({ projects: [], loading: false });
    render(<ProjectsPage />);
    // Sprint 3 T3.3: EmptyProjectsHero replaces PageShell.empty when projects.length === 0.
    // Hero shows hero title + 3 template cards (each containing "loopzero init <starter>") +
    // "Or from your own repo" section with bare "loopzero init" CLI command.
    expect(screen.getByText('Deploy your first project')).toBeInTheDocument();
    // Multiple "loopzero init …" matches expected — assert at least one + the dashboard CTA
    expect(screen.getAllByText(/loopzero init/i).length).toBeGreaterThan(0);
    expect(
      screen.getByRole('button', { name: /create project from dashboard/i }),
    ).toBeInTheDocument();
  });

  it('test_projects_loading_state_renders_skeleton', () => {
    // Post Brief #5 migration: DataTable loading state renders 5 skeleton
    // rows (replaced standalone Loader2 spinner with built-in skeleton).
    // Skeleton elements have `aria-busy="true"` or role="status".
    mockUseProjects.mockReturnValue({ projects: [], loading: true });
    const { container } = render(<ProjectsPage />);
    // DataTable renders skeleton with aria-busy on body
    expect(container.querySelector('[aria-busy="true"], [role="status"]')).toBeInTheDocument();
  });

  it('test_projects_search_filters_by_name', async () => {
    mockUseProjects.mockReturnValue({
      projects: [
        { name: 'alpha', apps: [] },
        { name: 'beta', apps: [] },
      ],
      loading: false,
    });
    const user = userEvent.setup();
    render(<ProjectsPage />);
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.getByText('beta')).toBeInTheDocument();
    const search = screen.getByPlaceholderText('Search projects...');
    await user.type(search, 'alp');
    expect(screen.getByText('alpha')).toBeInTheDocument();
    expect(screen.queryByText('beta')).not.toBeInTheDocument();
  });
});

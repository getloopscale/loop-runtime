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
    expect(screen.getByText('No projects yet')).toBeInTheDocument();
    expect(screen.getByText(/loopzero init/i)).toBeInTheDocument();
  });

  it('test_projects_loading_state_renders_skeleton', () => {
    mockUseProjects.mockReturnValue({ projects: [], loading: true });
    render(<ProjectsPage />);
    // Loader2 has aria-label="Loading projects" per ProjectsPage source
    expect(screen.getByLabelText('Loading projects')).toBeInTheDocument();
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

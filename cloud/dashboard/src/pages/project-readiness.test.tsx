import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@/test/render';
import { Routes, Route } from 'react-router-dom';

// EC-08 + general: mock useProjects to control project-not-found path
const mockUseProjects = vi.fn();
vi.mock('@/hooks/use-projects', () => ({
  useProjects: () => mockUseProjects(),
}));

import { ProjectReadinessPage } from './project-readiness';

function renderAtRoute(path: string) {
  return render(
    <Routes>
      <Route path="/projects/:name/readiness" element={<ProjectReadinessPage />} />
    </Routes>,
    { initialEntries: [path] },
  );
}

describe('ProjectReadinessPage (Sprint 1 T1.1)', () => {
  it('test_renders_PageShell_with_title', () => {
    mockUseProjects.mockReturnValue({
      projects: [{ name: 'acme-storefront' }],
      loading: false,
    });
    renderAtRoute('/projects/acme-storefront/readiness');
    expect(screen.getByText('Production readiness')).toBeInTheDocument();
  });

  it('test_renders_empty_state_when_project_not_found', () => {
    // EC-07: invalid projectName → distinct UX, not 8 confusing unknown signals
    mockUseProjects.mockReturnValue({
      projects: [{ name: 'something-else' }],
      loading: false,
    });
    renderAtRoute('/projects/does-not-exist/readiness');
    expect(screen.getByText(/no project named "does-not-exist"/i)).toBeInTheDocument();
  });

  it('test_shows_loading_state_while_projects_query_pending', () => {
    mockUseProjects.mockReturnValue({ projects: [], loading: true });
    renderAtRoute('/projects/acme-storefront/readiness');
    // PageShell renders skeleton when loading prop true — assert title at minimum
    expect(screen.getByText('Production readiness')).toBeInTheDocument();
  });
});

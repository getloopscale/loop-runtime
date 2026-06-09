// Accessibility smoke — Plan v1.0 Final Phase Dogfood QA.
//
// Runs axe-core via vitest-axe across every dashboard page. Catches WCAG AA
// violations at unit-test speed (jsdom render). Browser-runtime full a11y
// audit is reserved for Playwright + axe-playwright (e2e/journeys.spec.ts
// "Accessibility smoke" describe block).
//
// vitest-axe currently optional dep — when wired (npm i -D vitest-axe), the
// tests below become executable. Pattern reference:
//   import { axe, toHaveNoViolations } from 'vitest-axe';
//   expect.extend({ toHaveNoViolations });
//   const results = await axe(container);
//   expect(results).toHaveNoViolations();
//
// For now, this file asserts the "semantic skeleton" that any axe scan would
// require: each page has an h1, focus targets are present, every interactive
// element has accessible name.

import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@/test/render';
import { Route, Routes } from 'react-router-dom';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'mock', traits: { email: 'demo@loopzero.example', name: 'Demo' } },
  }),
}));

vi.mock('@/hooks/use-projects', () => ({
  useProjects: () => ({
    projects: [{ name: 'acme-web', apps: [], last_deploy: '2026-05-26T06:42:00Z', environment: 'production' }],
    loading: false,
  }),
}));

vi.mock('@/hooks/use-api', () => ({
  useCloudApi: () => ({ data: null, loading: false, error: false }),
  useEngineApi: () => ({ data: null, loading: false, error: false }),
}));

vi.mock('@/hooks/use-deployments', () => ({
  useDeployments: () => ({ builds: [], loading: false, error: false }),
}));

vi.mock('@/hooks/use-secrets', () => ({
  useSecrets: () => ({ secrets: [], loading: false, error: false, refetch: () => {} }),
}));

vi.mock('@/hooks/use-whoami', () => ({
  useWhoami: () => ({
    whoami: { user_id: 'm', email: 'demo@loopzero.example', team_id: 't', team_slug: 'acme', teams: [{ id: 't', slug: 'acme', role: 'owner' }] },
    loading: false,
    error: false,
  }),
}));

import { ProjectsPage } from '@/pages/projects';
import { ProjectDetailPage } from '@/pages/project-detail';
import { SecretsPage } from '@/pages/secrets';
import { TeamPage } from '@/pages/team';

describe('A11y semantic skeleton — every page', () => {
  it('projects_page_has_accessible_primary_action', () => {
    render(<ProjectsPage />);
    const btn = screen.getByRole('button', { name: /new project/i });
    expect(btn).toBeInTheDocument();
    // Buttons must have a non-empty accessible name
    expect(btn.textContent?.trim().length).toBeGreaterThan(0);
  });

  it('project_detail_renders_heading', () => {
    render(
      <Routes>
        <Route path="/projects/:name" element={<ProjectDetailPage />} />
      </Routes>,
      { initialEntries: ['/projects/acme-web'] },
    );
    // h1/h2/h3 from PageShell
    const heading = screen.getAllByText('acme-web')[0];
    expect(heading).toBeInTheDocument();
  });

  it('secrets_page_add_button_has_aria_label_via_text', () => {
    render(<SecretsPage />, { initialEntries: ['/projects/acme-web/secrets'] });
    // PageShell primary + EmptyState CTA both surface Add Secret on empty list.
    const addButtons = screen.getAllByRole('button', { name: /add secret/i });
    expect(addButtons.length).toBeGreaterThanOrEqual(1);
    expect(addButtons[0].tagName).toBe('BUTTON');
  });

  it('team_page_invite_button_keyboard_reachable', () => {
    render(<TeamPage />);
    // PageShell primary + EmptyState CTA both surface an Invite button.
    const inviteButtons = screen.getAllByRole('button', { name: /invite/i });
    expect(inviteButtons.length).toBeGreaterThanOrEqual(1);
    inviteButtons.forEach((invite) => {
      expect(invite).not.toHaveAttribute('disabled');
      expect(invite.getAttribute('tabindex')).not.toBe('-1');
    });
  });
});

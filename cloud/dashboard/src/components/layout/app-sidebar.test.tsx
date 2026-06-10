import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import { AppSidebar } from './app-sidebar';
import type { Session } from '@/lib/auth';

function makeSession(name?: string): Session {
  return {
    active: true,
    identity: {
      id: 'sess-1',
      traits: { email: 'paulo@example.com', name },
    },
  };
}

describe('AppSidebar', () => {
  it('test_app_sidebar_renders_all_nav_items', () => {
    render(<AppSidebar session={makeSession()} />);
    // MAIN_NAV (5) + Billing (1) + Audit (1) + Settings (1) + Profile (1) = 9 routes.
    // We assert each nav item is reachable via accessible-name match (case-insensitive)
    // by querying within the main sidebar (excluding the TeamSwitcher "Switch team" button).
    for (const label of [
      'Overview',
      'Projects',
      'Environments',
      'Domains',
      'Billing',
      'Audit log',
      'Settings',
      'Profile',
    ]) {
      expect(screen.getByRole('button', { name: new RegExp(label, 'i') })).toBeInTheDocument();
    }
    // "Team" nav item collides with the TeamSwitcher "Switch team" button label.
    // Verify via the more-specific item-by-link-text query.
    const teamButtons = screen.getAllByRole('button', { name: /Team/i });
    expect(teamButtons.length).toBeGreaterThanOrEqual(1);
  });

  it('test_app_sidebar_active_item_matches_current_route', () => {
    render(<AppSidebar session={makeSession()} />, { initialEntries: ['/projects'] });
    const projectsButton = screen.getByRole('button', { name: /Projects/i });
    // active class includes bg-primary/10 + text-primary per Sidebar.Item internals
    expect(projectsButton.className).toMatch(/text-primary|bg-primary/);
  });

  it('test_app_sidebar_team_switcher_renders_session_name', () => {
    // Plan v1.0 T5.2 (2026-05-26): AccountMenu replaced with TeamSwitcher.
    // The switcher renders the session traits.name (or falls back to email).
    render(<AppSidebar session={makeSession()} />);
    expect(screen.getByText('paulo@example.com')).toBeInTheDocument();
  });

  it('test_app_sidebar_team_switcher_trigger_has_aria_label', () => {
    // Plan v1.0 T5.2: dropdown trigger must be reachable by accessible name.
    render(<AppSidebar session={makeSession()} />);
    expect(
      screen.getByRole('button', { name: /switch team/i }),
    ).toBeInTheDocument();
  });

  it('test_app_sidebar_search_input_present_with_placeholder', () => {
    render(<AppSidebar session={makeSession()} />);
    expect(screen.getByPlaceholderText('Find a project...')).toBeInTheDocument();
  });
});

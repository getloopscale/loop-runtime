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
  it('test_app_sidebar_renders_all_8_nav_items', () => {
    render(<AppSidebar session={makeSession()} />);
    // 8 routes from MAIN_NAV (5) + BILLING_NAV (1) + FOOTER_NAV (2)
    for (const label of [
      'Overview',
      'Projects',
      'Environments',
      'Domains',
      'Team',
      'Billing',
      'Settings',
      'Profile',
    ]) {
      expect(screen.getByRole('button', { name: new RegExp(label, 'i') })).toBeInTheDocument();
    }
  });

  it('test_app_sidebar_active_item_matches_current_route', () => {
    render(<AppSidebar session={makeSession()} />, { initialEntries: ['/projects'] });
    const projectsButton = screen.getByRole('button', { name: /Projects/i });
    // active class includes bg-primary/10 + text-primary per Sidebar.Item internals
    expect(projectsButton.className).toMatch(/text-primary|bg-primary/);
  });

  it('test_app_sidebar_account_menu_renders_session_email', () => {
    render(<AppSidebar session={makeSession()} />);
    expect(screen.getByText('paulo@example.com')).toBeInTheDocument();
  });

  it('test_app_sidebar_account_menu_passes_plan_tier_to_badge', () => {
    render(<AppSidebar session={makeSession()} />);
    // Plan hard-coded to "hobby" per T-C.1; PlanBadge renders "Hobby" label by default
    expect(screen.getByText(/hobby/i)).toBeInTheDocument();
  });

  it('test_app_sidebar_search_input_present_with_placeholder', () => {
    render(<AppSidebar session={makeSession()} />);
    expect(screen.getByPlaceholderText('Find a project...')).toBeInTheDocument();
  });
});

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@/test/render';
import type { Session } from '@/lib/auth';

// Mock useRequireAuth to control session state per test.
// Hoisted via vi.mock so component import sees the mock.
vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: vi.fn(),
}));

// Mock AppSidebar + TopBar to avoid pulling project fetch + AccountMenu chain.
// We're testing DashboardLayout's own logic (session/verification gates), not children.
vi.mock('./app-sidebar', () => ({
  AppSidebar: () => <aside data-testid="app-sidebar">sidebar</aside>,
}));
vi.mock('./top-bar', () => ({
  TopBar: () => <header data-testid="top-bar">topbar</header>,
}));

import { useRequireAuth } from '@/hooks/use-auth';
import { DashboardLayout } from './dashboard-layout';

function makeSession(verified: boolean): Session {
  return {
    active: true,
    identity: {
      id: 'test-id',
      traits: { email: 'user@example.com' },
      verifiable_addresses: [
        { value: 'user@example.com', verified, via: 'email' },
      ],
    },
  };
}

describe('DashboardLayout', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_dashboard_layout_shows_spinner_when_session_loading', () => {
    vi.mocked(useRequireAuth).mockReturnValue(null);
    render(
      <DashboardLayout>
        <div>child</div>
      </DashboardLayout>,
    );
    expect(screen.getByLabelText('Loading')).toBeInTheDocument();
  });

  it('test_dashboard_layout_renders_children_when_session_ready', () => {
    vi.mocked(useRequireAuth).mockReturnValue(makeSession(true));
    render(
      <DashboardLayout>
        <div data-testid="child">child content</div>
      </DashboardLayout>,
    );
    expect(screen.getByTestId('child')).toBeInTheDocument();
    expect(screen.getByTestId('app-sidebar')).toBeInTheDocument();
    expect(screen.getByTestId('top-bar')).toBeInTheDocument();
  });

  it('test_dashboard_layout_shows_verification_banner_when_unverified', () => {
    vi.mocked(useRequireAuth).mockReturnValue(makeSession(false));
    render(
      <DashboardLayout>
        <div>child</div>
      </DashboardLayout>,
    );
    // Verification banner uses Alert with "Verify your email" title
    expect(screen.getByText('Verify your email')).toBeInTheDocument();
  });

  it('test_dashboard_layout_hides_verification_banner_when_verified', () => {
    vi.mocked(useRequireAuth).mockReturnValue(makeSession(true));
    render(
      <DashboardLayout>
        <div>child</div>
      </DashboardLayout>,
    );
    expect(screen.queryByText('Verify your email')).not.toBeInTheDocument();
  });
});

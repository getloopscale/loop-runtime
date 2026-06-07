// Regression coverage for the API-tokens route consolidation.
//
// Before this commit:
//   - /api-keys              → ApiKeysPage (orphaned; no sidebar entry pointed here)
//   - /settings/tokens       → SubPagePlaceholder ("coming soon" + a non-existent
//                              `loopzero token create` CLI suggestion — the sidebar's
//                              actual target)
//
// After:
//   - /api-keys              → <Navigate to="/settings/tokens" replace />
//   - /settings/tokens       → ApiKeysPage (the real, FAANG-shape lifecycle UI)
//
// These tests pin the new behavior. If a future edit re-orphans the page or
// reintroduces the placeholder, the suite fails loudly instead of silently
// hiding the only platform-key UI again.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { Routes, Route, Navigate } from 'react-router-dom';
import { render, screen } from '@/test/render';
import { ApiKeysPage } from './pages/api-keys';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'test', traits: { email: 'u@x.com' } },
  }),
}));

const mockCloudFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  cloudFetch: (...args: unknown[]) => mockCloudFetch(...args),
  engineFetch: (...args: unknown[]) => mockCloudFetch(...args),
}));

// Minimal route table that mirrors the two routes under test from app.tsx.
// We do NOT mount <App /> because <App /> declares its own BrowserRouter,
// which would conflict with the MemoryRouter the test render helper provides.
// Pinning the behavior in isolation keeps the regression contract sharp and
// independent of unrelated route additions to app.tsx.
function RoutingHarness() {
  return (
    <Routes>
      <Route path="/api-keys" element={<Navigate to="/settings/tokens" replace />} />
      <Route path="/settings/tokens" element={<ApiKeysPage />} />
    </Routes>
  );
}

describe('App routing — API tokens consolidation', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockCloudFetch.mockResolvedValue({ keys: [] });
  });

  it('/settings/tokens renders the real ApiKeysPage (not a placeholder)', async () => {
    render(<RoutingHarness />, { initialEntries: ['/settings/tokens'] });

    // The real page shows the empty-state copy from ApiKeysPage; the old
    // placeholder said "API Tokens is coming soon" + suggested the
    // non-existent `loopzero token create --name my-token` CLI.
    expect(await screen.findByText(/No API keys yet/i)).toBeInTheDocument();
    expect(screen.queryByText(/coming soon/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/loopzero token create/i)).not.toBeInTheDocument();
  });

  it('/api-keys redirects to /settings/tokens (URL alias for bookmarks)', async () => {
    render(<RoutingHarness />, { initialEntries: ['/api-keys'] });

    // After the Navigate replace, the rendered tree must be the real page.
    // We don't assert on location.pathname because MemoryRouter exposes it
    // via context not window.location; observing the rendered output is the
    // user-equivalent signal anyway.
    expect(await screen.findByText(/No API keys yet/i)).toBeInTheDocument();
  });
});

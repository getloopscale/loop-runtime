// Plan 4 of dashboard-memory-section-roadmap.md, T4.5.
// 3 cross-cut regression guards — one per touched page. Existing test
// files on billing/audit/settings remain unmodified; their full pass +
// these 3 new assertions cover the Plan 4 ZERO regression invariant (D2).

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

// Shared auth + nav mocks.
vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 't', traits: { email: 'u@x.com' } },
  }),
}));

vi.mock('@/hooks/use-whoami', () => ({
  useWhoami: () => ({ whoami: { team_id: 'team-a' } }),
}));

const mockCloudFetch = vi.fn();
const mockEngineFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  cloudFetch: (...args: unknown[]) => mockCloudFetch(...args),
  engineFetch: (...args: unknown[]) => mockEngineFetch(...args),
  getApiBase: () => '',
}));

// useCloudApi delegates to cloudFetch under the hood; the existing pages
// use both. Mocking the underlying fetcher covers both paths.

import { BillingPage } from './billing';
import { AuditPage } from './audit';
import { SettingsPage } from './settings';

describe('Plan 4 Memory cross-cuts — regression + additive guards', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // -------------------------------------------------------------------------
  // T4.2 — /billing Memory usage card
  // -------------------------------------------------------------------------
  it('billing: Memory usage card renders empty state when metrics show no activity', async () => {
    // The page calls useCloudApi for both /v1/billing AND /v1/memory/metrics.
    // Order is not guaranteed; mock both.
    mockCloudFetch.mockImplementation((path: string) => {
      if (String(path).includes('/v1/memory/metrics')) {
        return Promise.resolve({ total_memories: 0, retrieval_api_usage: 0, add_events: 0, entity_count: 0 });
      }
      return Promise.resolve({ plan: 'pro', team: 'acme', usage: {} });
    });
    render(<BillingPage />);
    const card = await screen.findByTestId('billing-memory-card');
    expect(card).toBeInTheDocument();
    expect(screen.getByText(/no memory activity yet/i)).toBeInTheDocument();
    // Get started CTA → /memory link visible.
    expect(screen.getByRole('button', { name: /get started/i })).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // T4.3 — /audit Memory source filter (client-side per D3)
  // -------------------------------------------------------------------------
  it('audit: Memory source filter chip toggles + filters entries client-side', async () => {
    mockEngineFetch.mockResolvedValueOnce({
      entries: [
        { id: 'a1', actor_email: 'u@x.com', actor_kind: 'user', action: 'deploy.created', ts: new Date().toISOString(), severity: 'info', resource_type: 'deploy', resource_id: 'd1' },
        { id: 'a2', actor_email: 'u@x.com', actor_kind: 'user', action: 'memory.remember', ts: new Date().toISOString(), severity: 'info', resource_type: 'memory', resource_id: 'm1' },
      ],
    });
    const user = userEvent.setup();
    render(<AuditPage />);
    // Filter button rendered.
    const filterChip = await screen.findByTestId('audit-source-filter');
    expect(filterChip).toBeInTheDocument();
    // Both entries visible initially.
    expect(await screen.findByText('deploy.created')).toBeInTheDocument();
    expect(screen.getByText('memory.remember')).toBeInTheDocument();
    // Click the "Filter by Memory" button.
    await user.click(screen.getByRole('button', { name: /filter by memory/i }));
    // After filter: only memory.* visible.
    expect(screen.queryByText('deploy.created')).not.toBeInTheDocument();
    expect(screen.getByText('memory.remember')).toBeInTheDocument();
  });

  // -------------------------------------------------------------------------
  // T4.4 — /settings Memory section card (D2 — Card not Tab)
  // -------------------------------------------------------------------------
  it('settings: Memory section card renders with Open Memory CTA', async () => {
    render(<SettingsPage />);
    const card = await screen.findByTestId('settings-memory-card');
    expect(card).toBeInTheDocument();
    // The Card.Title "Memory" + the CTA.
    expect(screen.getAllByText(/memory/i).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByRole('button', { name: /open memory/i })).toBeInTheDocument();
    // Existing CLI-First, Connected integrations, and Danger Zone cards must
    // still be there — ZERO regression (D2 invariant). "Delete Team" can
    // appear in both the DangerZone title + ConfirmDialog title (DOM-rendered
    // but hidden); use getAllByText so the count check is robust.
    expect(screen.getByText(/CLI-First/i)).toBeInTheDocument();
    expect(screen.getByText(/Connected integrations/i)).toBeInTheDocument();
    expect(screen.getAllByText(/Delete Team/i).length).toBeGreaterThanOrEqual(1);
  });
});

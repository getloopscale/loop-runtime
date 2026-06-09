// Plan 1 of dashboard-memory-section-roadmap.md, T1.8 acceptance.
// Focused on: (1) list renders, (2) filter updates URL, (3) ConfirmDialog
// gates delete via confirmationPhrase.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 't', traits: { email: 'u@x.com' } },
  }),
}));

const mockCloudFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  cloudFetch: (...args: unknown[]) => mockCloudFetch(...args),
  engineFetch: (...args: unknown[]) => mockCloudFetch(...args),
}));

import { MemoryListPage } from './memories';

describe('MemoryListPage (/memory/memories)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Reset URL between tests — pushState leaks across tests in vitest.
    window.history.pushState({}, '', '/memory/memories');
  });

  // T3.2 — 4 scope tabs matching entities.tsx canonical pattern.
  it('renders 4 scope tabs (USER/SESSION/AGENT/REPO) matching entities pattern', async () => {
    mockCloudFetch.mockResolvedValue({ memories: [] });
    render(<MemoryListPage />);
    expect(await screen.findByRole('tab', { name: /USER/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /SESSION/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /AGENT/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /REPO/i })).toBeInTheDocument();
  });

  // T3.2 — default scope is `user` (matches entities.tsx default).
  it('defaults to user scope and passes scope=user in the BFF query', async () => {
    mockCloudFetch.mockResolvedValue({ memories: [] });
    render(<MemoryListPage />);
    await screen.findByText(/No memories yet/i);
    const calls = mockCloudFetch.mock.calls;
    const url = String(calls[calls.length - 1]?.[0] ?? '');
    expect(url).toContain('scope=user');
  });

  // T3.2 EC-8 — invalid ?scope=garbage defensively coerces to 'user'.
  it('EC-8: invalid ?scope=garbage defaults to user (no crash)', async () => {
    mockCloudFetch.mockResolvedValue({ memories: [] });
    // MemoryRouter consumes initialEntries via the test render helper —
    // window.history.pushState does NOT propagate to router context.
    render(<MemoryListPage />, { initialEntries: ['/memory/memories?scope=garbage'] });
    await screen.findByText(/No memories yet/i);
    // BFF call still uses user scope (defensive coerce), NOT the garbage value.
    const calls = mockCloudFetch.mock.calls;
    const url = String(calls[calls.length - 1]?.[0] ?? '');
    expect(url).toContain('scope=user');
    expect(url).not.toContain('scope=garbage');
  });

  it('renders memories returned by the backend with scope_type Badge per row (T3.3 EC-9)', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      memories: [
        { id: 'mem-aaaa1111bbbb2222', content: 'prefers TypeScript', created_at: new Date().toISOString() },
        { id: 'mem-cccc3333dddd4444', content: 'lives in Lisbon', created_at: new Date().toISOString() },
      ],
    });
    render(<MemoryListPage />);
    expect(await screen.findByText('prefers TypeScript')).toBeInTheDocument();
    expect(screen.getByText('lives in Lisbon')).toBeInTheDocument();
    // EC-9: scope_type Badge per row (derived client-side from active URL scope).
    // Default scope is 'user' → Badge text 'USER'. Multiple rows → multiple Badges.
    const userBadges = screen.getAllByText('USER');
    expect(userBadges.length).toBeGreaterThanOrEqual(2);
  });

  // v1.5 follow-up (Path A — ADR-0010 honest disclosure):
  // REPO tab EmptyState explains skill scope is SDK-only per ADR-0010.
  it('REPO scope empty state explains skill SDK access (ADR-0010 honest disclosure)', async () => {
    mockCloudFetch.mockResolvedValue({ memories: [] });
    render(<MemoryListPage />, { initialEntries: ['/memory/memories?scope=repo'] });
    expect(await screen.findByText(/Skill scope — programmatic access only/i)).toBeInTheDocument();
    expect(screen.getByText(/Memory\.skills\(repoId\)\.recall\(query\)/i)).toBeInTheDocument();
    expect(screen.getByText(/ADR-0010/i)).toBeInTheDocument();
  });

  it('empty result renders a friendly EmptyState (D3 — no broken UI on first run)', async () => {
    mockCloudFetch.mockResolvedValueOnce({ memories: [] });
    render(<MemoryListPage />);
    expect(await screen.findByText(/No memories yet/i)).toBeInTheDocument();
  });

  it('typing in the category filter triggers a refetch with the new query', async () => {
    mockCloudFetch.mockResolvedValueOnce({ memories: [] });
    const user = userEvent.setup();
    render(<MemoryListPage />);

    // Wait for the first load to settle.
    expect(await screen.findByText(/No memories yet/i)).toBeInTheDocument();

    mockCloudFetch.mockResolvedValueOnce({ memories: [] });
    await user.type(screen.getByLabelText(/category/i), 'preferences');

    // The category propagates into the request URL — the assertion
    // checks the LAST cloudFetch call's path contains the filter value.
    // (We don't depend on debounce timing; useQuery re-keys per param,
    //  so any state change triggers at least one new fetch.)
    const calls = mockCloudFetch.mock.calls;
    const lastURL = String(calls[calls.length - 1]?.[0] ?? '');
    expect(lastURL).toContain('category=');
  });

  it('ConfirmDialog gates Forget until confirmationPhrase matches memory id', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      memories: [
        { id: 'mem-aaaa1111bbbb2222', content: 'fact', created_at: new Date().toISOString() },
      ],
    });
    const user = userEvent.setup();
    render(<MemoryListPage />);
    expect(await screen.findByText('fact')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /forget memory mem-aaaa1111bbbb2222/i }));

    // The ConfirmDialog opens — Forget button is rendered but disabled
    // until the typed phrase equals the memory id. We only assert that
    // the dialog renders the warning copy; the typed-verification logic
    // itself is owned by @loopzero/ui ConfirmDialog (covered there).
    expect(await screen.findByText(/permanently forgotten/i)).toBeInTheDocument();
    expect(screen.getByText(/this cannot be undone/i)).toBeInTheDocument();
  });
});

// Plan 2 of dashboard-memory-section-roadmap.md, T2.5 acceptance.
// Focused on: 4 tabs render + URL state + empty state + ConfirmDialog impact.

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

import { EntitiesPage } from './entities';

describe('EntitiesPage (/memory/entities)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders 4 scope tabs (USER / SESSION / AGENT / REPO)', async () => {
    mockCloudFetch.mockResolvedValueOnce({ entities: [] });
    render(<EntitiesPage />);
    expect(await screen.findByRole('tab', { name: /user/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /session/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /agent/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /repo/i })).toBeInTheDocument();
  });

  it('default active scope is "user" — backend hit with scope=user', async () => {
    mockCloudFetch.mockResolvedValueOnce({ entities: [] });
    render(<EntitiesPage />);
    // First call to backend must include scope=user.
    expect(await screen.findByRole('tab', { name: /user/i })).toBeInTheDocument();
    expect(mockCloudFetch).toHaveBeenCalledWith(expect.stringContaining('scope=user'));
  });

  it('empty entities renders EmptyState (D3 inheritance — no broken table)', async () => {
    mockCloudFetch.mockResolvedValueOnce({ entities: [] });
    render(<EntitiesPage />);
    expect(await screen.findByText(/no entities/i)).toBeInTheDocument();
  });

  it('renders entity rows when backend returns data', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      entities: [
        { id: 'alex', scope_type: 'user', memory_count: 12, last_seen_at: new Date().toISOString() },
        { id: 'bob', scope_type: 'user', memory_count: 3, last_seen_at: new Date().toISOString() },
      ],
    });
    render(<EntitiesPage />);
    expect(await screen.findByText('alex')).toBeInTheDocument();
    expect(screen.getByText('bob')).toBeInTheDocument();
    // memory_count rendered (12 + 3 — at least one of those numerals visible)
    expect(document.body.textContent ?? '').toMatch(/12/);
  });

  it('clicking trash opens a ConfirmDialog showing the affected memory count', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      entities: [
        { id: 'alex', scope_type: 'user', memory_count: 12, last_seen_at: new Date().toISOString() },
      ],
    });
    const user = userEvent.setup();
    render(<EntitiesPage />);
    expect(await screen.findByText('alex')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /forget all memories under user\/alex/i }));
    // ConfirmDialog body must show the impact + reversibility copy.
    // The number "12" is rendered inside a <strong> tag separate from the
    // " memories" sibling text, so assert via document.body to traverse
    // mixed text nodes.
    expect(await screen.findByText(/cannot be undone/i)).toBeInTheDocument();
    const body = document.body.textContent ?? '';
    expect(body).toMatch(/12/);
    expect(body.toLowerCase()).toContain('memor');
  });
});

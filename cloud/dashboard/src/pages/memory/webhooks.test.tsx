// Plan 3 of dashboard-memory-section-roadmap.md, T3.7 acceptance.
// Focused on D2 (EC-14): persistent "Demo only" Alert at TOP.

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

import { WebhooksPage } from './webhooks';

describe('WebhooksPage (/memory/webhooks)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('D2 (EC-14) — renders persistent "Demo only" Alert', async () => {
    mockCloudFetch.mockResolvedValueOnce({ webhooks: [] });
    render(<WebhooksPage />);
    const alert = await screen.findByTestId('webhook-demo-alert');
    expect(alert).toBeInTheDocument();
    // Title "Demo only" is mandated by D2 — the body text describes the
    // disclosure further; assert both pieces are present somewhere on the
    // page (Alert may split title + description into sibling nodes).
    expect(screen.getByText('Demo only')).toBeInTheDocument();
    expect(screen.getByText(/delivery is not yet implemented/i)).toBeInTheDocument();
    // CRITICAL: no dismiss button on the Demo-only Alert (D2 invariant —
    // customers must not be able to make the warning go away).
    expect(alert.querySelector('button')).toBeNull();
  });

  it('shows empty state when no webhooks exist', async () => {
    mockCloudFetch.mockResolvedValueOnce({ webhooks: [] });
    render(<WebhooksPage />);
    expect(await screen.findByText(/no webhooks yet/i)).toBeInTheDocument();
  });

  it('Add webhook dialog opens + form fields render', async () => {
    mockCloudFetch.mockResolvedValueOnce({ webhooks: [] });
    const user = userEvent.setup();
    render(<WebhooksPage />);
    await user.click(
      (await screen.findAllByRole('button', { name: /add webhook/i }))[0],
    );
    expect(await screen.findByLabelText(/url/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/label/i)).toBeInTheDocument();
    expect(screen.getByLabelText('memory.remembered')).toBeInTheDocument();
  });
});

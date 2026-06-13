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

  // Memory Webhook Delivery plan T5.7 — REGRESSION OF Plan 3 D2 (EC-14):
  // the Demo-only Alert is removed because actual delivery is now shipped
  // (HMAC signing + exp backoff retry + dead-letter terminal). The honesty
  // debt registered in Plan 3 is closed.
  it('T5.7 — Plan 3 D2 honesty debt closed: NO "Demo only" Alert', async () => {
    mockCloudFetch.mockResolvedValueOnce({ webhooks: [] });
    render(<WebhooksPage />);
    // Settle the page mount; sometimes the EmptyState arrives before assertions.
    expect(await screen.findByText(/no webhooks yet/i)).toBeInTheDocument();
    // The data-testid + the "Demo only" copy MUST be absent now.
    expect(screen.queryByTestId('webhook-demo-alert')).not.toBeInTheDocument();
    expect(screen.queryByText('Demo only')).not.toBeInTheDocument();
    expect(screen.queryByText(/delivery is not yet implemented/i)).not.toBeInTheDocument();
  });

  it('T5.7 — Recent deliveries column hits the per-webhook deliveries endpoint', async () => {
    mockCloudFetch.mockImplementation((path: string) => {
      if (String(path).includes('/v1/memory/webhooks/list')) {
        return Promise.resolve({
          webhooks: [
            { id: 'mwh-aaaa1111bbbb2222', url: 'https://hook.example.com', label: 'prod', events: ['memory.forgotten'], created_at: new Date().toISOString() },
          ],
        });
      }
      if (String(path).includes('/deliveries')) {
        return Promise.resolve({
          deliveries: [
            { id: 'mwd-1', event_type: 'memory.forgotten', status: 'delivered', attempt_count: 1, created_at: new Date().toISOString() },
          ],
        });
      }
      return Promise.resolve({ webhooks: [] });
    });
    render(<WebhooksPage />);
    expect(await screen.findByText('prod')).toBeInTheDocument();
    // Wait for the per-row deliveries badge to render.
    expect(await screen.findByTestId('deliveries-latest-mwh-aaaa1111bbbb2222')).toBeInTheDocument();
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

// Plan 0 of dashboard-memory-section-roadmap.md, T0.1 acceptance gate.
// Focused on the EC-13 absorption: "I've copied my key safely" Checkbox
// MUST gate the Close (Done) button on the show-once secret dialog. If a
// future refactor breaks this gate, the customer can dismiss the dialog
// without copying — and the secret is unrecoverable.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

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

import { ApiKeysPage } from './api-keys';

describe('ApiKeysPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders empty state when no keys exist', async () => {
    mockCloudFetch.mockResolvedValueOnce({ keys: [] });
    render(<ApiKeysPage />);
    expect(await screen.findByText(/No API keys yet/i)).toBeInTheDocument();
    expect(
      screen.getAllByRole('button', { name: /create api key/i }).length,
    ).toBeGreaterThanOrEqual(1);
  });

  it('lists keys with masked prefix + scope badges', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      keys: [
        {
          id: 'apk-1111111111111111',
          name: 'prod-agent',
          key_prefix: 'loopzero_live_Ab',
          scopes: ['memory:read', 'memory:write'],
          created_at: new Date().toISOString(),
        },
      ],
    });
    render(<ApiKeysPage />);
    expect(await screen.findByText('prod-agent')).toBeInTheDocument();
    expect(screen.getByText(/loopzero_live_Ab…/)).toBeInTheDocument();
    expect(screen.getByText('memory:read')).toBeInTheDocument();
    expect(screen.getByText('memory:write')).toBeInTheDocument();
  });

  it('EC-13: Close button is disabled until "I\'ve copied" checkbox is checked', async () => {
    // Empty list so the page renders without table noise.
    mockCloudFetch.mockResolvedValueOnce({ keys: [] });
    // POST /v1/api-keys returns the show-once secret.
    mockCloudFetch.mockResolvedValueOnce({
      id: 'apk-2222222222222222',
      name: 'test',
      key_prefix: 'loopzero_live_Cd',
      scopes: ['memory:read'],
      secret: 'REDACTED_SECRET_014',
    });

    const user = userEvent.setup();
    render(<ApiKeysPage />);

    // Open the create dialog from the empty-state CTA.
    await user.click(
      (await screen.findAllByRole('button', { name: /create api key/i }))[0],
    );
    // Name the key + submit.
    await user.type(screen.getByLabelText(/name/i), 'test');
    await user.click(screen.getByRole('button', { name: /create key/i }));

    // Show-once dialog renders the secret.
    expect(
      await screen.findByText(/REDACTED_SECRET_014/),
    ).toBeInTheDocument();

    // GUARD: "Done" button MUST be disabled before the customer ticks the box.
    const doneButton = screen.getByRole('button', { name: /^done$/i });
    expect(doneButton).toBeDisabled();
    expect(doneButton.getAttribute('aria-disabled')).toBe('true');

    // Tick the checkbox.
    await user.click(screen.getByLabelText(/i've copied my key safely/i));

    // Done becomes enabled.
    expect(doneButton).not.toBeDisabled();
  });
});

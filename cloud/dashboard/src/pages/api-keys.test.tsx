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

  // =============================================================================
  // API Key Rotation plan T6.5 — Rotate button + ConfirmDialog + show-once reveal
  // =============================================================================

  it('T6.5 — Rotate button per row opens ConfirmDialog with grace-window copy', async () => {
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
    const user = userEvent.setup();
    render(<ApiKeysPage />);
    expect(await screen.findByText('prod-agent')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /rotate api key prod-agent/i }));
    // ConfirmDialog body explains the grace window per D2.
    expect(await screen.findByText(/keep working for 24 hours/i)).toBeInTheDocument();
    expect(screen.getByText(/overlap during the grace window/i)).toBeInTheDocument();
  });

  it('T6.5 — successful rotation triggers the show-once secret reveal', async () => {
    mockCloudFetch.mockResolvedValueOnce({
      keys: [
        {
          id: 'apk-1111111111111111',
          name: 'prod-agent',
          key_prefix: 'loopzero_live_Ab',
          scopes: ['memory:read'],
          created_at: new Date().toISOString(),
        },
      ],
    });
    // Second mock = the POST /v1/api-keys/{id}/rotate response.
    mockCloudFetch.mockResolvedValueOnce({
      id: 'apk-2222222222222222',
      name: 'prod-agent (rotated 2026-06-06)',
      key_prefix: 'loopzero_live_Xy',
      scopes: ['memory:read'],
      secret: 'REDACTED_SECRET_017',
    });

    const user = userEvent.setup();
    render(<ApiKeysPage />);
    expect(await screen.findByText('prod-agent')).toBeInTheDocument();

    // Open rotate dialog → type confirmation → confirm.
    await user.click(screen.getByRole('button', { name: /rotate api key prod-agent/i }));
    const confirmInput = await screen.findByRole('textbox');
    await user.type(confirmInput, 'prod-agent');
    await user.click(screen.getByRole('button', { name: /rotate key/i }));

    // The new secret MUST appear in the show-once reveal dialog.
    expect(await screen.findByText(/loopzero_live_rotatedNEWsecret/)).toBeInTheDocument();
  });

  // =============================================================================
  // API Key Lineage View plan T7.4 — Lineage button + Sheet timeline
  // =============================================================================

  it('T7.4 — Lineage button per row opens Sheet with timeline + current highlight', async () => {
    mockCloudFetch.mockImplementation((path: string) => {
      if (String(path).includes('/v1/api-keys/apk-1111111111111111/lineage')) {
        return Promise.resolve({
          lineage: [
            {
              id: 'apk-1111111111111111',
              name: 'prod-agent (root)',
              key_prefix: 'loopzero_live_Aa',
              created_at: new Date(Date.now() - 86400000).toISOString(),
              revoked_at: null,
              expires_at: null,
              is_current: false,
            },
            {
              id: 'apk-2222222222222222',
              name: 'prod-agent (rotated 2026-06-06)',
              key_prefix: 'loopzero_live_Bb',
              created_at: new Date().toISOString(),
              revoked_at: null,
              expires_at: null,
              is_current: true,
            },
          ],
        });
      }
      return Promise.resolve({
        keys: [
          {
            id: 'apk-1111111111111111',
            name: 'prod-agent',
            key_prefix: 'loopzero_live_Aa',
            scopes: ['memory:read'],
            created_at: new Date().toISOString(),
          },
        ],
      });
    });

    const user = userEvent.setup();
    render(<ApiKeysPage />);
    expect(await screen.findByText('prod-agent')).toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /view lineage for api key prod-agent/i }));

    // Sheet renders timeline with both lineage rows.
    expect(await screen.findByTestId('lineage-row-apk-1111111111111111')).toBeInTheDocument();
    expect(screen.getByTestId('lineage-row-apk-2222222222222222')).toBeInTheDocument();
    // The is_current row has the Current badge.
    expect(screen.getByTestId('lineage-current-apk-2222222222222222')).toBeInTheDocument();
    // The non-current row does NOT have the Current badge.
    expect(screen.queryByTestId('lineage-current-apk-1111111111111111')).not.toBeInTheDocument();
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

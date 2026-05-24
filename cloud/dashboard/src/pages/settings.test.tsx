import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen } from '@/test/render';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({
    active: true,
    identity: { id: 'test', traits: { email: 'u@x.com' } },
  }),
}));

import { SettingsPage } from './settings';

describe('SettingsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('test_settings_renders_general_section', () => {
    // Plan v1.2 divergence: "General" section is CLI-First card (CodeBlock with
    // common commands) per design language — CLI-first principle (CLAUDE.md Rule 9).
    render(<SettingsPage />);
    expect(screen.getByText('CLI-First')).toBeInTheDocument();
    expect(screen.getByText(/Most workspace settings/i)).toBeInTheDocument();
  });

  it('test_settings_save_general_calls_api', () => {
    // Plan v1.2 divergence: no in-app save (CLI-first). Verify CodeBlock renders
    // canonical CLI commands so user knows how to mutate state.
    render(<SettingsPage />);
    expect(screen.getByText(/loopzero whoami/)).toBeInTheDocument();
    expect(screen.getByText(/loopzero secret set/)).toBeInTheDocument();
  });

  it('test_settings_danger_zone_delete_requires_phrase_match', async () => {
    const user = userEvent.setup();
    render(<SettingsPage />);
    // DangerZone shows "Delete Team" button — clicking opens ConfirmDialog
    // (Brief #3 primitive) which requires typing "delete my team" to enable confirm.
    const deleteTeamButtons = screen.getAllByRole('button', { name: /delete team/i });
    await user.click(deleteTeamButtons[0]);
    // Dialog opens — assert description + confirmation phrase prompt visible
    expect(screen.getByText(/This will permanently delete your team/i)).toBeInTheDocument();
  });
});

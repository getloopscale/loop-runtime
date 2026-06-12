import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ProjectStatusDetail } from '@/lib/api/types';
import { LiveUrlHero } from './live-url-hero';

/**
 * T1.1 of plan 2026-06-12-dashboard-journey-j25-live-url-discoverability v1.2.
 *
 * RED specs (5) per Implementation Contract + SEPA iter 2 pre-RED decisions:
 *  1. test_project_overview_renders_visit_button_when_status_url_populated
 *  2. test_project_overview_renders_pending_badge_when_status_url_null (ADR D3)
 *  3. test_overview_uses_default_production_env (EC-4)
 *  4. test_overview_renders_skeleton_while_status_loading (EC-6)
 *  5. test_overview_copy_button_writes_to_clipboard_and_shows_toast (EC-7 — ARIA-live)
 *
 * Per SEPA iter 2 CRITICAL fix: canonical URL is `https://teste-production.loopzero.example`
 * (status.go:252 emits `https://<project>-<env>.<baseDomain>`). NOT `teste.loopzero.example`
 * (v1.0 fabrication).
 *
 * Per SEPA iter 2 MAJOR: ProjectStatusDetail.url is `url?: string` (optional);
 * tests mock as `undefined` not `null`.
 *
 * Per SEPA iter 2 MAJOR: no Toast primitive in @loopzero/ui — use ARIA-live
 * transient state ("Copied" message in `<span aria-live="polite">`).
 */

const CANONICAL_URL = 'https://teste-production.loopzero.example';

function makeStatus(overrides: Partial<ProjectStatusDetail> = {}): ProjectStatusDetail {
  return {
    project: 'teste',
    environment: 'production',
    url: CANONICAL_URL,
    ...overrides,
  };
}

describe('LiveUrlHero', () => {
  let clipboardMock: { writeText: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    clipboardMock = { writeText: vi.fn().mockResolvedValue(undefined) };
    Object.defineProperty(navigator, 'clipboard', {
      value: clipboardMock,
      writable: true,
      configurable: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders visit button when status.url is populated', () => {
    render(<LiveUrlHero status={makeStatus()} loading={false} />);
    const link = screen.getByRole('link', { name: /visit/i });
    expect(link).toHaveAttribute('href', CANONICAL_URL);
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));
  });

  it('renders pending badge when status.url is absent (ADR D3)', () => {
    render(<LiveUrlHero status={makeStatus({ url: undefined })} loading={false} />);
    expect(screen.getByText(/URL pending/i)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /visit/i })).toBeNull();
  });

  it('renders skeleton while status is loading (EC-6)', () => {
    render(<LiveUrlHero status={null} loading={true} />);
    expect(screen.getByTestId('live-url-hero-skeleton')).toBeInTheDocument();
    expect(screen.queryByText(/URL pending/i)).toBeNull();
    expect(screen.queryByRole('link', { name: /visit/i })).toBeNull();
  });

  it('renders nothing destructive when status is null and not loading (idle fallback)', () => {
    render(<LiveUrlHero status={null} loading={false} />);
    expect(screen.queryByTestId('live-url-hero-skeleton')).toBeNull();
    expect(screen.getByText(/URL pending/i)).toBeInTheDocument();
  });

  it('writes URL to clipboard and shows ARIA-live Copied message on copy click (EC-7)', async () => {
    render(<LiveUrlHero status={makeStatus()} loading={false} />);
    const copyBtn = screen.getByRole('button', { name: /copy/i });

    await act(async () => {
      fireEvent.click(copyBtn);
    });

    await waitFor(() => {
      expect(clipboardMock.writeText).toHaveBeenCalledWith(CANONICAL_URL);
    });

    const live = screen.getByRole('status');
    await waitFor(() => {
      expect(live).toHaveTextContent(/copied/i);
    });
  });
});

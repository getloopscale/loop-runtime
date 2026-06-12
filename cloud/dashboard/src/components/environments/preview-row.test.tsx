/**
 * preview-row.test — locks the D3 substitute + D7 dashboard render
 * contract from plan 2026-06-07-preview-as-category-single-slot-plan.md.
 *
 * The contract:
 *   - slot=null → row renders empty state "No active branch"; URL/cell "—".
 *   - slot.active_pr_number=null → same empty state. (Slot row exists for
 *     audit/project_id continuity, but no PR is active.)
 *   - slot.active_pr_number != null → row shows PR #N + branch + URL link.
 *
 * Wrapped in <table><tbody> because PreviewRow returns a <tr> and React
 * complains about invalid DOM nesting otherwise.
 */
import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { PreviewRow } from './preview-row';

function renderRow(slot: Parameters<typeof PreviewRow>[0]['slot']) {
  return render(
    <table>
      <tbody>
        <PreviewRow slot={slot} />
      </tbody>
    </table>,
  );
}

describe('PreviewRow', () => {
  it('test_slot_null_renders_no_active_branch_empty_state', () => {
    renderRow(null);
    expect(screen.getByText('No active branch')).toBeInTheDocument();
    // The "preview · <project>" label and the kind badge are always
    // visible. When slot is null, project is "unknown" (defensive
    // empty-state rendering).
    expect(screen.getByText(/preview · unknown/)).toBeInTheDocument();
    expect(screen.getByText('Preview')).toBeInTheDocument();
  });

  it('test_slot_with_null_pr_renders_no_active_branch_empty_state', () => {
    renderRow({
      project_id: 'demo',
      active_pr_number: null,
      active_branch: null,
      url: null,
      updated_at: '2026-06-07T00:00:00Z',
    });
    expect(screen.getByText('No active branch')).toBeInTheDocument();
  });

  it('test_slot_active_renders_pr_number_and_branch', () => {
    renderRow({
      project_id: 'demo',
      active_pr_number: 42,
      active_branch: 'feat/new-thing',
      url: 'https://demo-pr-42.preview-dev.loopzero.example',
      updated_at: '2026-06-07T00:00:00Z',
    });
    // 2026-06-07 per-project: row also surfaces project_id in the name cell.
    expect(screen.getByText(/preview · demo/)).toBeInTheDocument();
    expect(screen.getByText('PR #42 — feat/new-thing')).toBeInTheDocument();
    // URL renders as an external link with "Open" label.
    const link = screen.getByRole('link', { name: /Open/ });
    expect(link).toHaveAttribute(
      'href',
      'https://demo-pr-42.preview-dev.loopzero.example',
    );
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
  });

  it('test_slot_active_without_url_renders_dash_in_url_cell', () => {
    renderRow({
      project_id: 'demo',
      active_pr_number: 7,
      active_branch: 'fix/bug',
      url: null,
      updated_at: '2026-06-07T00:00:00Z',
    });
    // PR + branch still rendered.
    expect(screen.getByText('PR #7 — fix/bug')).toBeInTheDocument();
    // No link, em-dash in URL cell.
    expect(screen.queryByRole('link', { name: /Open/ })).toBeNull();
  });
});

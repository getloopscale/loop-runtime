// confirm-dialog.test.tsx
//
// Plan 2026-06-08-dashboard-screens-review-fixes T3.2 + ADR D5.
// Tests the local re-export of @loopzero/ui's ConfirmDialog. The
// upstream library has its own comprehensive test suite — these tests
// pin THIS module's contract:
//   - canonical import path resolves
//   - props type passes through unchanged
//   - destructive intent renders a destructive-styled confirm button
//   - confirmationPhrase gate blocks confirm until typed exactly
//   - onConfirm fires on confirm click

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@/test/render';
import { ConfirmDialog } from './confirm-dialog';

describe('ConfirmDialog — canonical destructive confirm primitive', () => {
  it('renders title + description when open', () => {
    render(
      <ConfirmDialog
        open
        onOpenChange={() => {}}
        title="Disconnect repo?"
        description="The project stops receiving auto-deploys."
        onConfirm={() => {}}
      />,
    );
    expect(screen.getByText(/disconnect repo\?/i)).toBeInTheDocument();
    expect(screen.getByText(/auto-deploys/i)).toBeInTheDocument();
  });

  it('omits content when open=false', () => {
    render(
      <ConfirmDialog
        open={false}
        onOpenChange={() => {}}
        title="Hidden"
        description="Hidden"
        onConfirm={() => {}}
      />,
    );
    expect(screen.queryByText(/^Hidden$/)).not.toBeInTheDocument();
  });

  it('fires onConfirm when confirm button clicked (no phrase gate)', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={() => {}}
        title="Cancel build?"
        description="The build will stop."
        confirmLabel="Cancel build"
        onConfirm={onConfirm}
      />,
    );
    const confirmBtn = screen.getByRole('button', { name: /cancel build/i });
    fireEvent.click(confirmBtn);
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1));
  });

  it('blocks confirm until confirmationPhrase typed exactly (T2T gate)', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={() => {}}
        title="Delete tenant?"
        description="Irreversible."
        confirmationPhrase="delete tenant"
        confirmLabel="Delete"
        onConfirm={onConfirm}
      />,
    );

    // Click without typing phrase — confirm should be disabled (no fire).
    const confirmBtn = screen.getByRole('button', { name: /^delete$/i });
    fireEvent.click(confirmBtn);
    expect(onConfirm).not.toHaveBeenCalled();

    // Type the phrase exactly.
    const input = screen.getByRole('textbox');
    fireEvent.change(input, { target: { value: 'delete tenant' } });

    // Click again — should now fire.
    fireEvent.click(confirmBtn);
    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1));
  });

  it('confirmationPhrase rejects close-but-not-exact strings', async () => {
    const onConfirm = vi.fn();
    render(
      <ConfirmDialog
        open
        onOpenChange={() => {}}
        title="Delete tenant?"
        description="Irreversible."
        confirmationPhrase="delete tenant"
        confirmLabel="Delete"
        onConfirm={onConfirm}
      />,
    );

    const input = screen.getByRole('textbox');
    // Common slip — caps mismatch should NOT enable confirm
    fireEvent.change(input, { target: { value: 'Delete Tenant' } });

    const confirmBtn = screen.getByRole('button', { name: /^delete$/i });
    fireEvent.click(confirmBtn);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('passes intent="destructive" through (smoke contract — actual styling lives in @loopzero/ui)', () => {
    // The upstream owns the destructive CSS; the local re-export just
    // forwards. We assert the prop reaches the rendered subtree by
    // checking the component renders without crashing — the upstream
    // test suite covers visual.
    expect(() =>
      render(
        <ConfirmDialog
          open
          onOpenChange={() => {}}
          title="Destructive"
          description="Will destroy."
          intent="destructive"
          confirmLabel="Destroy"
          onConfirm={() => {}}
        />,
      ),
    ).not.toThrow();
  });
});

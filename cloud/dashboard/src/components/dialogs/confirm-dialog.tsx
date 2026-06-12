// confirm-dialog.tsx
//
// Plan 2026-06-08-dashboard-screens-review-fixes T3.2 + ADR D5.
//
// Re-export wrapper over `@loopzero/ui`'s ConfirmDialog primitive. The
// upstream library already implements the FAANG-grade destructive flow:
//   - open / onOpenChange
//   - title + description (ReactNode)
//   - confirmLabel + cancelLabel
//   - intent: "default" | "destructive"
//   - confirmationPhrase: type-to-confirm gate (operator MUST type the
//     exact phrase before the confirm button enables)
//   - onConfirm: () => void | Promise<void> (loading state managed
//     internally during async)
//
// The local re-export exists so the dashboard codebase has ONE
// canonical import path for destructive confirmations:
//
//   import { ConfirmDialog } from '@/components/dialogs/confirm-dialog';
//
// vs scattering `import { ConfirmDialog } from '@loopzero/ui'` across
// pages. When a future plan tightens the contract (e.g. add a default
// "destructive" intent OR force a confirmationPhrase for production
// targets) this is the single seam to modify.
//
// AUDIT NOTE (2026-06-08, T3.2 plan body):
//   - delete-environment-dialog → bespoke type-to-confirm (exempt per
//     ADR D5 — env destruction is too sensitive for the generic
//     primitive)
//   - rollback-dialog            → bespoke type-to-confirm for
//     production env (exempt — same rationale)
//   - cancel-build-dialog        → bespoke single-click confirm via
//     Dialog + Alert (exempt — destructive but recoverable; J-17 T6.2
//     shipped the canonical pattern)
//   - promote-dialog             → bespoke focus-on-Cancel default
//     (Vercel/Stripe convention); T4.4 will add cross-project toggle
//     per EC-4 absorption
//
// So this primitive is intentionally idle until a NEW destructive
// action surface needs confirmation — at that point, prefer this
// over a fifth bespoke implementation (DRY rule of three already
// satisfied above).

import { ConfirmDialog as UpstreamConfirmDialog } from '@loopzero/ui';
import type { ConfirmDialogProps as UpstreamConfirmDialogProps } from '@loopzero/ui';

export type ConfirmDialogProps = UpstreamConfirmDialogProps;

/**
 * ConfirmDialog — canonical destructive-action confirmation dialog.
 *
 * @example  // Simple destructive (e.g. "Disconnect repo")
 *   <ConfirmDialog
 *     open={open}
 *     onOpenChange={setOpen}
 *     title="Disconnect repository?"
 *     description="The project will stop receiving auto-deploys."
 *     intent="destructive"
 *     confirmLabel="Disconnect"
 *     onConfirm={() => disconnect.mutate()}
 *   />
 *
 * @example  // Type-to-confirm (high-stakes — e.g. "Delete tenant")
 *   <ConfirmDialog
 *     open={open}
 *     onOpenChange={setOpen}
 *     title="Delete tenant?"
 *     description="This is irreversible."
 *     intent="destructive"
 *     confirmationPhrase="delete tenant"
 *     confirmLabel="I understand, delete"
 *     onConfirm={() => deleteTenant.mutate()}
 *   />
 */
export const ConfirmDialog = UpstreamConfirmDialog;

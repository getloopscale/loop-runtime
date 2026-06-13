import { Loader2, XCircle } from 'lucide-react';
import { Alert, Button, Dialog } from '@loopzero/ui';
import { useCancelBuild } from '@/hooks/mutations/use-cancel-build';
import type { Build } from '@/lib/api/types';

/**
 * CancelBuildDialog — Plan J-17 T6.2.
 *
 * Confirmation dialog for `loopzero build cancel`. The previous BuildHeader
 * stub fired Cancel on the very first click, which doesn't match the
 * Vercel-grade safety bar — a deploy in flight can be days of compile
 * time on a Pro plan and accidental cancels are expensive. This dialog
 * forces a deliberate second action before talking to the backend.
 *
 * The cancellation event flows over SSE; the open BuildTimelineLive
 * picks up `build.cancelled` and animates the transition without any
 * extra wiring here.
 */
export interface CancelBuildDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  build: Build;
  onCancelled?: () => void;
}

export function CancelBuildDialog({
  open,
  onOpenChange,
  build,
  onCancelled,
}: CancelBuildDialogProps) {
  const cancelBuild = useCancelBuild();

  const handleConfirm = async () => {
    try {
      await cancelBuild.mutateAsync(build.build_id);
      onCancelled?.();
      onOpenChange(false);
    } catch {
      // surfaced in cancelBuild.error
    }
  };

  const errorMessage = cancelBuild.error
    ? cancelBuild.error instanceof Error
      ? cancelBuild.error.message
      : 'Cancel failed. Try again in a moment.'
    : null;

  const sha = build.commit_sha.slice(0, 7);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title className="inline-flex items-center gap-2">
            <XCircle className="size-5 text-destructive" aria-hidden="true" />
            Cancel build {sha}
          </Dialog.Title>
          <Dialog.Description>
            This stops the build at the current step. Any partial work is
            discarded. The active release in{' '}
            <span className="font-mono">{build.environment}</span> is unaffected.
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-4">
          <Alert
            intent="warning"
            description="If the build was about to succeed you'll need to push a new commit or redeploy to retry."
          />

          {errorMessage && (
            <Alert intent="destructive" description={errorMessage} />
          )}

          <Dialog.Footer>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={cancelBuild.isPending}
            >
              Keep building
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={handleConfirm}
              disabled={cancelBuild.isPending}
            >
              {cancelBuild.isPending && (
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
              )}
              {cancelBuild.isPending ? 'Cancelling…' : `Cancel build`}
            </Button>
          </Dialog.Footer>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}

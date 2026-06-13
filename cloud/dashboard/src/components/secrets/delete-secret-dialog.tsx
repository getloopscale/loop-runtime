import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Dialog, Input, Label } from '@loopzero/ui';
import { useDeleteSecret } from '@/hooks/mutations/use-secrets-mutations';
import type { Secret } from '@/lib/api/types';

interface DeleteSecretDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  secret: Secret | null;
  /** Env to remove the secret from (one per dialog instance). */
  environment?: string;
  project?: string;
  onDeleted?: () => void;
}

/**
 * J-22 T3.4 — DeleteSecretDialog (Vercel-canonical destructive pattern).
 *
 * Same contract as RotateSecretDialog (T3.3): Delete button is DISABLED
 * until the user types the EXACT secret name (case-sensitive — EC-19)
 * into the confirm input. Paste-allowed; case-mismatch keeps disabled.
 *
 * Per-env scope: each delete removes the secret from ONE environment.
 * Removing from all envs requires multiple deletes (or future bulk-delete).
 */
export function DeleteSecretDialog({
  open,
  onOpenChange,
  secret,
  environment,
  project,
  onDeleted,
}: DeleteSecretDialogProps) {
  const [confirmInput, setConfirmInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const deleteSecret = useDeleteSecret();
  const submitting = deleteSecret.isPending;

  const reset = () => {
    setConfirmInput('');
    setError(null);
  };

  // EC-19: exact match, case-sensitive.
  const canDelete = secret !== null && confirmInput === secret.name;

  const handleDelete = async () => {
    if (!canDelete || secret === null) return;
    setError(null);
    try {
      const env =
        environment ?? (secret.environments && secret.environments[0]) ?? 'production';
      await deleteSecret.mutateAsync({
        name: secret.name,
        environment: env,
        project,
      });
      reset();
      onOpenChange(false);
      onDeleted?.();
    } catch (err) {
      setError(
        err instanceof Error
          ? `Delete failed: ${err.message}. The secret was NOT removed.`
          : 'Delete failed. The secret was NOT removed.',
      );
    }
  };

  if (!secret) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete {secret.name}</Dialog.Title>
          <Dialog.Description>
            This permanently removes the secret from {environment ?? 'this environment'}.
            Apps reading it will fail on next deploy.
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-4 py-2">
          <div>
            <Label htmlFor="delete-confirm">
              Type <span className="font-mono font-semibold">{secret.name}</span> to confirm
            </Label>
            <Input
              id="delete-confirm"
              type="text"
              value={confirmInput}
              onChange={(e) => setConfirmInput(e.target.value)}
              autoFocus
            />
          </div>

          {error && <Alert intent="destructive" description={error} />}
        </div>

        <Dialog.Footer>
          <Button
            type="button"
            variant="ghost"
            onClick={() => {
              reset();
              onOpenChange(false);
            }}
            disabled={submitting}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            onClick={handleDelete}
            disabled={!canDelete || submitting}
          >
            {submitting ? (
              <>
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                Deleting…
              </>
            ) : (
              'Delete'
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

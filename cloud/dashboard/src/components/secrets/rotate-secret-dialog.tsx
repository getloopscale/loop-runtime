import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Dialog, Input, Label } from '@loopzero/ui';
import { useRotateSecret } from '@/hooks/mutations/use-secrets-mutations';
import type { Secret } from '@/lib/api/types';

interface RotateSecretDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  secret: Secret | null;
  /** Optional project param forwarded to the backend. */
  project?: string;
  onRotated?: () => void;
}

/**
 * J-22 T3.3 — RotateSecretDialog (Vercel-canonical destructive pattern).
 *
 * D3 (type-to-confirm): Rotate button is disabled until the user types
 * the EXACT secret name (case-sensitive — EC-19) into the confirm input.
 * Paste is allowed; case-mismatch keeps Rotate disabled (a pasted
 * `database_url` against current `DATABASE_URL` does not unlock).
 *
 * EC-13 (optimistic concurrency): the mutation carries `expected_version`
 * read from secret.version. Server returns 409 STALE_VERSION when the
 * row has advanced since the dialog opened; the UI surfaces the message
 * and the user re-fetches before retrying.
 */
export function RotateSecretDialog({
  open,
  onOpenChange,
  secret,
  project,
  onRotated,
}: RotateSecretDialogProps) {
  const [confirmInput, setConfirmInput] = useState('');
  const [newValue, setNewValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const rotateSecret = useRotateSecret();
  const submitting = rotateSecret.isPending;

  const reset = () => {
    setConfirmInput('');
    setNewValue('');
    setError(null);
  };

  // EC-19: exact match, case-sensitive.
  const canRotate = secret !== null && confirmInput === secret.name && newValue.length > 0;

  const handleRotate = async () => {
    if (!canRotate || secret === null) return;
    setError(null);
    try {
      // EC-13: read the current version from the secret prop; backend
      // checks expected_version on the server side and rejects 409
      // STALE_VERSION when the row has advanced.
      const env =
        (secret.environments && secret.environments[0]) ?? 'production';
      const expectedVersion = (secret as { version?: number }).version ?? 1;
      await rotateSecret.mutateAsync({
        name: secret.name,
        newValue,
        expectedVersion,
        environment: env,
        project,
      });
      reset();
      onOpenChange(false);
      onRotated?.();
    } catch (err) {
      setError(
        err instanceof Error
          ? `Rotate failed: ${err.message}. The secret was NOT changed.`
          : 'Rotate failed. The secret was NOT changed.',
      );
    }
  };

  if (!secret) return null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Rotate {secret.name}</Dialog.Title>
          <Dialog.Description>
            The current value will be overwritten. Type the secret name to confirm.
          </Dialog.Description>
        </Dialog.Header>

        <div className="space-y-4 py-2">
          <div>
            <Label htmlFor="rotate-confirm">
              Type <span className="font-mono font-semibold">{secret.name}</span> to confirm
            </Label>
            <Input
              id="rotate-confirm"
              type="text"
              value={confirmInput}
              onChange={(e) => setConfirmInput(e.target.value)}
              autoFocus
            />
          </div>

          <div>
            <Label htmlFor="rotate-new-value">New value</Label>
            <Input
              id="rotate-new-value"
              type="password"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
              placeholder="sk_live_•••"
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
          <Button type="button" onClick={handleRotate} disabled={!canRotate || submitting}>
            {submitting ? (
              <>
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                Rotating…
              </>
            ) : (
              'Rotate'
            )}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

import { useState } from 'react';
import { Loader2, Undo2 } from 'lucide-react';
import { Alert, Button, Dialog, Input, Label } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import type { Build, RollbackResponse } from '@/lib/api/types';

interface RollbackDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectName: string;
  targetBuild: Build;
  onRolledBack?: (response: RollbackResponse) => void;
}

const CONFIRMATION_PHRASE = 'rollback production';

export function RollbackDialog({
  open,
  onOpenChange,
  projectName,
  targetBuild,
  onRolledBack,
}: RollbackDialogProps) {
  const [phrase, setPhrase] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const isProd = targetBuild.environment === 'production';
  const requiresPhrase = isProd;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (requiresPhrase && phrase !== CONFIRMATION_PHRASE) {
      setError(`Type "${CONFIRMATION_PHRASE}" exactly to confirm.`);
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<RollbackResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName)}/rollback`,
        {
          method: 'POST',
          body: JSON.stringify({ build_id: targetBuild.build_id }),
        },
      );
      if (res === null) {
        setError("Couldn't trigger rollback. Try again in a moment.");
        return;
      }
      onRolledBack?.(res);
      setPhrase('');
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.');
    } finally {
      setSubmitting(false);
    }
  };

  const sha = targetBuild.commit_sha.slice(0, 7);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title className="inline-flex items-center gap-2">
            <Undo2 className="size-5 text-warning" aria-hidden="true" />
            Rollback to {sha}
          </Dialog.Title>
          <Dialog.Description>
            This will replace the current release in <span className="font-mono">{targetBuild.environment}</span> with the build from commit <span className="font-mono">{sha}</span>.
          </Dialog.Description>
        </Dialog.Header>

        <form onSubmit={handleSubmit} className="space-y-4">
          <Alert
            intent="warning"
            description={`Traffic will switch within a few seconds. Existing connections drain gracefully.`}
          />

          {requiresPhrase && (
            <div>
              <Label htmlFor="rollback-phrase">
                Type <span className="font-mono">{CONFIRMATION_PHRASE}</span> to confirm
              </Label>
              <Input
                id="rollback-phrase"
                type="text"
                value={phrase}
                onChange={(e) => setPhrase(e.target.value)}
                autoFocus
                required
              />
            </div>
          )}

          {error && <Alert intent="destructive" description={error} />}

          <Dialog.Footer>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant="destructive"
              disabled={submitting || (requiresPhrase && phrase !== CONFIRMATION_PHRASE)}
            >
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Rolling back…' : `Rollback to ${sha}`}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

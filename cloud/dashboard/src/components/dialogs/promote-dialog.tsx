import { useState } from 'react';
import { ArrowRight, Loader2, Rocket } from 'lucide-react';
import { Alert, Button, Dialog, Input, Label } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import type { PromoteResponse } from '@/lib/api/types';

interface PromoteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projectName: string;
  onPromoted?: (response: PromoteResponse) => void;
}

export function PromoteDialog({
  open,
  onOpenChange,
  projectName,
  onPromoted,
}: PromoteDialogProps) {
  const [fromEnv, setFromEnv] = useState('staging');
  const [toEnv, setToEnv] = useState('production');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!fromEnv.trim() || !toEnv.trim()) {
      setError('Both source and target environments are required.');
      return;
    }
    if (fromEnv === toEnv) {
      setError('Source and target must differ.');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<PromoteResponse>(
        `/api/v1/projects/${encodeURIComponent(projectName)}/promote`,
        {
          method: 'POST',
          body: JSON.stringify({
            from_environment: fromEnv,
            to_environment: toEnv,
          }),
        },
      );
      if (res === null) {
        setError("Couldn't trigger promote. Try again in a moment.");
        return;
      }
      onPromoted?.(res);
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      setError(
        e instanceof Error
          ? `Promote failed: ${e.message}. The current release remains active. Retry, or rollback if it persists.`
          : 'Promote failed. The current release remains active. Retry, or contact support if it persists.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title className="inline-flex items-center gap-2">
            <Rocket className="size-5 text-primary" aria-hidden="true" />
            Promote release
          </Dialog.Title>
          <Dialog.Description>
            Copy the current release from source to target without rebuilding.
          </Dialog.Description>
        </Dialog.Header>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="flex items-end gap-2">
            <div className="flex-1">
              <Label htmlFor="promote-from">From</Label>
              <Input
                id="promote-from"
                type="text"
                value={fromEnv}
                onChange={(e) => setFromEnv(e.target.value)}
                required
              />
            </div>
            <ArrowRight
              className="mb-2 size-5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
            <div className="flex-1">
              <Label htmlFor="promote-to">To</Label>
              <Input
                id="promote-to"
                type="text"
                value={toEnv}
                onChange={(e) => setToEnv(e.target.value)}
                required
              />
            </div>
          </div>

          <Alert
            intent="warning"
            description="Preflight checks (TLS valid, migrations applied, smoke test green) run before traffic switches."
          />

          {error && <Alert intent="destructive" description={error} />}

          <Dialog.Footer>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Promoting…' : `Promote to ${toEnv}`}
            </Button>
          </Dialog.Footer>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

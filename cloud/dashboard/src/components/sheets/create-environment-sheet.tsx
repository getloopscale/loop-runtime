import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Checkbox, Input, Label, Sheet } from '@loopzerokit/ui';
import { engineFetch } from '@/lib/api';
import type { Environment } from '@/lib/api/types';

interface CreateEnvironmentSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (env: Environment) => void;
}

const NAME_PATTERN = /^[a-z][a-z0-9-]{1,38}[a-z0-9]$/;

/**
 * CreateEnvironmentSheet — right-side Sheet (2026 FAANG pattern).
 * Sheet keeps environment table in view so user verifies they're not
 * duplicating a name.
 */
export function CreateEnvironmentSheet({ open, onOpenChange, onCreated }: CreateEnvironmentSheetProps) {
  const [name, setName] = useState('');
  const [isProtected, setIsProtected] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!NAME_PATTERN.test(name)) {
      setError('Name must be lowercase, 3-40 chars, hyphens allowed.');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<Environment>('/api/v1/environments', {
        method: 'POST',
        body: JSON.stringify({ name, protected: isProtected }),
      });
      if (res === null) {
        setError("Couldn't create environment. Try again in a moment.");
        return;
      }
      onCreated?.(res);
      setName('');
      setIsProtected(false);
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      setError(
        e instanceof Error
          ? `Create environment failed: ${e.message}. No environment was created. Retry, or contact support if it persists.`
          : 'Create environment failed. No environment was created. Retry, or contact support if it persists.',
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>New environment</Sheet.Title>
            <Sheet.Description>
              Environments are isolated namespaces for your deployments.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="env-name">Name</Label>
              <Input
                id="env-name"
                type="text"
                placeholder="staging"
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                autoFocus
                required
              />
            </div>

            <div className="flex items-start gap-2">
              <Checkbox
                id="env-protected"
                checked={isProtected}
                onCheckedChange={(c) => setIsProtected(c === true)}
              />
              <div>
                <Label htmlFor="env-protected">Protected</Label>
                <p className="text-label text-muted-foreground">
                  Requires explicit promote/rollback confirmation. Use for production-like envs.
                </p>
              </div>
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Creating…' : 'Create environment'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}

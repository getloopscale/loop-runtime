import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Input, Label, Sheet } from '@loopzerokit/ui';
import { engineFetch } from '@/lib/api';
import type { Secret } from '@/lib/api/types';

interface AddSecretSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: (secret: Secret) => void;
}

const KEY_PATTERN = /^[A-Z][A-Z0-9_]{1,63}$/;

/**
 * AddSecretSheet — right-side Sheet (2026 FAANG pattern).
 * Sheet allows secret list to stay visible (helps user verify they're not
 * about to add a duplicate key).
 */
export function AddSecretSheet({ open, onOpenChange, onCreated }: AddSecretSheetProps) {
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [environments, setEnvironments] = useState('production');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reset = () => {
    setName('');
    setValue('');
    setEnvironments('production');
    setError(null);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!KEY_PATTERN.test(name)) {
      setError('Key must be SCREAMING_SNAKE_CASE, 2-64 chars.');
      return;
    }
    if (!value.trim()) {
      setError('Value is required.');
      return;
    }
    setSubmitting(true);
    try {
      const envs = environments
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
      const res = await engineFetch<Secret>('/api/v1/secrets', {
        method: 'POST',
        body: JSON.stringify({ name, value, environments: envs }),
      });
      if (res === null) {
        setError("Couldn't reach the secrets endpoint. Try again in a moment.");
        return;
      }
      onCreated?.(res);
      reset();
      onOpenChange(false);
    } catch (e) {
      // T0.1 TCIA — sponsor critério #6
      setError(
        e instanceof Error
          ? `Add secret failed: ${e.message}. No secret was stored. Retry, or contact support if it persists.`
          : 'Add secret failed. No secret was stored. Retry, or contact support if it persists.',
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
            <Sheet.Title>Add secret</Sheet.Title>
            <Sheet.Description>
              Secret values are encrypted at rest and never logged.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="secret-name">Key</Label>
              <Input
                id="secret-name"
                type="text"
                placeholder="STRIPE_SECRET_KEY"
                value={name}
                onChange={(e) => setName(e.target.value.toUpperCase())}
                autoFocus
                required
              />
              <p className="mt-1 text-label text-muted-foreground">
                SCREAMING_SNAKE_CASE, 2-64 characters.
              </p>
            </div>

            <div>
              <Label htmlFor="secret-value">Value</Label>
              <Input
                id="secret-value"
                type="password"
                placeholder="sk_live_•••"
                value={value}
                onChange={(e) => setValue(e.target.value)}
                required
              />
            </div>

            <div>
              <Label htmlFor="secret-envs">Environments</Label>
              <Input
                id="secret-envs"
                type="text"
                placeholder="production, staging"
                value={environments}
                onChange={(e) => setEnvironments(e.target.value)}
              />
              <p className="mt-1 text-label text-muted-foreground">
                Comma-separated list. Use a single name to scope to one env.
              </p>
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Saving…' : 'Save secret'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}

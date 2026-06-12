import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Input, Label, Sheet } from '@loopzero/ui';
import { useCreateSecret } from '@/hooks/mutations/use-secrets-mutations';

interface AddSecretSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Default env(s) to pre-select; typically derived from URL ?env= filter (D4). */
  defaultEnvs?: string[];
  /** Available envs for multi-select. */
  availableEnvs?: string[];
  /** Optional project param forwarded to the backend. */
  project?: string;
  onCreated?: () => void;
}

const KEY_PATTERN = /^[A-Z][A-Z0-9_]{1,63}$/;

/**
 * J-22 T3.2 — AddSecretSheet (canonical).
 *
 * Replaces legacy `components/sheets/add-secret-sheet.tsx` which had a
 * frontend/backend schema mismatch (sent `environments: []` array; backend
 * `Set` expects `environment: string`). Canonical version uses the
 * useCreateSecret mutation (T2.2) and the existing single-env POST path
 * via per-env iteration in the parent hook OR a future bulk wire.
 *
 * D4 (env multi-select): availableEnvs prop populates checkboxes; pre-selected
 * via defaultEnvs (typically derived from URL ?env= filter).
 *
 * D1 (masking forever): no preview of the entered value after submit;
 * onCreated callback closes the sheet without echoing the plaintext.
 */
export function AddSecretSheet({
  open,
  onOpenChange,
  defaultEnvs = ['production'],
  availableEnvs = ['production', 'staging'],
  project,
  onCreated,
}: AddSecretSheetProps) {
  const [name, setName] = useState('');
  const [value, setValue] = useState('');
  const [selectedEnvs, setSelectedEnvs] = useState<string[]>(defaultEnvs);
  const [error, setError] = useState<string | null>(null);
  const createSecret = useCreateSecret();
  const submitting = createSecret.isPending;

  const reset = () => {
    setName('');
    setValue('');
    setSelectedEnvs(defaultEnvs);
    setError(null);
  };

  const toggleEnv = (env: string) => {
    setSelectedEnvs((current) =>
      current.includes(env) ? current.filter((e) => e !== env) : [...current, env],
    );
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
    if (selectedEnvs.length === 0) {
      setError('Pick at least one environment.');
      return;
    }
    try {
      await createSecret.mutateAsync({ name, value, environments: selectedEnvs, project });
      reset();
      onOpenChange(false);
      onCreated?.();
    } catch (err) {
      setError(
        err instanceof Error
          ? `Add secret failed: ${err.message}. No secret was stored.`
          : 'Add secret failed. No secret was stored.',
      );
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>Add secret</Sheet.Title>
            <Sheet.Description>
              Encrypted at rest. Value is never logged or shown again after submit.
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

            <fieldset>
              <legend className="text-body-sm font-medium">Environments</legend>
              <div className="mt-2 flex flex-wrap gap-3">
                {availableEnvs.map((env) => (
                  <label key={env} className="flex items-center gap-2 text-body-sm">
                    <input
                      type="checkbox"
                      checked={selectedEnvs.includes(env)}
                      onChange={() => toggleEnv(env)}
                      aria-label={`Scope to ${env}`}
                    />
                    {env}
                  </label>
                ))}
              </div>
            </fieldset>

            {error && <Alert intent="destructive" description={error} className="mt-2" />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 flex justify-end gap-2">
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
            <Button type="submit" disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                  Adding…
                </>
              ) : (
                'Add secret'
              )}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}

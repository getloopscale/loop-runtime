import { useMemo, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Sheet, Textarea } from '@loopzero/ui';
import { useBulkUploadSecrets } from '@/hooks/mutations/use-secrets-mutations';
import { parseDotenv } from '@/lib/dotenv';

interface BulkUploadSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  project: string;
  /** Default env(s) the bulk entries get scoped to. */
  defaultEnvs?: string[];
  availableEnvs?: string[];
  onUploaded?: (createdCount: number) => void;
}

/**
 * J-22 T3.5 — BulkUploadSheet.
 *
 * Client-side `.env` parser (lib/dotenv.ts) renders a preview with per-key
 * status + warnings BEFORE the user submits. Submit calls the T1.1 bulk
 * endpoint via useBulkUploadSecrets (T2.2) — atomic per-key savepoints
 * (EC-9), size cap 100 (EC-18), plan limit gate (T1.3) all enforced
 * server-side.
 *
 * EC-17 quoted values + comments handled by the parser.
 * EC-14 duplicate-key pre-flight warning surfaced as inline warning row.
 */
export function BulkUploadSheet({
  open,
  onOpenChange,
  project,
  defaultEnvs = ['production'],
  availableEnvs = ['production', 'staging'],
  onUploaded,
}: BulkUploadSheetProps) {
  const [source, setSource] = useState('');
  const [selectedEnvs, setSelectedEnvs] = useState<string[]>(defaultEnvs);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const bulkUpload = useBulkUploadSecrets();
  const submitting = bulkUpload.isPending;

  const parsed = useMemo(() => parseDotenv(source), [source]);
  const canSubmit = parsed.entries.length > 0 && selectedEnvs.length > 0;

  const toggleEnv = (env: string) => {
    setSelectedEnvs((current) =>
      current.includes(env) ? current.filter((e) => e !== env) : [...current, env],
    );
  };

  const handleSubmit = async () => {
    if (!canSubmit) return;
    setSubmitError(null);
    try {
      const result = await bulkUpload.mutateAsync({
        project,
        secrets: parsed.entries.map((e) => ({
          name: e.key,
          value: e.value,
          envs: selectedEnvs,
        })),
      });
      const created = (result?.results ?? []).filter((r) => r.status === 'created').length;
      setSource('');
      onOpenChange(false);
      onUploaded?.(created);
    } catch (err) {
      setSubmitError(
        err instanceof Error
          ? `Bulk upload failed: ${err.message}. No partial entries were stored.`
          : 'Bulk upload failed.',
      );
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-lg flex flex-col p-0">
        <Sheet.Header className="px-6 py-5 border-b border-border/60">
          <Sheet.Title>Bulk upload .env</Sheet.Title>
          <Sheet.Description>
            Paste your .env content. We preview before posting; max 100 keys per batch.
          </Sheet.Description>
        </Sheet.Header>

        <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
          <div>
            <label htmlFor="dotenv-source" className="text-body-sm font-medium">
              .env content
            </label>
            <Textarea
              id="dotenv-source"
              rows={10}
              placeholder={'DATABASE_URL=postgres://...\nSTRIPE_KEY="sk_live_..."'}
              value={source}
              onChange={(e) => setSource(e.target.value)}
              className="mt-1 font-mono text-body-sm"
            />
          </div>

          <fieldset>
            <legend className="text-body-sm font-medium">Scope to envs</legend>
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

          <div className="space-y-2" aria-label="Preview">
            <div className="text-body-sm font-medium">
              Preview: {parsed.entries.length} entries · {parsed.warnings.length} warnings
            </div>
            {parsed.warnings.map((w) => (
              <div
                key={`${w.line}-${w.message}`}
                className="text-label text-amber-600 dark:text-amber-400"
              >
                Line {w.line}: {w.message}
              </div>
            ))}
            {parsed.entries.length > 0 && (
              <ul className="text-label font-mono text-muted-foreground">
                {parsed.entries.map((e) => (
                  <li key={e.key}>
                    {e.key}=••••••••
                  </li>
                ))}
              </ul>
            )}
          </div>

          {submitError && <Alert intent="destructive" description={submitError} />}
        </Sheet.Body>

        <Sheet.Footer className="px-6 py-4 border-t border-border/60 flex justify-end gap-2">
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            Cancel
          </Button>
          <Button type="button" onClick={handleSubmit} disabled={!canSubmit || submitting}>
            {submitting ? (
              <>
                <Loader2 className="size-4 animate-spin" aria-hidden="true" />
                Uploading…
              </>
            ) : (
              `Upload ${parsed.entries.length} secret(s)`
            )}
          </Button>
        </Sheet.Footer>
      </Sheet.Content>
    </Sheet>
  );
}

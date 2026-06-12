import { useState } from 'react';
import { Link, useParams, useSearchParams } from 'react-router-dom';
import { ArrowLeft, KeyRound, Plus, Upload } from 'lucide-react';
import { Button, Card, PageShell } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useSecrets } from '@/hooks/use-secrets';
import { useEnvironments } from '@/hooks/use-environments';
import { SecretsTable } from '@/components/secrets/secrets-table';
import { AddSecretSheet } from '@/components/secrets/add-secret-sheet';
import { RotateSecretDialog } from '@/components/secrets/rotate-secret-dialog';
import { DeleteSecretDialog } from '@/components/secrets/delete-secret-dialog';
import { BulkUploadSheet } from '@/components/secrets/bulk-upload-sheet';
import type { Secret } from '@/lib/api/types';
import { DEFAULT_PERMANENT_ENV } from '@/lib/domain';

/**
 * SecretsPage — J-22 T4.1.
 *
 * Vercel-canonical: lists secrets (D1 masked); per-row Rotate + Delete
 * dialogs; Add sheet + Bulk Upload sheet. Page-level integration of the
 * components shipped in T3.1-T3.5.
 *
 * URL state — T4.2 ties ?env= filter into useSecrets({env}).
 *
 * Plan 2026-06-11 T1.4 — env filter options now derived dynamically
 * from useEnvironments(name). The previous hardcoded ENV_OPTIONS
 * `['all', 'production', 'staging']` silently dropped custom envs like
 * `staging-eu-1` or `preview-pr-42` from the filter chips, breaking
 * J-22↔J-10 contract whenever a tenant departed from the default tier.
 */

export function SecretsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const envFilter = searchParams.get('env') ?? undefined;
  useSetPageTitle(`${name} · Secrets`);
  const { secrets, loading } = useSecrets({ project: name, env: envFilter });

  // T1.4: env filter options derived from useEnvironments(name) instead
  // of a hardcoded literal. Closes the J-22↔J-10 silent break.
  // EC-5: hook already falls back to [DEFAULT_PERMANENT_ENV] on error.
  const { envs: projectEnvs, error: envsError } = useEnvironments(name);
  const ENV_OPTIONS = ['all', ...projectEnvs] as const;

  // T4.2: URL is the source of truth — toggling filter calls
  // setSearchParams which triggers useSecrets refetch (queryKey changes).
  const handleEnvChange = (next: string) => {
    const params = new URLSearchParams(searchParams);
    if (next === 'all') {
      params.delete('env');
    } else {
      params.set('env', next);
    }
    setSearchParams(params, { replace: true });
  };

  const [addOpen, setAddOpen] = useState(false);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [rotateTarget, setRotateTarget] = useState<Secret | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Secret | null>(null);

  const defaultEnvs = envFilter ? [envFilter] : [DEFAULT_PERMANENT_ENV];

  return (
    <>
    <PageShell
      title="Secrets"
      description={`Environment variables for ${name}. Values are encrypted at rest and masked in the UI.`}
      primaryAction={{
        label: 'Add secret',
        icon: Plus,
        onClick: () => setAddOpen(true),
      }}
      loading={loading}
      empty={
        !loading && secrets.length === 0
          ? {
              icon: KeyRound,
              title: 'No secrets yet',
              description:
                'Add a secret to make it available to your apps at runtime.',
            }
          : undefined
      }
    >
      <div className="mb-4 flex items-center justify-between gap-4 flex-wrap">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
        <div className="flex items-center gap-2">
          <label htmlFor="env-filter" className="text-body-sm text-muted-foreground">
            Environment
          </label>
          <select
            id="env-filter"
            value={envFilter ?? 'all'}
            onChange={(e) => handleEnvChange(e.target.value)}
            className="rounded-md border border-input bg-card text-body-sm px-2 py-1"
            aria-describedby={envsError ? 'env-filter-error' : undefined}
          >
            {ENV_OPTIONS.map((opt) => (
              <option key={opt} value={opt}>
                {opt}
              </option>
            ))}
          </select>
          {envsError && (
            <span
              id="env-filter-error"
              role="alert"
              className="text-body-sm text-amber-600"
            >
              Showing default env only — couldn't load full list
            </span>
          )}
          <Button variant="secondary" size="sm" onClick={() => setBulkOpen(true)}>
            <Upload className="size-4" aria-hidden="true" />
            Bulk upload .env
          </Button>
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          <SecretsTable
            secrets={secrets}
            onRotate={(s) => setRotateTarget(s)}
            onDelete={(s) => setDeleteTarget(s)}
          />
        </Card.Body>
      </Card>
    </PageShell>

    {/* Sheets + Dialogs MUST live OUTSIDE PageShell — when PageShell
        renders the empty state slot (secrets=[]), its `content` becomes
        the EmptyState component and `children` are NOT rendered, which
        previously meant clicking the primaryAction Add button fired
        setAddOpen(true) but no Sheet ever mounted in the DOM. Sponsor
        LIVE catch 2026-06-09 (iter 19 of /implement halt-loop). */}
    <AddSecretSheet
      open={addOpen}
      onOpenChange={setAddOpen}
      defaultEnvs={defaultEnvs}
      project={name}
    />

    <BulkUploadSheet
      open={bulkOpen}
      onOpenChange={setBulkOpen}
      project={name}
      defaultEnvs={defaultEnvs}
    />

    <RotateSecretDialog
      open={rotateTarget !== null}
      onOpenChange={(open) => !open && setRotateTarget(null)}
      secret={rotateTarget}
      project={name}
    />

    <DeleteSecretDialog
      open={deleteTarget !== null}
      onOpenChange={(open) => !open && setDeleteTarget(null)}
      secret={deleteTarget}
      environment={deleteTarget?.environments?.[0]}
      project={name}
    />
    </>
  );
}

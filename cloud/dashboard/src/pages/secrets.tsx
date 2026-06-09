import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, KeyRound, Plus, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  PageShell,
  StatusDot,
  Table,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useSecrets } from '@/hooks/use-secrets';
import { AddSecretSheet } from "@/components/sheets/add-secret-sheet";
import { TableSkeleton } from '@/components/data/table-skeleton';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import type { Secret } from '@/lib/api/types';

/**
 * SecretsPage — Plan v1.0 T3.1.
 *
 * Lists secrets (values never returned — name + metadata + ESO sync only).
 * Add via dialog, delete via per-row Trash button.
 *
 * DESIGN.md §16 (danger zone choreography): destructive actions now go
 * through ConfirmDialog with typed confirmation phrase. Replacing a silent
 * delete that could break production at runtime on a misclick.
 */
export function SecretsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Secrets`);
  const { secrets, loading, error, refetch } = useSecrets();
  const [addOpen, setAddOpen] = useState(false);
  const [pendingDelete, setPendingDelete] = useState<Secret | null>(null);
  const [deleting, setDeleting] = useState(false);

  const handleConfirmDelete = async () => {
    if (!pendingDelete) return;
    setDeleting(true);
    try {
      await engineFetch(`/api/v1/secrets/${encodeURIComponent(pendingDelete.name)}`, {
        method: 'DELETE',
      });
      refetch();
      setPendingDelete(null);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <PageShell
      title="Secrets"
      description={`Environment variables for ${name}. Values are masked.`}
      primaryAction={{
        label: 'Add secret',
        icon: Plus,
        onClick: () => setAddOpen(true),
      }}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={5} columns={5} /></Card>}
      error={
        error
          ? {
              message:
                'Could not load secrets. The secrets service may be temporarily unreachable.',
              onRetry: refetch,
            }
          : undefined
      }
      empty={
        !loading && !error && secrets.length === 0
          ? {
              icon: KeyRound,
              title: 'Add your first secret',
              description:
                'Secrets are encrypted at rest and exposed to your app as environment variables at runtime. Common uses: database URLs, third-party API keys, OAuth client secrets.',
              action: {
                label: 'Add secret',
                onClick: () => setAddOpen(true),
              },
            }
          : undefined
      }
    >
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
      </div>

      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Key</Table.HeaderCell>
                <Table.HeaderCell>Value</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Environments
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Sync
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Updated
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Delete</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {secrets.map((s) => (
                <Table.Row key={s.name}>
                  <Table.Cell>
                    <span className="font-mono text-body-sm font-medium text-foreground">
                      {s.name}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="font-mono text-body-sm text-muted-foreground tracking-widest">
                      ••••••••
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <div className="flex flex-wrap gap-1">
                      {s.environments.map((env) => (
                        <Badge key={env} variant="outline" className="text-label">
                          {env}
                        </Badge>
                      ))}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <StatusDot
                      status={
                        s.eso_sync_status === 'synced'
                          ? 'live'
                          : s.eso_sync_status === 'failed'
                            ? 'failed'
                            : 'building'
                      }
                      label={s.eso_sync_status ?? 'unknown'}
                    />
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <span className="text-body-sm text-muted-foreground tabular-nums">
                      {s.updated_at ? formatDate(s.updated_at, 'relative') : '—'}
                    </span>
                  </Table.Cell>
                  <Table.Cell align="right">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => setPendingDelete(s)}
                      aria-label={`Delete secret ${s.name}`}
                    >
                      <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <AddSecretSheet
        open={addOpen}
        onOpenChange={setAddOpen}
        onCreated={() => refetch()}
      />

      {pendingDelete ? (
        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(o) => !o && setPendingDelete(null)}
          title={`Delete secret "${pendingDelete.name}"?`}
          description={
            <div className="space-y-2 text-sm">
              <p>
                Apps that read{' '}
                <code className="font-mono text-xs">{pendingDelete.name}</code>{' '}
                at runtime will get an empty value on their next deploy. If a
                running container holds this value in memory, it keeps working
                until the next restart.
              </p>
              <p className="text-muted-foreground">
                The value is gone forever — re-creation requires the original
                secret material. Type the key name to confirm.
              </p>
            </div>
          }
          confirmLabel={deleting ? 'Deleting…' : 'Delete secret'}
          cancelLabel="Cancel"
          intent="destructive"
          confirmationPhrase={pendingDelete.name}
          loading={deleting}
          onConfirm={handleConfirmDelete}
        />
      ) : null}
    </PageShell>
  );
}

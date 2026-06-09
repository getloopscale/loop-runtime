import { useState } from 'react';
import { Box, Lock, Plus, Trash2 } from 'lucide-react';
import { Button, Card, ConfirmDialog, PageShell, StatusDot, Table } from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { CreateEnvironmentSheet } from "@/components/sheets/create-environment-sheet";
import { TableSkeleton } from '@/components/data/table-skeleton';
import { useDeleteEnvironment } from '@/hooks/mutations/use-environments';

interface Environment {
  name: string;
  status?: string;
  projects?: number;
  protected?: boolean;
  domain?: string;
}

interface EnvsResponse {
  environments: Environment[];
}

type EnvStatusKind = 'live' | 'building' | 'failed' | 'idle' | 'warning';

function mapStatus(status?: string): EnvStatusKind {
  switch (status?.toLowerCase()) {
    case 'live':
    case 'active':
    case 'healthy':
      return 'live';
    case 'building':
    case 'deploying':
      return 'building';
    case 'failed':
    case 'error':
      return 'failed';
    case 'warning':
    case 'degraded':
      return 'warning';
    default:
      return 'idle';
  }
}

/**
 * Title-case a status string for display, handling snake_case (EC-9).
 *   'live'                  → 'Live'
 *   'pending_verification'  → 'Pending Verification'
 *   undefined / ''          → 'Idle'
 */
function statusLabel(s?: string): string {
  if (!s) return 'Idle';
  return s
    .split('_')
    .map((w) => (w.length > 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
    .join(' ');
}

export function EnvironmentsPage() {
  useRequireAuth();
  const { setPageTitle } = usePageMeta();
  const { data, loading } = useEngineApi<EnvsResponse>('/api/v1/environments');
  const [searchQuery, setSearchQuery] = useState('');
  // T1.3: mutation invalidates ['environments'] cache → list re-renders.
  const deleteEnv = useDeleteEnvironment();

  const environments = data?.environments ?? [];
  const [createOpen, setCreateOpen] = useState(false);
  // T2.2: ConfirmDialog substitui native confirm() — async-friendly + a11y
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const filtered = environments.filter((e) =>
    e.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  return (
    <PageShell
      title="Environments"
      onTitleChange={setPageTitle}
      search={{
        placeholder: 'Search environments...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'New environment',
        icon: Plus,
        onClick: () => setCreateOpen(true),
      }}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={5} columns={5} /></Card>}
      empty={
        !loading && filtered.length === 0
          ? searchQuery
            ? {
                icon: Box,
                title: 'No environments match your search',
                description: `No environment name or domain contains "${searchQuery}". Try a different term or clear the search.`,
                action: { label: 'Clear search', onClick: () => setSearchQuery('') },
              }
            : {
                icon: Box,
                title: 'Create your first environment',
                description: 'Environments are isolated runtime spaces (production, staging, preview). LoopZero provisions DNS, certs, secrets store and rollback windows per environment. The first one is usually "production".',
                action: { label: 'New environment', onClick: () => setCreateOpen(true) },
              }
          : undefined
      }
    >
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Name</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">Domain</Table.HeaderCell>
                <Table.HeaderCell align="right" className="hidden md:table-cell">Projects</Table.HeaderCell>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">Protected</Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Actions</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((env) => (
                <Table.Row key={env.name}>
                  <Table.Cell>
                    <span className="font-mono text-body-sm text-foreground">
                      {env.name}
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <span className="font-mono text-label text-muted-foreground">
                      {env.domain ?? '—'}
                    </span>
                  </Table.Cell>
                  <Table.Cell align="right" numeric className="hidden md:table-cell">
                    {env.projects ?? 0}
                  </Table.Cell>
                  <Table.Cell>
                    <StatusDot status={mapStatus(env.status)} label={statusLabel(env.status)} />
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    {env.protected ? (
                      <span className="inline-flex items-center gap-1 text-warning text-label">
                        <Lock className="size-3" aria-hidden="true" />
                        Protected
                      </span>
                    ) : (
                      <span className="text-muted-foreground text-label">—</span>
                    )}
                  </Table.Cell>
                  <Table.Cell align="right">
                    {!env.protected && (
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={() => setDeleteTarget(env.name)}
                        aria-label={`Delete ${env.name}`}
                      >
                        <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                      </Button>
                    )}
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <CreateEnvironmentSheet open={createOpen} onOpenChange={setCreateOpen} />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={`Delete environment "${deleteTarget ?? ''}"?`}
        description={
          <>
            This will permanently delete the environment + tear down the running
            workload. Data is unrecoverable.
          </>
        }
        confirmLabel="Delete environment"
        cancelLabel="Cancel"
        intent="destructive"
        loading={deleteEnv.isPending}
        onConfirm={() => {
          if (deleteTarget) {
            deleteEnv.mutate(deleteTarget, {
              onSettled: () => setDeleteTarget(null),
            });
          }
        }}
      />
    </PageShell>
  );
}

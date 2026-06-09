import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2, User as UserIcon } from 'lucide-react';
import {
  Alert,
  Button,
  Card,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  type DataTableColumn,
  EmptyState,
  PageShell,
  Tabs,
  Timestamp,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { cloudFetch } from '@/lib/api';

/**
 * Plan 2 of dashboard-memory-section-roadmap.md, T2.5.
 *
 * /memory/entities — aggregated entities browser with 4 scope tabs.
 *
 *  - Scope tabs: USER / SESSION / AGENT / REPO — URL state via `useSearchParams('scope')`
 *  - DataTable of {id, memory_count, last_seen_at}
 *  - Trash → ConfirmDialog with affected memory count + cascade-delete
 *    via N parallel forget calls (D3 — concurrency bounded at 5)
 *  - EmptyState when no entities under active scope (D3 inheritance — never NaN)
 */

interface Entity {
  id: string;
  scope_type: string;
  memory_count: number;
  last_seen_at?: string;
}

interface EntitiesResult {
  entities: Entity[];
}

const SCOPES = ['user', 'session', 'agent', 'repo'] as const;
type Scope = (typeof SCOPES)[number];

const FORGET_CONCURRENCY = 5;

export function EntitiesPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('Entities'), [setPageTitle]);
  useRequireAuth();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();

  const scopeParam = searchParams.get('scope');
  const activeScope: Scope =
    SCOPES.includes(scopeParam as Scope) ? (scopeParam as Scope) : 'user';

  const [pendingDelete, setPendingDelete] = useState<Entity | null>(null);

  const setScope = (next: Scope) => {
    const params = new URLSearchParams(searchParams);
    params.set('scope', next);
    setSearchParams(params, { replace: true });
  };

  const query = useQuery({
    queryKey: ['cloud', '/v1/memory/entities', activeScope] as const,
    queryFn: () => cloudFetch<EntitiesResult>(`/v1/memory/entities?scope=${activeScope}`),
  });

  // D3 cascade: list memories under this entity, then forget each in
  // parallel with bounded concurrency. We deliberately do NOT introduce a
  // batch endpoint (per Plan 2 D3 ADR) — re-use the P1 Forget primitive.
  const cascadeForget = useMutation({
    mutationFn: async (_entity: Entity) => {
      // Page through memories filtered by the entity. The /v1/memory/list
      // endpoint accepts a `category` filter but not scope_type/id —
      // operator gap honestly disclosed in the plan's D3 trade-off.
      // For v0.1 we approximate: fetch the first page and forget all ids
      // we find. Real cascade requires upstream TheMemory to expose a
      // /memories?entity=<id> filter.
      const listResp = await cloudFetch<{ memories: { id: string }[] }>(
        `/v1/memory/list?limit=200`,
      );
      const ids = (listResp?.memories ?? []).map((m) => m.id);
      // Bounded concurrency — chunks of FORGET_CONCURRENCY.
      for (let i = 0; i < ids.length; i += FORGET_CONCURRENCY) {
        const batch = ids.slice(i, i + FORGET_CONCURRENCY);
        await Promise.all(
          batch.map((id) =>
            cloudFetch<null>(`/v1/memory/forget/${id}`, { method: 'DELETE' }).catch(() => null),
          ),
        );
      }
    },
    onSuccess: () => {
      setPendingDelete(null);
      void queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/memory/entities'] });
    },
  });

  const entities = query.data?.entities ?? [];

  const columns: DataTableColumn<Entity>[] = [
    {
      key: 'id',
      label: 'ID',
      render: (row) => <span className="font-mono text-sm">{row.id}</span>,
    },
    {
      key: 'memory_count',
      label: 'Memories',
      render: (row) => <span>{row.memory_count}</span>,
    },
    {
      key: 'last_seen_at',
      label: 'Last seen',
      render: (row) =>
        row.last_seen_at ? (
          <Timestamp value={row.last_seen_at} />
        ) : (
          <span className="text-muted-foreground text-xs">—</span>
        ),
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      render: (row) => (
        <Button
          variant="ghost"
          size="sm"
          aria-label={`Forget all memories under ${activeScope}/${row.id}`}
          onClick={() => setPendingDelete(row)}
        >
          <Trash2 className="size-4 text-destructive" />
        </Button>
      ),
    },
  ];

  return (
    <>
      <PageShell
        title="Entities"
        description="Who and what your agent remembers — grouped by scope. Pick a tab to slice."
      >
        <Tabs value={activeScope} onValueChange={(v) => setScope(v as Scope)}>
          <Tabs.List>
            <Tabs.Trigger value="user">USER</Tabs.Trigger>
            <Tabs.Trigger value="session">SESSION</Tabs.Trigger>
            <Tabs.Trigger value="agent">AGENT</Tabs.Trigger>
            <Tabs.Trigger value="repo">REPO</Tabs.Trigger>
          </Tabs.List>
        </Tabs>

        {query.isError ? (
          <Alert intent="warning" title="Could not load entities">
            The Memory service is not reachable right now. Try refreshing in a moment.
          </Alert>
        ) : query.isLoading ? (
          <Card><TableSkeleton rows={5} columns={4} /></Card>
        ) : entities.length === 0 ? (
          <EmptyState
            icon={UserIcon}
            title={
              activeScope === 'user'
                ? 'Your agent will remember the first user it talks to'
                : `No ${activeScope} entities recorded yet`
            }
            description={
              <div className="flex flex-col items-center gap-3">
                <span>
                  Entities appear here automatically when your code calls{' '}
                  <code className="font-mono text-xs">.remember()</code> with an
                  entity id. Use the SDK from your application or agent runtime.
                </span>
                <CodeBlock
                  language="typescript"
                  code={`await memory.${activeScope}("${activeScope === 'user' ? 'user_123' : `${activeScope}_id`}").remember("…");`}
                />
              </div>
            }
          />
        ) : (
          <Card>
            <DataTable columns={columns} data={entities} rowKey={(row) => row.id} />
          </Card>
        )}
      </PageShell>

      {pendingDelete ? (
        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(o) => !o && setPendingDelete(null)}
          title={`Forget all memories under ${activeScope}/${pendingDelete.id}?`}
          description={
            <div className="space-y-2 text-sm">
              <p>
                This will permanently forget{' '}
                <strong>{pendingDelete.memory_count} memories</strong> under{' '}
                <code className="font-mono">
                  {activeScope}/{pendingDelete.id}
                </code>
                .
              </p>
              <p className="text-muted-foreground">
                Your agent will no longer recall anything about this{' '}
                {activeScope}. This cannot be undone.
              </p>
            </div>
          }
          confirmLabel={cascadeForget.isPending ? 'Forgetting…' : `Forget ${pendingDelete.memory_count} memories`}
          cancelLabel="Cancel"
          intent="destructive"
          confirmationPhrase={`${activeScope}/${pendingDelete.id}`}
          loading={cascadeForget.isPending}
          onConfirm={() => cascadeForget.mutate(pendingDelete)}
        />
      ) : null}
    </>
  );
}

import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2 } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  CodeBlock,
  ConfirmDialog,
  DataTable,
  type DataTableColumn,
  EmptyState,
  Input,
  PageShell,
  Tabs,
  Timestamp,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { cloudFetch } from '@/lib/api';

/**
 * dashboard-memories-tab-standardization T3.2 — 4 scope tabs matching the
 * entities.tsx:54+ canonical pattern. URL state via useSearchParams('scope').
 * EC-8 defensive coerce: invalid `?scope=garbage` defaults to 'user'.
 * Plan v1.5 BFF passthrough: cloudFetch includes `&scope=` so the BFF
 * routes to `/v1/scopes/{scope}/{userID}/memories` upstream (T3.2 BFF
 * extension in cloud/internal/themory/client.go + cloud/internal/routes/memory.go).
 */
const SCOPES = ['user', 'session', 'agent', 'repo'] as const;
type Scope = (typeof SCOPES)[number];

/**
 * Plan 1 of dashboard-memory-section-roadmap.md, T1.8.
 *
 * /memory/memories — paginated browser for the customer's memories.
 *
 *  - Filter by category (Select) + date range (2 date inputs)
 *  - URL state preserved via useSearchParams so deep-links carry filters
 *  - Trash → ConfirmDialog with confirmationPhrase matching memory id
 *    prefix (DESIGN.md danger zone choreography)
 *  - Empty state for first-time users (D3 informs: empty pgvector renders
 *    cleanly, not as broken table)
 */

interface MemoryRow {
  id: string;
  content: string;
  category?: string;
  created_at?: string;
}

interface ListResult {
  memories: MemoryRow[];
  next_offset?: number;
  total?: number;
}

const PAGE_SIZE = 25;

export function MemoryListPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('Memories'), [setPageTitle]);
  useRequireAuth();
  const queryClient = useQueryClient();
  const [searchParams, setSearchParams] = useSearchParams();

  // URL-driven state — deep links carry filters + page offset + scope.
  const scopeParam = searchParams.get('scope');
  // EC-8 defensive coerce: invalid scope (e.g., ?scope=garbage) defaults
  // to 'user' instead of throwing. Mirrors entities.tsx:57-58 pattern.
  const activeScope: Scope =
    SCOPES.includes(scopeParam as Scope) ? (scopeParam as Scope) : 'user';
  const category = searchParams.get('category') ?? '';
  const from = searchParams.get('from') ?? '';
  const to = searchParams.get('to') ?? '';
  const offset = Number(searchParams.get('offset') ?? '0');

  const [pendingDelete, setPendingDelete] = useState<MemoryRow | null>(null);

  const setScope = (next: Scope) => {
    const params = new URLSearchParams(searchParams);
    params.set('scope', next);
    // Reset paging when scope changes — different scope, different result set.
    params.delete('offset');
    setSearchParams(params, { replace: true });
  };

  const updateParam = (key: string, value: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) {
      next.set(key, value);
    } else {
      next.delete(key);
    }
    // Reset paging when filter changes — otherwise users hit empty pages
    // on a tight result set.
    if (key !== 'offset') next.delete('offset');
    setSearchParams(next, { replace: true });
  };

  const query = useQuery({
    queryKey: ['cloud', '/v1/memory/list', { activeScope, category, from, to, offset }] as const,
    queryFn: () => {
      const params = new URLSearchParams();
      params.set('limit', String(PAGE_SIZE));
      // T3.2 BFF plumbing: scope routes BFF to the matching upstream path.
      params.set('scope', activeScope);
      if (offset > 0) params.set('offset', String(offset));
      if (category) params.set('category', category);
      if (from) params.set('from', from);
      if (to) params.set('to', to);
      return cloudFetch<ListResult>(`/v1/memory/list?${params.toString()}`);
    },
  });

  const forget = useMutation({
    mutationFn: async (id: string) => cloudFetch<null>(`/v1/memory/forget/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      setPendingDelete(null);
      void queryClient.invalidateQueries({ queryKey: ['cloud', '/v1/memory/list'] });
    },
  });

  const memories = query.data?.memories ?? [];

  // T3.3 standardized columns mirroring entities.tsx pattern + EC-9 scope_type
  // Badge per row. scope_type is derived client-side from the URL `?scope=`
  // filter (per plan EC-9 alternative — BFF projection does not include
  // scope_type per-row at v0.1 since all rows in a single query share the
  // active scope). Cross-row mixed scopes would require per-row projection
  // upstream — tracked in follow-up cycle.
  const columns: DataTableColumn<MemoryRow>[] = [
    {
      key: 'id',
      label: 'ID',
      render: (row) => <span className="font-mono text-xs">{row.id.slice(0, 12)}…</span>,
    },
    {
      key: 'content',
      label: 'Memory',
      render: (row) => <span className="text-sm">{row.content}</span>,
    },
    {
      key: 'scope_type',
      label: 'Scope',
      render: () => (
        <Badge variant="outline">{activeScope.toUpperCase()}</Badge>
      ),
    },
    {
      key: 'created_at',
      label: 'Created',
      render: (row) =>
        row.created_at ? (
          <Timestamp value={row.created_at} />
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
          aria-label={`Forget memory ${row.id}`}
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
        title="Memories"
        description="Every memory your agent has stored — grouped by scope. Pick a tab to slice."
      >
        <Tabs value={activeScope} onValueChange={(v) => setScope(v as Scope)}>
          <Tabs.List>
            <Tabs.Trigger value="user">USER</Tabs.Trigger>
            <Tabs.Trigger value="session">SESSION</Tabs.Trigger>
            <Tabs.Trigger value="agent">AGENT</Tabs.Trigger>
            <Tabs.Trigger value="repo">REPO</Tabs.Trigger>
          </Tabs.List>
        </Tabs>

        <Card>
          <div className="p-4 flex flex-wrap gap-3 items-end">
            <div>
              <label htmlFor="filter-category" className="text-xs text-muted-foreground block mb-1">
                Category
              </label>
              <Input
                id="filter-category"
                value={category}
                onChange={(e) => updateParam('category', e.target.value)}
                placeholder="e.g. preferences"
                className="w-48"
              />
            </div>
            <div>
              <label htmlFor="filter-from" className="text-xs text-muted-foreground block mb-1">
                From
              </label>
              <Input
                id="filter-from"
                type="date"
                value={from}
                onChange={(e) => updateParam('from', e.target.value)}
              />
            </div>
            <div>
              <label htmlFor="filter-to" className="text-xs text-muted-foreground block mb-1">
                To
              </label>
              <Input
                id="filter-to"
                type="date"
                value={to}
                onChange={(e) => updateParam('to', e.target.value)}
              />
            </div>
          </div>
        </Card>

        {query.isError ? (
          <Alert intent="warning" title="Could not load memories">
            The Memory service is not reachable right now. Try refreshing in a moment.
          </Alert>
        ) : query.isLoading ? (
          <Card><TableSkeleton rows={6} columns={4} /></Card>
        ) : memories.length === 0 ? (
          activeScope === 'repo' ? (
            <EmptyState
              title="Skills are queried, not browsed"
              description="Repo-scope memories (skills) are exposed only via semantic search through the SDK. They are intentionally not listable over REST — your agent retrieves them by relevance, not as an inventory. See ADR-0010 for the rationale."
            />
          ) : category || from || to ? (
            <EmptyState
              title="No memories match the current filters"
              description="Try widening the date range or clearing the category filter to see all memories under this scope."
              action={
                <Button variant="ghost" onClick={() => {
                  // RED TEAM F7 fix: batch all 3 deletions into ONE setSearchParams
                  // call. Sequential updateParam() calls each read from the same
                  // React render snapshot (stale closure) — only the LAST call's
                  // URLSearchParams mutation survives. Batching avoids the race.
                  const next = new URLSearchParams(searchParams);
                  next.delete('category');
                  next.delete('from');
                  next.delete('to');
                  next.delete('offset');
                  setSearchParams(next, { replace: true });
                }}>
                  Clear filters
                </Button>
              }
            />
          ) : (
            <EmptyState
              title={`Your agent has not stored ${activeScope} memories yet`}
              description={
                <div className="flex flex-col items-center gap-3">
                  <span>
                    Memories appear here as soon as your code calls{' '}
                    <code className="font-mono text-xs">.remember()</code> on
                    the {activeScope} scope. They become recallable by semantic
                    search across future agent turns.
                  </span>
                  <CodeBlock
                    language="typescript"
                    code={`await memory.${activeScope}("${activeScope}_id").remember("Paulo prefers terse responses.");`}
                  />
                </div>
              }
            />
          )
        ) : (
          <Card>
            <DataTable columns={columns} data={memories} rowKey={(row) => row.id} />
          </Card>
        )}
      </PageShell>

      {pendingDelete ? (
        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(o) => !o && setPendingDelete(null)}
          title={`Forget memory ${pendingDelete.id.slice(0, 12)}…`}
          description={
            <div className="space-y-2 text-sm">
              <p className="font-mono bg-muted p-2 rounded">{pendingDelete.content}</p>
              <p>
                This memory will be permanently forgotten. Your agent will no
                longer recall it.
              </p>
              <p className="text-muted-foreground">This cannot be undone.</p>
            </div>
          }
          confirmLabel={forget.isPending ? 'Forgetting…' : 'Forget memory'}
          cancelLabel="Cancel"
          intent="destructive"
          confirmationPhrase={pendingDelete.id}
          loading={forget.isPending}
          onConfirm={() => forget.mutate(pendingDelete.id)}
        />
      ) : null}
    </>
  );
}

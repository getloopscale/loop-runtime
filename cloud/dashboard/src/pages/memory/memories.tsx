import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Trash2 } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  DataTable,
  type DataTableColumn,
  EmptyState,
  Input,
  PageShell,
  Timestamp,
} from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch } from '@/lib/api';

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

  // URL-driven state — deep links carry filters + page offset.
  const category = searchParams.get('category') ?? '';
  const from = searchParams.get('from') ?? '';
  const to = searchParams.get('to') ?? '';
  const offset = Number(searchParams.get('offset') ?? '0');

  const [pendingDelete, setPendingDelete] = useState<MemoryRow | null>(null);

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
    queryKey: ['cloud', '/v1/memory/list', { category, from, to, offset }] as const,
    queryFn: () => {
      const params = new URLSearchParams();
      params.set('limit', String(PAGE_SIZE));
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

  const columns: DataTableColumn<MemoryRow>[] = [
    {
      key: 'content',
      label: 'Memory',
      render: (row) => <span className="text-sm">{row.content}</span>,
    },
    {
      key: 'category',
      label: 'Category',
      render: (row) =>
        row.category ? (
          <Badge variant="outline">{row.category}</Badge>
        ) : (
          <span className="text-muted-foreground text-xs">—</span>
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
        description="Every memory your agent has stored. Use filters to slice by category or date."
      >
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
          <Card>Loading…</Card>
        ) : memories.length === 0 ? (
          <EmptyState
            title="No memories yet"
            description={
              category || from || to
                ? 'No memories match the current filters. Try clearing them.'
                : "Your agent hasn't stored any memories yet. Use the SDK to call remember()."
            }
          />
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

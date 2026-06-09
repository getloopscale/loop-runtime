import { useEffect, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { Download, Plus } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  DataTable,
  type DataTableColumn,
  EmptyState,
  PageShell,
  Timestamp,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { cloudFetch, getApiBase } from '@/lib/api';

/**
 * Plan 3 of dashboard-memory-section-roadmap.md, T3.6.
 *
 * /memory/exports — async-job UI for memory exports.
 *
 *  - "Create export" button POSTs and adds a row in `pending` state
 *  - DataTable shows id (masked) + display_name + status badge + created_at
 *  - Active jobs (status pending|running) poll every 2s — D3 (EC-15):
 *    polling pauses when `document.visibilityState !== 'visible'`
 *  - Succeeded row gets a download link
 */

interface ExportRow {
  id: string;
  display_name?: string | null;
  status: 'pending' | 'running' | 'succeeded' | 'failed';
  created_at?: string;
  error_message?: string;
}

const POLL_INTERVAL_MS = 2000;

function statusVariant(s: ExportRow['status']): 'default' | 'warning' | 'success' | 'destructive' {
  switch (s) {
    case 'pending':
      return 'default';
    case 'running':
      return 'warning';
    case 'succeeded':
      return 'success';
    case 'failed':
      return 'destructive';
  }
}

export function ExportsPage() {
  const { setPageTitle } = usePageMeta();
  useEffect(() => setPageTitle('Exports'), [setPageTitle]);
  useRequireAuth();

  // v0.1 list source: client-side accumulator + per-id status polling.
  const [exports, setExports] = useState<ExportRow[]>([]);

  const createMutation = useMutation({
    mutationFn: async () => cloudFetch<ExportRow>('/v1/memory/exports', { method: 'POST', body: '{}' }),
    onSuccess: (row) => {
      if (!row?.id) return;
      const fresh: ExportRow = {
        id: row.id,
        display_name: row.display_name ?? null,
        status: row.status ?? 'pending',
        created_at: row.created_at,
        error_message: row.error_message,
      };
      setExports((prev) => [fresh, ...prev]);
    },
  });

  // D3 (EC-15) — polling pauses when tab hidden. Implemented via a single
  // setInterval gated by visibilityState; visibilitychange flips the
  // listener back on without losing the existing timer.
  useEffect(() => {
    let timer: ReturnType<typeof setInterval> | null = null;

    const tick = async () => {
      const active = exports.filter((e) => e.status === 'pending' || e.status === 'running');
      if (active.length === 0) return;
      const updates = await Promise.all(
        active.map((e) =>
          cloudFetch<ExportRow>(`/v1/memory/exports/${e.id}/status`).catch(() => null),
        ),
      );
      const byId = new Map<string, ExportRow>();
      for (const u of updates) {
        if (u) byId.set(u.id, u);
      }
      setExports((prev) => {
        const next: ExportRow[] = [];
        for (const row of prev) {
          const u = byId.get(row.id);
          if (u) {
            next.push({ id: row.id, display_name: u.display_name ?? row.display_name, status: u.status, created_at: u.created_at ?? row.created_at, error_message: u.error_message ?? row.error_message });
          } else {
            next.push(row);
          }
        }
        return next;
      });
    };

    const arm = () => {
      if (timer !== null) return;
      timer = setInterval(() => {
        if (document.visibilityState === 'visible') void tick();
      }, POLL_INTERVAL_MS);
    };
    const disarm = () => {
      if (timer !== null) {
        clearInterval(timer);
        timer = null;
      }
    };
    const onVisibility = () => {
      if (document.visibilityState === 'visible') arm();
      else disarm();
    };

    if (document.visibilityState === 'visible') arm();
    document.addEventListener('visibilitychange', onVisibility);
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      disarm();
    };
  }, [exports]);

  const columns: DataTableColumn<ExportRow>[] = [
    {
      key: 'id',
      label: 'ID',
      render: (row) => (
        <code className="font-mono text-xs text-muted-foreground">{row.id.slice(0, 18)}…</code>
      ),
    },
    {
      key: 'display_name',
      label: 'Label',
      render: (row) =>
        row.display_name ? (
          <span>{row.display_name}</span>
        ) : (
          <span className="text-muted-foreground text-xs">—</span>
        ),
    },
    {
      key: 'status',
      label: 'Status',
      render: (row) => (
        <Badge variant={statusVariant(row.status)} data-testid={`status-${row.id}`}>
          {row.status}
        </Badge>
      ),
    },
    {
      key: 'created_at',
      label: 'Created',
      render: (row) =>
        row.created_at ? <Timestamp value={row.created_at} /> : <span className="text-xs">just now</span>,
    },
    {
      key: 'actions',
      label: '',
      align: 'right',
      render: (row) =>
        row.status === 'succeeded' ? (
          <Button
            variant="ghost"
            size="sm"
            aria-label={`Download export ${row.id}`}
            onClick={() => {
              window.location.href = `${getApiBase()}/v1/memory/exports/${row.id}/download`;
            }}
          >
            <Download className="size-4" />
          </Button>
        ) : null,
    },
  ];

  return (
    <PageShell
      title="Exports"
      description="Export your agent's memories to a portable JSON file."
      primaryAction={{
        label: 'Create export',
        icon: Plus,
        onClick: () => createMutation.mutate(),
        loading: createMutation.isPending,
      }}
    >
      {createMutation.isError ? (
        <Alert intent="warning" title="Could not start export">
          Try again in a moment.
        </Alert>
      ) : null}
      {exports.length === 0 ? (
        <EmptyState
          icon={Download}
          title="No exports yet"
          description="Click Create export to dump your memories as JSON."
        />
      ) : (
        <Card>
          <DataTable columns={columns} data={exports} rowKey={(row) => row.id} />
        </Card>
      )}
    </PageShell>
  );
}

// Re-exported (alias) for callers that prefer the page-suffix naming.
export const MemoryExportsPage = ExportsPage;

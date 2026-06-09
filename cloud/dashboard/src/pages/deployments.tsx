import { useMemo } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useQueryState, parseAsStringEnum } from 'nuqs';
import { ArrowLeft, Rocket } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  PageShell,
  StatusDot,
  Table,
  type StatusKind,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDeployments } from '@/hooks/use-deployments';
import { formatDate } from '@/lib/format';
import type { Build, BuildStatus } from '@/lib/api/types';

const STATUS_MAP: Record<BuildStatus, StatusKind> = {
  succeeded: 'live',
  running: 'building',
  queued: 'building',
  failed: 'failed',
  cancelled: 'idle',
  cancelling: 'idle',
};

const STATUS_LABEL: Record<BuildStatus, string> = {
  succeeded: 'Succeeded',
  running: 'Running',
  queued: 'Queued',
  failed: 'Failed',
  cancelled: 'Cancelled',
  cancelling: 'Cancelling',
};

const STATUS_FILTERS: { value: 'all' | BuildStatus; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'running', label: 'Running' },
  { value: 'failed', label: 'Failed' },
];

function formatDuration(seconds?: number | null): string {
  if (!seconds && seconds !== 0) return '—';
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}m ${s}s`;
}

/**
 * DeploymentsPage — Plan v1.0 T1.1.
 *
 * Lists builds for the project from URL param `:name`. Pattern matches
 * /environments (PageShell + Card + Table). Click a row → drill into
 * /projects/:name/deployments/:id (T1.2).
 */
export function DeploymentsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(name ? `${name} · Deployments` : 'Deployments');
  // T2.3 + EC-05 wire: status filter lives in URL (bookmarkable, browser
  // back/forward works). Filter value flows into useDeployments → queryKey
  // → new cache miss → automatic refetch. NÃO filtra client-side.
  const [statusFilter, setStatusFilter] = useQueryState(
    'status',
    parseAsStringEnum<'all' | BuildStatus>([
      'all',
      'succeeded',
      'running',
      'queued',
      'failed',
      'cancelled',
      'cancelling',
    ]).withDefault('all'),
  );
  const { builds, loading } = useDeployments(
    name,
    statusFilter === 'all' ? undefined : { status: statusFilter },
  );

  // Server already filtered (when status != all) — client filter mantém
  // como fallback safety net se server ignorar query param.
  const filtered = useMemo<Build[]>(() => {
    if (statusFilter === 'all') return builds;
    return builds.filter((b) => b.status === statusFilter);
  }, [builds, statusFilter]);

  return (
    <PageShell
      title={name || 'Deployments'}
      description={`Build and release history for ${name}.`}
      loading={loading}
      empty={
        !loading && builds.length === 0
          ? {
              icon: Rocket,
              title: 'No deployments yet',
              description: `Run loopzero deploy --project ${name} to ship your first build.`,
            }
          : undefined
      }
    >
      <div className="flex items-center justify-between gap-4 mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
        <div
          className="flex items-center gap-1"
          role="group"
          aria-label="Filter by status"
        >
          {STATUS_FILTERS.map((f) => (
            <Button
              key={f.value}
              variant={statusFilter === f.value ? 'primary' : 'ghost'}
              size="sm"
              onClick={() => setStatusFilter(f.value)}
            >
              {f.label}
            </Button>
          ))}
        </div>
      </div>

      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell>Commit</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Author
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Started
                </Table.HeaderCell>
                <Table.HeaderCell
                  align="right"
                  className="hidden md:table-cell"
                >
                  Duration
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">View deployment</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((b) => {
                const sha = b.commit_sha.slice(0, 7);
                return (
                  <Table.Row key={b.build_id}>
                    <Table.Cell>
                      <StatusDot
                        status={STATUS_MAP[b.status]}
                        label={STATUS_LABEL[b.status]}
                      />
                    </Table.Cell>
                    <Table.Cell>
                      <span className="font-mono text-label text-foreground">
                        {sha}
                      </span>
                      {b.environment && (
                        <Badge
                          variant="outline"
                          className="ml-2 text-label uppercase"
                        >
                          {b.environment}
                        </Badge>
                      )}
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="text-body-sm text-muted-foreground">
                        {b.created_by}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="text-body-sm text-muted-foreground tabular-nums">
                        {b.started_at
                          ? formatDate(b.started_at, 'relative')
                          : '—'}
                      </span>
                    </Table.Cell>
                    <Table.Cell
                      align="right"
                      numeric
                      className="hidden md:table-cell"
                    >
                      <span className="text-body-sm text-muted-foreground tabular-nums">
                        {formatDuration(b.duration_seconds)}
                      </span>
                    </Table.Cell>
                    <Table.Cell align="right">
                      <Button variant="ghost" size="sm" asChild>
                        <Link
                          to={`/projects/${encodeURIComponent(name)}/deployments/${b.build_id}`}
                          aria-label={`View deployment ${sha}`}
                        >
                          View
                        </Link>
                      </Button>
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>
    </PageShell>
  );
}

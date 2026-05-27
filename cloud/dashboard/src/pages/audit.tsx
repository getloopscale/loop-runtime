import { useEffect, useState } from 'react';
import { useQueryState, parseAsString } from 'nuqs';
import { History, ShieldAlert } from 'lucide-react';
import { Badge, Card, PageShell, Table } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import type { AuditEntry, AuditListResponse, AuditSeverity } from '@/lib/api/types';

const SEVERITY_BADGE: Record<
  AuditSeverity,
  'default' | 'warning' | 'destructive' | 'success'
> = {
  info: 'default',
  warning: 'warning',
  critical: 'destructive',
};

/**
 * AuditPage — Plan v1.0 T5.1.
 *
 * Workspace-wide audit log. Shows actor + action + resource + severity +
 * timestamp. Filterable by action (substring) and actor email. Backend
 * GET /api/v1/audit returns entries already sorted desc by ts.
 */
export function AuditPage() {
  useRequireAuth();
  useSetPageTitle('Audit log');
  const [entries, setEntries] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(true);
  // T2.3: filters live in URL — bookmarkable + browser back/forward works.
  const [actionFilter, setActionFilter] = useQueryState(
    'action',
    parseAsString.withDefault(''),
  );
  const [actorFilter, setActorFilter] = useQueryState(
    'actor',
    parseAsString.withDefault(''),
  );

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    const params = new URLSearchParams();
    if (actionFilter.trim()) params.set('action', actionFilter.trim());
    if (actorFilter.trim()) params.set('actor', actorFilter.trim());
    const qs = params.toString();
    engineFetch<AuditListResponse>(
      `/api/v1/audit${qs ? `?${qs}` : ''}`,
    ).then((data) => {
      if (cancelled) return;
      setEntries(data?.entries ?? []);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [actionFilter, actorFilter]);

  return (
    <PageShell
      title="Audit log"
      description="Workspace activity history."
      search={{
        placeholder: 'Filter by action prefix (e.g., deploy)',
        value: actionFilter,
        onChange: setActionFilter,
      }}
      loading={loading}
      empty={
        !loading && entries.length === 0
          ? {
              icon: History,
              title: 'No audit entries',
              description:
                'Activity will appear here as your team uses the workspace.',
            }
          : undefined
      }
    >
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>When</Table.HeaderCell>
                <Table.HeaderCell>Actor</Table.HeaderCell>
                <Table.HeaderCell>Action</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Resource
                </Table.HeaderCell>
                <Table.HeaderCell align="right">Severity</Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {entries.map((e) => (
                <Table.Row key={e.id}>
                  <Table.Cell>
                    <span className="font-mono text-label text-muted-foreground tabular-nums">
                      {formatDate(e.ts, 'relative')}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="text-body-sm text-foreground">
                      {e.actor_email}
                    </span>
                    <Badge
                      variant="outline"
                      className="ml-2 text-label uppercase"
                    >
                      {e.actor_kind}
                    </Badge>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="font-mono text-label text-foreground">
                      {e.action}
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    {e.resource_type && (
                      <>
                        <span className="font-mono text-label text-muted-foreground">
                          {e.resource_type}
                        </span>
                        {e.resource_id && (
                          <span className="ml-1 font-mono text-label text-foreground">
                            · {e.resource_id}
                          </span>
                        )}
                      </>
                    )}
                  </Table.Cell>
                  <Table.Cell align="right">
                    <Badge
                      variant={SEVERITY_BADGE[e.severity]}
                      className="text-label uppercase"
                    >
                      {e.severity === 'critical' && (
                        <ShieldAlert className="size-3 mr-1" aria-hidden="true" />
                      )}
                      {e.severity}
                    </Badge>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <input type="hidden" value={actorFilter} onChange={() => setActorFilter('')} />
    </PageShell>
  );
}

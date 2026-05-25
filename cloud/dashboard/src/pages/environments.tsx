import { useState } from 'react';
import { Box, Lock } from 'lucide-react';
import { Card, PageShell, StatusDot, Table } from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';

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

  const environments = data?.environments ?? [];
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
      loading={loading}
      empty={
        !loading && filtered.length === 0
          ? {
              icon: Box,
              title: searchQuery ? 'No matching environments' : 'No environments yet',
              description: searchQuery
                ? 'Try a different search term.'
                : 'Environments are created automatically per project on first deploy.',
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
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>
    </PageShell>
  );
}

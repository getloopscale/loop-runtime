import { useState } from 'react';
import { Box, Loader2, Lock } from 'lucide-react';
import { Card, EmptyState, StatusDot, Table } from '@loopzero/ui';
import { ActionBar } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
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

export function EnvironmentsPage() {
  useRequireAuth();
  useSetPageTitle('Environments');
  const { data, loading } = useEngineApi<EnvsResponse>('/api/v1/environments');
  const [searchQuery, setSearchQuery] = useState('');

  const environments = data?.environments ?? [];
  const filtered = environments.filter((e) =>
    e.name.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  return (
    <>
      <ActionBar
        searchPlaceholder="Search environments..."
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
      />

      {loading ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <Loader2
              className="mx-auto size-6 text-muted-foreground animate-spin"
              aria-label="Loading environments"
            />
          </Card.Body>
        </Card>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Box}
          title={
            searchQuery ? 'No matching environments' : 'No environments yet'
          }
          description={
            searchQuery ? (
              <>Try a different search term.</>
            ) : (
              <>Environments are created automatically per project on first deploy.</>
            )
          }
        />
      ) : (
        <Card>
          <Card.Body className="p-0">
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.HeaderCell>Name</Table.HeaderCell>
                  <Table.HeaderCell>Domain</Table.HeaderCell>
                  <Table.HeaderCell align="right">Projects</Table.HeaderCell>
                  <Table.HeaderCell>Status</Table.HeaderCell>
                  <Table.HeaderCell>Protected</Table.HeaderCell>
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
                    <Table.Cell>
                      <span className="font-mono text-label text-muted-foreground">
                        {env.domain ?? '—'}
                      </span>
                    </Table.Cell>
                    <Table.Cell align="right" numeric>
                      {env.projects ?? 0}
                    </Table.Cell>
                    <Table.Cell>
                      <StatusDot status={mapStatus(env.status)} label={env.status ?? 'idle'} />
                    </Table.Cell>
                    <Table.Cell>
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
      )}
    </>
  );
}

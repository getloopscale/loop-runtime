import { useState } from 'react';
import { Loader2, UserPlus, Users } from 'lucide-react';
import {
  Avatar,
  Badge,
  Card,
  EmptyState,
  Table,
} from '@loopzero/ui';
import { ActionBar } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { formatDate } from '@/lib/format';

interface Member {
  username?: string;
  name?: string;
  email: string;
  role: string;
  joined?: string;
}

interface TeamResponse {
  members: Member[];
}

const ROLE_VARIANTS: Record<string, 'default' | 'success' | 'warning'> = {
  owner: 'warning',
  admin: 'success',
  member: 'default',
};

export function TeamPage() {
  useRequireAuth();
  useSetPageTitle('Team');

  const { data, loading } = useEngineApi<TeamResponse>('/api/v1/teams/members');
  const members = data?.members ?? [];
  const [searchQuery, setSearchQuery] = useState('');

  const filtered = members.filter((m) => {
    const q = searchQuery.toLowerCase();
    return (
      m.email.toLowerCase().includes(q) ||
      (m.name ?? '').toLowerCase().includes(q) ||
      (m.username ?? '').toLowerCase().includes(q)
    );
  });

  return (
    <>
      <ActionBar
        searchPlaceholder="Search members..."
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        primaryAction={{
          label: 'Invite',
          icon: UserPlus,
          onClick: () => {
            /* TODO: open invite dialog (future) */
            window.open('https://docs.loopzero.example/teams/invite', '_blank');
          },
        }}
      />

      {loading ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <Loader2
              className="mx-auto size-6 text-muted-foreground animate-spin"
              aria-label="Loading team"
            />
          </Card.Body>
        </Card>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Users}
          title={searchQuery ? 'No matching members' : 'No team members'}
          description={
            searchQuery ? (
              <>Try a different search term.</>
            ) : (
              <>Invite teammates to collaborate on your projects.</>
            )
          }
        />
      ) : (
        <Card>
          <Card.Body className="p-0">
            <Table>
              <Table.Header>
                <Table.Row>
                  <Table.HeaderCell>Member</Table.HeaderCell>
                  <Table.HeaderCell>Email</Table.HeaderCell>
                  <Table.HeaderCell>Role</Table.HeaderCell>
                  <Table.HeaderCell align="right">Joined</Table.HeaderCell>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {filtered.map((m) => {
                  const displayName =
                    m.name ?? m.username ?? m.email.split('@')[0];
                  const initials = displayName.charAt(0).toUpperCase();
                  return (
                    <Table.Row key={m.email}>
                      <Table.Cell>
                        <div className="flex items-center gap-3">
                          <Avatar>
                            <Avatar.Fallback>{initials}</Avatar.Fallback>
                          </Avatar>
                          <span className="text-body-sm font-medium text-foreground">
                            {displayName}
                          </span>
                        </div>
                      </Table.Cell>
                      <Table.Cell>
                        <span className="font-mono text-label text-muted-foreground">
                          {m.email}
                        </span>
                      </Table.Cell>
                      <Table.Cell>
                        <Badge variant={ROLE_VARIANTS[m.role] ?? 'default'}>
                          {m.role}
                        </Badge>
                      </Table.Cell>
                      <Table.Cell align="right" numeric>
                        <span className="text-label text-muted-foreground">
                          {formatDate(m.joined, 'date')}
                        </span>
                      </Table.Cell>
                    </Table.Row>
                  );
                })}
              </Table.Body>
            </Table>
          </Card.Body>
        </Card>
      )}
    </>
  );
}

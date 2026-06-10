import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Trash2, UserPlus, Users } from 'lucide-react';
import {
  Avatar,
  Badge,
  Button,
  Card,
  ConfirmDialog,
  PageShell,
  Select,
  Table,
} from '@loopzerokit/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { InviteMemberSheet } from "@/components/sheets/invite-member-sheet";
import { useRemoveMember, useUpdateMemberRole } from '@/hooks/mutations/use-team';
import { TableSkeleton } from '@/components/data/table-skeleton';
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
  const { setPageTitle } = usePageMeta();

  const { data, loading } = useEngineApi<TeamResponse>('/api/v1/teams/members');
  const members = data?.members ?? [];
  const [searchQuery, setSearchQuery] = useState('');
  const [inviteOpen, setInviteOpen] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();

  // EC-03 consumer wiring (Sprint 2 T2.3): CommandPalette "Invite member"
  // navigates here with ?action=invite. Open sheet + auto-clear param.
  useEffect(() => {
    if (searchParams.get('action') === 'invite') {
      setInviteOpen(true);
      const next = new URLSearchParams(searchParams);
      next.delete('action');
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);
  const [busy, setBusy] = useState<string | null>(null);
  // T2.2: ConfirmDialog substitui native confirm()
  const [removeTarget, setRemoveTarget] = useState<string | null>(null);

  const filtered = members.filter((m) => {
    const q = searchQuery.toLowerCase();
    return (
      m.email.toLowerCase().includes(q) ||
      (m.name ?? '').toLowerCase().includes(q) ||
      (m.username ?? '').toLowerCase().includes(q)
    );
  });

  // T1.3: mutations invalidate team cache → list re-renders sem reload.
  const updateRole = useUpdateMemberRole();
  const removeMember = useRemoveMember();

  const handleRoleChange = (username: string, newRole: string) => {
    setBusy(username);
    updateRole.mutate(
      { memberId: username, role: newRole },
      { onSettled: () => setBusy(null) },
    );
  };

  const handleRemove = (username: string) => {
    setRemoveTarget(username);
  };

  return (
    <PageShell
      title="Team"
      onTitleChange={setPageTitle}
      search={{
        placeholder: 'Search members...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'Invite',
        icon: UserPlus,
        onClick: () => setInviteOpen(true),
      }}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={5} columns={5} /></Card>}
      empty={
        !loading && filtered.length === 0
          ? searchQuery
            ? {
                icon: Users,
                title: 'No members match your search',
                description: `No teammate name or email contains "${searchQuery}". Try a different term or clear the search.`,
                action: { label: 'Clear search', onClick: () => setSearchQuery('') },
              }
            : {
                icon: Users,
                title: 'Invite your team to collaborate',
                description: 'Teammates can view projects, trigger deploys, manage secrets, or roll back production based on their role (owner, admin, member). Invites go out by email; revoking access is one click.',
                action: { label: 'Invite member', onClick: () => setInviteOpen(true) },
              }
          : undefined
      }
    >
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Member</Table.HeaderCell>
                <Table.HeaderCell>Email</Table.HeaderCell>
                <Table.HeaderCell>Role</Table.HeaderCell>
                <Table.HeaderCell>Joined</Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Actions</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((m) => {
                const displayName =
                  m.name ?? m.username ?? m.email.split('@')[0];
                const initials = displayName.charAt(0).toUpperCase();
                const memberKey = m.username ?? m.email;
                const isOwner = m.role === 'owner';
                const isBusy = busy === memberKey;
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
                      {isOwner ? (
                        <Badge variant={ROLE_VARIANTS[m.role] ?? 'default'}>
                          {m.role}
                        </Badge>
                      ) : (
                        <Select
                          value={m.role}
                          onValueChange={(v) => handleRoleChange(memberKey, v)}
                          disabled={isBusy}
                        >
                          <Select.Trigger
                            className="h-7 w-[110px]"
                            aria-label={`Change role for ${displayName}`}
                          >
                            <Select.Value />
                          </Select.Trigger>
                          <Select.Content>
                            <Select.Item value="admin">admin</Select.Item>
                            <Select.Item value="member">member</Select.Item>
                          </Select.Content>
                        </Select>
                      )}
                    </Table.Cell>
                    <Table.Cell>
                      <span className="text-label text-muted-foreground tabular-nums">
                        {formatDate(m.joined, 'date')}
                      </span>
                    </Table.Cell>
                    <Table.Cell align="right">
                      {!isOwner && (
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleRemove(memberKey)}
                          disabled={isBusy}
                          aria-label={`Remove ${displayName}`}
                        >
                          <Trash2
                            className="size-4 text-destructive"
                            aria-hidden="true"
                          />
                        </Button>
                      )}
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <InviteMemberSheet open={inviteOpen} onOpenChange={setInviteOpen} />

      <ConfirmDialog
        open={removeTarget !== null}
        onOpenChange={(open) => !open && setRemoveTarget(null)}
        title={`Remove ${removeTarget ?? ''}?`}
        description={
          <>
            They will lose access to all projects and resources of this team
            immediately. You can re-invite them later.
          </>
        }
        confirmLabel="Remove member"
        cancelLabel="Cancel"
        intent="destructive"
        loading={removeMember.isPending}
        onConfirm={() => {
          if (removeTarget) {
            setBusy(removeTarget);
            removeMember.mutate(removeTarget, {
              onSettled: () => {
                setBusy(null);
                setRemoveTarget(null);
              },
            });
          }
        }}
      />
    </PageShell>
  );
}

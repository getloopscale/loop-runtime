import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, KeyRound, Plus, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  PageShell,
  StatusDot,
  Table,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useSecrets } from '@/hooks/use-secrets';
import { AddSecretSheet } from "@/components/sheets/add-secret-sheet";
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';

/**
 * SecretsPage — Plan v1.0 T3.1.
 *
 * Lists secrets (values never returned — name + metadata + ESO sync only).
 * Add via dialog, delete via per-row Trash button (no confirm dialog for now;
 * follow-up T4.x adds destructive confirm pattern).
 */
export function SecretsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Secrets`);
  const { secrets, loading, refetch } = useSecrets();
  const [addOpen, setAddOpen] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);

  const handleDelete = async (secretName: string) => {
    setDeleting(secretName);
    try {
      await engineFetch(`/api/v1/secrets/${encodeURIComponent(secretName)}`, {
        method: 'DELETE',
      });
      refetch();
    } finally {
      setDeleting(null);
    }
  };

  return (
    <PageShell
      title="Secrets"
      description={`Environment variables for ${name}. Values are masked.`}
      primaryAction={{
        label: 'Add secret',
        icon: Plus,
        onClick: () => setAddOpen(true),
      }}
      loading={loading}
      empty={
        !loading && secrets.length === 0
          ? {
              icon: KeyRound,
              title: 'No secrets yet',
              description:
                'Add a secret to make it available to your apps at runtime.',
            }
          : undefined
      }
    >
      <div className="mb-4">
        <Button asChild variant="ghost" size="sm">
          <Link
            to={`/projects/${encodeURIComponent(name)}`}
            className="inline-flex items-center gap-1"
          >
            <ArrowLeft className="size-4" aria-hidden="true" />
            <span>Back to project</span>
          </Link>
        </Button>
      </div>

      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Key</Table.HeaderCell>
                <Table.HeaderCell>Value</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Environments
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Sync
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Updated
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Delete</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {secrets.map((s) => (
                <Table.Row key={s.name}>
                  <Table.Cell>
                    <span className="font-mono text-body-sm font-medium text-foreground">
                      {s.name}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <span className="font-mono text-body-sm text-muted-foreground tracking-widest">
                      ••••••••
                    </span>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <div className="flex flex-wrap gap-1">
                      {s.environments.map((env) => (
                        <Badge key={env} variant="outline" className="text-label">
                          {env}
                        </Badge>
                      ))}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <StatusDot
                      status={
                        s.eso_sync_status === 'synced'
                          ? 'live'
                          : s.eso_sync_status === 'failed'
                            ? 'failed'
                            : 'building'
                      }
                      label={s.eso_sync_status ?? 'unknown'}
                    />
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <span className="text-body-sm text-muted-foreground tabular-nums">
                      {s.updated_at ? formatDate(s.updated_at, 'relative') : '—'}
                    </span>
                  </Table.Cell>
                  <Table.Cell align="right">
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={deleting === s.name}
                      onClick={() => handleDelete(s.name)}
                      aria-label={`Delete ${s.name}`}
                    >
                      <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <AddSecretSheet
        open={addOpen}
        onOpenChange={setAddOpen}
        onCreated={() => refetch()}
      />
    </PageShell>
  );
}

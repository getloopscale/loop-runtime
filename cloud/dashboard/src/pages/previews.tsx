import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, ExternalLink, GitPullRequest, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  ConfirmDialog,
  PageShell,
  Table,
} from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { usePreviews } from '@/hooks/use-previews';
import { TableSkeleton } from '@/components/data/table-skeleton';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import type { Preview, PreviewStatus } from '@/lib/api/types';

const STATUS_BADGE: Record<
  PreviewStatus,
  'default' | 'warning' | 'success' | 'destructive' | 'outline'
> = {
  creating: 'warning',
  active: 'success',
  expiring: 'warning',
  failed: 'destructive',
  deleting: 'outline',
  deleted: 'outline',
};

function expiresIn(iso: string): string {
  const ms = new Date(iso).getTime() - Date.now();
  if (ms <= 0) return 'expired';
  const hours = Math.floor(ms / 3600000);
  if (hours < 1) return `${Math.floor(ms / 60000)}m`;
  if (hours < 24) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

/**
 * PreviewsPage — Plan v1.0 T3.2.
 *
 * Lists active preview environments for the project. Each row: PR title +
 * branch + URL + expires countdown + delete action.
 *
 * DESIGN.md §16 (danger zone choreography): delete now goes through a
 * ConfirmDialog with typed confirmation phrase. The previous silent delete
 * could nuke a teammate's PR preview by accident on a misclick.
 */
export function PreviewsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Previews`);
  const { previews, loading, error, refetch } = usePreviews(name);
  const [pendingDelete, setPendingDelete] = useState<Preview | null>(null);
  const [deleting, setDeleting] = useState(false);

  const handleConfirmDelete = async () => {
    if (!pendingDelete) return;
    setDeleting(true);
    try {
      await engineFetch(
        `/api/v1/projects/${encodeURIComponent(name)}/previews/${encodeURIComponent(pendingDelete.id)}`,
        { method: 'DELETE' },
      );
      refetch();
      setPendingDelete(null);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <PageShell
      title="Preview environments"
      description={`Per-PR deploys for ${name}.`}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={4} columns={5} /></Card>}
      error={
        error
          ? {
              message:
                'Could not load preview environments. The build service may be temporarily unreachable.',
              onRetry: refetch,
            }
          : undefined
      }
      empty={
        !loading && !error && previews.length === 0
          ? {
              icon: GitPullRequest,
              title: 'Open a pull request to spin up a preview',
              description:
                'LoopZero deploys an isolated environment per PR so reviewers can click a live URL instead of pulling the branch. Connect GitHub on the Repository tab if you haven\'t already.',
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
                <Table.HeaderCell>Pull Request</Table.HeaderCell>
                <Table.HeaderCell>Branch</Table.HeaderCell>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Expires in
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Actions</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {previews.map((p) => (
                <Table.Row key={p.id}>
                  <Table.Cell>
                    <span className="font-medium text-body-sm text-foreground">
                      {p.pr_title ?? p.branch}
                    </span>
                    {p.pr_number && (
                      <span className="ml-2 text-label text-muted-foreground">
                        #{p.pr_number}
                      </span>
                    )}
                  </Table.Cell>
                  <Table.Cell>
                    <span className="font-mono text-label text-muted-foreground">
                      {p.branch}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <Badge variant={STATUS_BADGE[p.status]} className="text-label uppercase">
                      {p.status}
                    </Badge>
                  </Table.Cell>
                  <Table.Cell className="hidden md:table-cell">
                    <span className="font-mono text-label text-muted-foreground tabular-nums">
                      {expiresIn(p.expires_at)}
                    </span>
                    <span className="ml-2 text-label text-muted-foreground">
                      ({formatDate(p.expires_at, 'date')})
                    </span>
                  </Table.Cell>
                  <Table.Cell align="right">
                    <div className="inline-flex items-center gap-1">
                      <Button asChild variant="ghost" size="sm">
                        <a href={p.url} target="_blank" rel="noopener noreferrer">
                          <ExternalLink className="size-4" aria-hidden="true" />
                          <span className="sr-only">Open URL</span>
                        </a>
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setPendingDelete(p)}
                        aria-label={`Delete preview ${p.pr_title ?? p.branch}`}
                      >
                        <Trash2 className="size-4 text-destructive" aria-hidden="true" />
                      </Button>
                    </div>
                  </Table.Cell>
                </Table.Row>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      {pendingDelete ? (
        <ConfirmDialog
          open={pendingDelete !== null}
          onOpenChange={(o) => !o && setPendingDelete(null)}
          title={`Delete preview "${pendingDelete.pr_title ?? pendingDelete.branch}"?`}
          description={
            <div className="space-y-2 text-sm">
              <p>
                The preview URL{' '}
                <code className="font-mono text-xs">{pendingDelete.url}</code>{' '}
                stops responding immediately. Reviewers and the PR comment link
                will receive a 404.
              </p>
              <p className="text-muted-foreground">
                A new preview will be created on the next push to this branch.
                Type the branch name to confirm.
              </p>
            </div>
          }
          confirmLabel={deleting ? 'Deleting…' : 'Delete preview'}
          cancelLabel="Cancel"
          intent="destructive"
          confirmationPhrase={pendingDelete.branch}
          loading={deleting}
          onConfirm={handleConfirmDelete}
        />
      ) : null}
    </PageShell>
  );
}

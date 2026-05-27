import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, ExternalLink, GitPullRequest, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  PageShell,
  Table,
} from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { usePreviews } from '@/hooks/use-previews';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';
import type { PreviewStatus } from '@/lib/api/types';

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
 */
export function PreviewsPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Previews`);
  const { previews, loading, refetch } = usePreviews(name);

  const handleDelete = async (id: string) => {
    await engineFetch(
      `/api/v1/projects/${encodeURIComponent(name)}/previews/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    );
    refetch();
  };

  return (
    <PageShell
      title="Preview environments"
      description={`Per-PR deploys for ${name}.`}
      loading={loading}
      empty={
        !loading && previews.length === 0
          ? {
              icon: GitPullRequest,
              title: 'No preview environments yet',
              description:
                'Open a pull request and LoopZero will deploy a preview automatically.',
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
                        onClick={() => handleDelete(p.id)}
                        aria-label={`Delete preview ${p.id}`}
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
    </PageShell>
  );
}

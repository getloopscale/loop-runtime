import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { ArrowLeft, Github, Link2, Loader2, Unlink2 } from 'lucide-react';
import {
  Alert,
  Badge,
  Button,
  Card,
  Checkbox,
  Input,
  Label,
  PageShell,
} from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';
import type { ProjectConnection } from '@/lib/api/types';

/**
 * ProjectConnectPage — Plan v1.0 T3.3.
 *
 * Shows GitHub repository connection state. Connected: repo + branch +
 * auto-deploy + disconnect button. Disconnected: "Connect GitHub" CTA
 * (OAuth handled by backend, not implemented in this iter — opens docs).
 */
export function ProjectConnectPage() {
  useRequireAuth();
  const { name = '' } = useParams<{ name: string }>();
  useSetPageTitle(`${name} · Connect`);

  const [conn, setConn] = useState<ProjectConnection | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [repoUrl, setRepoUrl] = useState('');
  const [branch, setBranch] = useState('main');
  const [autoDeploy, setAutoDeploy] = useState(true);
  const [connectError, setConnectError] = useState<string | null>(null);

  const fetchConn = async () => {
    setLoading(true);
    const data = await engineFetch<ProjectConnection>(
      `/api/v1/projects/${encodeURIComponent(name)}/connect`,
    );
    setConn(data);
    setLoading(false);
  };

  useEffect(() => {
    if (!name) return;
    let cancelled = false;
    engineFetch<ProjectConnection>(
      `/api/v1/projects/${encodeURIComponent(name)}/connect`,
    ).then((data) => {
      if (cancelled) return;
      setConn(data);
      setLoading(false);
    });
    return () => {
      cancelled = true;
    };
  }, [name]);

  const handleDisconnect = async () => {
    setBusy(true);
    try {
      await engineFetch(
        `/api/v1/projects/${encodeURIComponent(name)}/connect`,
        { method: 'DELETE' },
      );
      await fetchConn();
    } finally {
      setBusy(false);
    }
  };

  const handleConnect = async (e: React.FormEvent) => {
    e.preventDefault();
    setConnectError(null);
    if (!/^https:\/\/github\.com\/[^/]+\/[^/]+/.test(repoUrl)) {
      setConnectError('Enter a valid GitHub repo URL (https://github.com/owner/repo).');
      return;
    }
    setBusy(true);
    try {
      const res = await engineFetch<ProjectConnection>(
        `/api/v1/projects/${encodeURIComponent(name)}/connect`,
        {
          method: 'POST',
          body: JSON.stringify({
            repo_url: repoUrl,
            branch,
            auto_deploy: autoDeploy,
          }),
        },
      );
      if (res === null) {
        setConnectError("Couldn't connect repository. Try again in a moment.");
        return;
      }
      await fetchConn();
      setRepoUrl('');
    } finally {
      setBusy(false);
    }
  };

  if (loading) {
    return (
      <PageShell title="Connect">
        <div className="flex items-center justify-center py-12">
          <Loader2
            className="size-6 animate-spin text-muted-foreground"
            aria-label="Loading connection state"
          />
        </div>
      </PageShell>
    );
  }

  return (
    <PageShell title="Repository connection" description={`GitHub link for ${name}.`}>
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

      {conn?.connected ? (
        <Card>
          <Card.Header>
            <div className="flex items-center justify-between gap-4">
              <Card.Title className="inline-flex items-center gap-2">
                <Github className="size-5" aria-hidden="true" />
                Connected
              </Card.Title>
              <Badge variant="success" className="text-label uppercase">
                Active
              </Badge>
            </div>
          </Card.Header>
          <Card.Body className="space-y-4">
            <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2 text-body-sm">
              <div>
                <dt className="text-label uppercase text-muted-foreground">
                  Repository
                </dt>
                <dd className="mt-1 font-mono text-foreground break-all">
                  <a
                    href={conn.repo_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline hover:text-primary"
                  >
                    {conn.repo_url}
                  </a>
                </dd>
              </div>
              <div>
                <dt className="text-label uppercase text-muted-foreground">
                  Branch
                </dt>
                <dd className="mt-1 font-mono text-foreground">{conn.branch ?? '—'}</dd>
              </div>
              <div>
                <dt className="text-label uppercase text-muted-foreground">
                  Auto deploy
                </dt>
                <dd className="mt-1 text-foreground">
                  {conn.auto_deploy ? 'Enabled — deploy on push to branch' : 'Disabled'}
                </dd>
              </div>
              {conn.installation_id && (
                <div>
                  <dt className="text-label uppercase text-muted-foreground">
                    Installation
                  </dt>
                  <dd className="mt-1 font-mono text-foreground">
                    {conn.installation_id}
                  </dd>
                </div>
              )}
            </dl>

            <Button
              variant="destructive"
              size="sm"
              onClick={handleDisconnect}
              disabled={busy}
              className="inline-flex items-center gap-2"
            >
              {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              <Unlink2 className="size-4" aria-hidden="true" />
              Disconnect repository
            </Button>
          </Card.Body>
        </Card>
      ) : (
        <Card>
          <Card.Header>
            <Card.Title className="inline-flex items-center gap-2">
              <Github className="size-5" aria-hidden="true" />
              Connect a GitHub repository
            </Card.Title>
            <Card.Description>
              Auto-deploy on push to branch. LoopZero signs every image with cosign.
            </Card.Description>
          </Card.Header>
          <Card.Body>
            <form onSubmit={handleConnect} className="space-y-4">
              <div>
                <Label htmlFor="repo-url">Repository URL</Label>
                <Input
                  id="repo-url"
                  type="url"
                  placeholder="https://github.com/owner/repo"
                  value={repoUrl}
                  onChange={(e) => setRepoUrl(e.target.value)}
                  required
                  autoFocus
                />
              </div>

              <div>
                <Label htmlFor="branch">Branch</Label>
                <Input
                  id="branch"
                  type="text"
                  value={branch}
                  onChange={(e) => setBranch(e.target.value)}
                  required
                />
              </div>

              <div className="flex items-start gap-2">
                <Checkbox
                  id="auto-deploy"
                  checked={autoDeploy}
                  onCheckedChange={(c) => setAutoDeploy(c === true)}
                />
                <div>
                  <Label htmlFor="auto-deploy">Auto-deploy on push</Label>
                  <p className="text-label text-muted-foreground">
                    Triggers a build whenever a commit lands on the selected branch.
                  </p>
                </div>
              </div>

              {connectError && (
                <Alert intent="destructive" description={connectError} />
              )}

              <Button type="submit" disabled={busy}>
                {busy && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
                <Link2 className="size-4" aria-hidden="true" />
                {busy ? 'Connecting…' : 'Connect repository'}
              </Button>
            </form>
          </Card.Body>
        </Card>
      )}
    </PageShell>
  );
}

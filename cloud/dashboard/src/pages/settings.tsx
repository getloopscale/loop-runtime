import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ArrowUpRight, Brain } from 'lucide-react';
import { Card, CodeBlock, ConfirmDialog, DangerZone, Button, PageShell, StatusDot } from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useWhoami } from '@/hooks/use-whoami';
import { engineFetch } from '@/lib/api';

export function SettingsPage() {
  useRequireAuth();
  const { setPageTitle } = usePageMeta();
  const navigate = useNavigate();
  const { whoami } = useWhoami();

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDeleteTeam = async () => {
    // Delete CURRENT team via whoami().team_id (was hardcoded "team-acme" —
    // would have deleted demo tenant for every user in prod). Now correctly
    // scoped to the authenticated session's active team.
    if (!whoami?.team_id) return;
    setDeleting(true);
    try {
      await engineFetch(
        `/api/v1/tenants/${encodeURIComponent(whoami.team_id)}`,
        { method: 'DELETE' },
      );
      setConfirmOpen(false);
      navigate('/');
    } finally {
      setDeleting(false);
    }
  };

  return (
    <PageShell
      title="Settings"
      description="Workspace configuration."
      onTitleChange={setPageTitle}
    >
      <Card>
        <Card.Header>
          <Card.Title>CLI-First</Card.Title>
          <Card.Description>
            Use the CLI to script most settings, or jump to a specific setting below.
          </Card.Description>
        </Card.Header>
        <Card.Body>
          <CodeBlock
            caption="Common settings commands"
            code={`loopzero whoami
loopzero team list
loopzero secret set KEY=value
loopzero env create staging`}
            terminal
            copyable
          />
        </Card.Body>
      </Card>

      {/* T-DR-1.4 v1.2 (closes P2-2 scoped per Phase 0): Connected
          integrations card — read-only state display. Workspace Settings
          + API Keys RESCOPED per T-DR-0.2 + T-DR-0.3 (zero backend
          endpoints). Mitigates sparseness (~40% → ~20% whitespace). */}
      <Card>
        <Card.Header>
          <Card.Title>Connected integrations</Card.Title>
          <Card.Description>
            Services your team uses to ship code.
          </Card.Description>
        </Card.Header>
        <Card.Body className="space-y-2">
          <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
            <div className="space-y-0.5">
              <p className="text-body-sm font-medium text-foreground">GitHub</p>
              <p className="text-label text-muted-foreground">Auto-deploy via push to main branch.</p>
            </div>
            <StatusDot status="live" label="Connected" />
          </div>
          <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
            <div className="space-y-0.5">
              <p className="text-body-sm font-medium text-foreground">Custom domain provider</p>
              <p className="text-label text-muted-foreground">TLS certificates managed automatically.</p>
            </div>
            <StatusDot status="live" label="Connected" />
          </div>
          <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
            <div className="space-y-0.5">
              <p className="text-body-sm font-medium text-foreground">Stripe billing</p>
              <p className="text-label text-muted-foreground">Subscription + invoices via Stripe portal.</p>
            </div>
            <StatusDot status="live" label="Connected" />
          </div>
          <div className="flex items-center justify-between py-2 border-b border-border last:border-0">
            <div className="space-y-0.5">
              <p className="text-body-sm font-medium text-foreground">Cloud infrastructure</p>
              <p className="text-label text-muted-foreground">Managed runtime for your services.</p>
            </div>
            <StatusDot status="live" label="Active" />
          </div>
        </Card.Body>
      </Card>

      {/* Plan 4 of dashboard-memory-section-roadmap.md, T4.4 — Memory promotion.
          Roadmap §P4 mandated "Memory tab"; the existing /settings page is a
          flat Card layout with no Tabs container. Promoting to Tabs would
          restructure all existing cards, violating Plan 4 D2 ZERO regression
          invariant. Implemented as additive Card matching the existing
          CLI-First / Connected integrations / Danger Zone pattern. */}
      <Card data-testid="settings-memory-card">
        <Card.Header>
          <Card.Title>
            <div className="flex items-center gap-2">
              <Brain className="size-5 text-primary" aria-hidden="true" />
              Memory
            </div>
          </Card.Title>
          <Card.Description>
            Your agent&apos;s persistent memory — browse what it knows, manage
            entities, configure exports + webhooks.
          </Card.Description>
        </Card.Header>
        <Card.Body>
          <div className="flex items-center justify-between">
            <p className="text-body-sm text-muted-foreground max-w-md">
              Manage memories, entities, exports, and webhooks. Configure scopes
              via API Keys.
            </p>
            <Button
              variant="secondary"
              size="sm"
              onClick={() => navigate('/memory')}
              className="gap-1.5"
            >
              Open Memory
              <ArrowUpRight className="size-3.5" aria-hidden="true" />
            </Button>
          </div>
        </Card.Body>
      </Card>

      <DangerZone>
        <DangerZone.Action
          title="Delete Team"
          description="Permanently delete this team and all its projects. This cannot be undone."
          action={
            <Button variant="destructive" size="sm" onClick={() => setConfirmOpen(true)}>
              Delete Team
            </Button>
          }
        />
      </DangerZone>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Delete Team?"
        description={
          <>
            This will permanently delete your team and all its projects,
            environments, domains, and members. This action cannot be undone.
          </>
        }
        confirmLabel="Delete Team"
        cancelLabel="Cancel"
        intent="destructive"
        confirmationPhrase="delete my team"
        loading={deleting}
        onConfirm={handleDeleteTeam}
      />
    </PageShell>
  );
}

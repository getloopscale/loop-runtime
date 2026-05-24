import { useState } from 'react';
import { Card, CodeBlock, ConfirmDialog, DangerZone, Button } from '@loopzero/ui';
import { ContentGrid } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';

export function SettingsPage() {
  useRequireAuth();
  useSetPageTitle('Settings');

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const handleDeleteTeam = async () => {
    setDeleting(true);
    try {
      /* TODO: wire DELETE /api/v1/teams/{tenantId} when backend ready */
      await new Promise((r) => setTimeout(r, 1500));
      setConfirmOpen(false);
    } finally {
      setDeleting(false);
    }
  };

  return (
    <ContentGrid columns="1" gap={6}>
      <Card>
        <Card.Header>
          <Card.Title>CLI-First</Card.Title>
          <Card.Description>
            Most workspace settings are managed via the CLI for maximum
            control and scriptability.
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
    </ContentGrid>
  );
}

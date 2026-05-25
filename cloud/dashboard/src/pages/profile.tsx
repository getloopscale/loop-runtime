import { useState } from 'react';
import { Mail, User } from 'lucide-react';
import {
  Avatar,
  Button,
  Card,
  ConfirmDialog,
  DangerZone,
} from '@loopzero/ui';
import { ContentGrid } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { logout } from '@/lib/auth';
import { maskEmail } from '@/lib/format';

export function ProfilePage() {
  const session = useRequireAuth();
  useSetPageTitle('Profile');

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);

  if (!session) return null;

  const email = session.identity.traits.email ?? '';
  const name = session.identity.traits.name ?? '';
  const initials = (name || email).charAt(0).toUpperCase();

  const handleDeleteAccount = async () => {
    setDeleting(true);
    try {
      /* TODO: wire Kratos self-service settings flow for account deletion */
      await new Promise((r) => setTimeout(r, 1500));
      setConfirmOpen(false);
      await logout();
    } finally {
      setDeleting(false);
    }
  };

  return (
    <ContentGrid columns="1" gap={6}>
      <Card>
        <Card.Header>
          <Card.Title>Account</Card.Title>
          <Card.Description>Your identity and session info.</Card.Description>
        </Card.Header>
        <Card.Body className="space-y-4">
          <div className="flex items-center gap-4">
            <Avatar className="size-12">
              <Avatar.Fallback>{initials}</Avatar.Fallback>
            </Avatar>
            <div className="space-y-0.5">
              {name && (
                <p className="text-body-sm font-medium text-foreground">
                  {name}
                </p>
              )}
              <p className="font-mono text-label text-muted-foreground flex items-center gap-1.5">
                <Mail className="size-3" aria-hidden="true" />
                {maskEmail(email)}
              </p>
            </div>
          </div>
          <div className="rounded-lg bg-success/5 border border-success/20 p-3 text-label text-success flex items-center gap-2">
            <User className="size-3.5" aria-hidden="true" />
            Authenticated via secure session.
          </div>
        </Card.Body>
      </Card>

      <Card>
        <Card.Body>
          <Button variant="secondary" onClick={logout} className="w-full justify-center">
            Sign out
          </Button>
        </Card.Body>
      </Card>

      <DangerZone>
        <DangerZone.Action
          title="Delete Account"
          description="Permanently delete your account, all your projects, and all your data. This action cannot be undone."
          action={
            <Button variant="destructive" size="sm" onClick={() => setConfirmOpen(true)}>
              Delete Account
            </Button>
          }
        />
      </DangerZone>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Delete your account?"
        description={
          <>
            This will permanently delete your account and all associated data
            (projects, environments, domains). You will be signed out
            immediately. This cannot be undone.
          </>
        }
        confirmLabel="Delete My Account"
        cancelLabel="Cancel"
        intent="destructive"
        confirmationPhrase={email}
        loading={deleting}
        onConfirm={handleDeleteAccount}
      />
    </ContentGrid>
  );
}

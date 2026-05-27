import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { Alert, Button, Input, Label, Select, Sheet } from '@loopzero/ui';
import { engineFetch } from '@/lib/api';
import type { TeamMember, TeamRole } from '@/lib/api/types';

interface InviteMemberSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onInvited?: (member: TeamMember) => void;
}

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

/**
 * InviteMemberSheet — right-side Sheet (2026 FAANG pattern).
 * Sheet preserves team list context while operator fills the invite form.
 */
export function InviteMemberSheet({ open, onOpenChange, onInvited }: InviteMemberSheetProps) {
  const [email, setEmail] = useState('');
  const [role, setRole] = useState<TeamRole>('member');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (!EMAIL_PATTERN.test(email)) {
      setError('Enter a valid email address.');
      return;
    }
    setSubmitting(true);
    try {
      const res = await engineFetch<TeamMember>('/api/v1/teams/members', {
        method: 'POST',
        body: JSON.stringify({ email, role }),
      });
      if (res === null) {
        setError("Couldn't send invite. Try again in a moment.");
        return;
      }
      onInvited?.(res);
      setEmail('');
      onOpenChange(false);
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Something went wrong.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <Sheet.Content side="right" className="w-full sm:max-w-md flex flex-col p-0">
        <form onSubmit={handleSubmit} className="flex h-full flex-col">
          <Sheet.Header className="px-6 py-5 border-b border-border/60">
            <Sheet.Title>Invite teammate</Sheet.Title>
            <Sheet.Description>
              They'll receive an email with a join link.
            </Sheet.Description>
          </Sheet.Header>

          <Sheet.Body className="flex-1 overflow-y-auto px-6 py-5 space-y-4">
            <div>
              <Label htmlFor="invite-email">Email</Label>
              <Input
                id="invite-email"
                type="email"
                placeholder="teammate@acme.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                autoFocus
                required
              />
            </div>

            <div>
              <Label htmlFor="invite-role">Role</Label>
              <Select value={role} onValueChange={(v) => setRole(v as TeamRole)}>
                <Select.Trigger id="invite-role">
                  <Select.Value placeholder="Member" />
                </Select.Trigger>
                <Select.Content>
                  <Select.Item value="member">Member · view + deploy</Select.Item>
                  <Select.Item value="admin">Admin · manage team + billing</Select.Item>
                </Select.Content>
              </Select>
            </div>

            {error && <Alert intent="destructive" description={error} />}
          </Sheet.Body>

          <Sheet.Footer className="px-6 py-4 border-t border-border/60 gap-2">
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="size-4 animate-spin" aria-hidden="true" />}
              {submitting ? 'Sending…' : 'Send invite'}
            </Button>
          </Sheet.Footer>
        </form>
      </Sheet.Content>
    </Sheet>
  );
}

import { motion } from 'framer-motion';
import { UserPlus, Loader2 } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';

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

const roleColors: Record<string, 'default' | 'success' | 'warning'> = {
  owner: 'warning',
  admin: 'success',
  member: 'default',
};

export function TeamPage() {
  useRequireAuth();
  const { data, loading } = useEngineApi<TeamResponse>('/api/v1/teams/members');
  const members = data?.members || [];

  if (loading) {
    return <PageWrapper title="Team" description="Manage who has access."><Card><CardContent className="py-16 text-center"><Loader2 className="mx-auto h-6 w-6 text-white/20 animate-spin" /></CardContent></Card></PageWrapper>;
  }

  return (
    <PageWrapper
      title="Team"
      description="Manage who has access to your projects."
      action={
        <Button variant="secondary" size="sm">
          <UserPlus className="h-4 w-4" />
          Invite
        </Button>
      }
    >
      <Card>
        <CardContent className="p-0">
          {members.map((member, i) => {
            const displayName = member.name || member.username || member.email.split('@')[0];
            const avatar = displayName.charAt(0).toUpperCase();
            return (
            <motion.div
              key={member.email}
              initial={{ opacity: 0, x: -8 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.06, duration: 0.3 }}
              className="flex items-center justify-between px-5 py-4 border-b border-white/4 last:border-0 group hover:bg-white/2 transition-colors"
            >
              <div className="flex items-center gap-3">
                <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-4 text-sm font-semibold text-white/50 border border-white/6 group-hover:border-accent/20 group-hover:text-accent/70 transition-all">
                  {avatar}
                </div>
                <div>
                  <div className="text-sm font-medium text-white/80">{displayName}</div>
                  <div className="text-xs text-white/30 font-mono">{member.email}</div>
                </div>
              </div>
              <div className="flex items-center gap-3">
                {member.joined && <span className="text-xs text-white/20">{member.joined}</span>}
                <Badge variant={roleColors[member.role] || 'default'}>{member.role}</Badge>
              </div>
            </motion.div>
            );
          })}
        </CardContent>
      </Card>
    </PageWrapper>
  );
}

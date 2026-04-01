import { motion } from 'framer-motion';
import { UserPlus } from 'lucide-react';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';

const members = [
  { name: 'Paulo', email: 'paulo@loopzero.example', role: 'owner', avatar: 'P', joined: 'Jan 2026' },
  { name: 'Ana', email: 'ana@loopzero.example', role: 'admin', avatar: 'A', joined: 'Feb 2026' },
  { name: 'Diego', email: 'diego@loopzero.example', role: 'member', avatar: 'D', joined: 'Mar 2026' },
];

const roleColors: Record<string, 'default' | 'success' | 'warning'> = {
  owner: 'warning',
  admin: 'success',
  member: 'default',
};

export function TeamPage() {
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
          {members.map((member, i) => (
            <motion.div
              key={member.email}
              initial={{ opacity: 0, x: -8 }}
              animate={{ opacity: 1, x: 0 }}
              transition={{ delay: i * 0.06, duration: 0.3 }}
              className="flex items-center justify-between px-5 py-4 border-b border-white/[0.04] last:border-0 group hover:bg-white/[0.02] transition-colors"
            >
              <div className="flex items-center gap-3">
                <div className="flex h-9 w-9 items-center justify-center rounded-full bg-surface-4 text-sm font-semibold text-white/50 border border-white/[0.06] group-hover:border-accent/20 group-hover:text-accent/70 transition-all">
                  {member.avatar}
                </div>
                <div>
                  <div className="text-sm font-medium text-white/80">{member.name}</div>
                  <div className="text-xs text-white/30 font-mono">{member.email}</div>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <span className="text-xs text-white/20">{member.joined}</span>
                <Badge variant={roleColors[member.role]}>{member.role}</Badge>
              </div>
            </motion.div>
          ))}
        </CardContent>
      </Card>
    </PageWrapper>
  );
}

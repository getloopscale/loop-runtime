import { motion } from 'framer-motion';
import { User, Mail, UserCircle } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';
import { useRequireAuth } from '@/hooks/use-auth';
import { logout } from '@/lib/auth';

function maskEmail(email: string): string {
  const [local, domain] = email.split('@');
  if (!domain) return email;
  const [domainName, tld] = domain.split('.');
  return `${local.slice(0, 2)}***@${(domainName?.slice(0, 2) || '')}***.${tld || 'com'}`;
}

export function ProfilePage() {
  const session = useRequireAuth();
  if (!session) return null;

  const email = session.identity.traits.email || '';
  const name = session.identity.traits.name || '';

  return (
    <PageWrapper title="Profile" description="Your account information">
      <div className="max-w-2xl space-y-6">
        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }}>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <User className="h-4 w-4 text-accent/60" />
                Account
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              {name && (
                <div className="flex items-center gap-3 rounded-lg border border-white/[0.06] bg-surface-0 p-4">
                  <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10">
                    <UserCircle className="h-5 w-5 text-accent/60" />
                  </div>
                  <div>
                    <p className="text-sm font-medium text-white">{name}</p>
                    <p className="text-xs text-white/30">Name</p>
                  </div>
                </div>
              )}
              <div className="flex items-center gap-3 rounded-lg border border-white/[0.06] bg-surface-0 p-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10">
                  <Mail className="h-5 w-5 text-accent/60" />
                </div>
                <div>
                  <p className="text-sm font-medium text-white">{maskEmail(email)}</p>
                  <p className="text-xs text-white/30">Email address</p>
                </div>
              </div>
              <div className="rounded-lg bg-accent/5 border border-accent/10 p-3 text-xs text-accent/60">
                Authenticated via secure session. Your session is active.
              </div>
            </CardContent>
          </Card>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }}>
          <Card>
            <CardContent className="pt-6">
              <Button variant="danger" onClick={logout} className="w-full justify-center">
                Sign out
              </Button>
            </CardContent>
          </Card>
        </motion.div>
      </div>
    </PageWrapper>
  );
}

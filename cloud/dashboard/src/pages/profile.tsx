import { motion } from 'framer-motion';
import { User, Mail, Shield, ExternalLink } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { PageWrapper } from '@/components/page-wrapper';
import { useRequireAuth } from '@/hooks/use-auth';
import { getEmail, clearAuth } from '@/lib/api';

const ACCOUNTS_URL = import.meta.env.VITE_API_URL || 'http://localhost:13002';

export function ProfilePage() {
  useRequireAuth();
  const email = getEmail();

  const handleLogout = () => {
    clearAuth();
    window.location.href = '/login';
  };

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
            <CardContent className="space-y-4">
              <div className="flex items-center gap-3 rounded-lg border border-white/[0.06] bg-surface-0 p-4">
                <div className="flex h-10 w-10 items-center justify-center rounded-full bg-accent/10">
                  <Mail className="h-5 w-5 text-accent/60" />
                </div>
                <div>
                  <p className="text-sm font-medium text-white">{email || 'Not available'}</p>
                  <p className="text-xs text-white/30">Email address</p>
                </div>
              </div>
            </CardContent>
          </Card>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }}>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Shield className="h-4 w-4 text-accent/60" />
                Security
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-3">
              <a href={`${ACCOUNTS_URL}/self-service/settings/browser?return_to=${encodeURIComponent(window.location.origin + '/profile')}`}
                className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-surface-0 p-4 hover:border-white/[0.12] transition-colors">
                <div>
                  <p className="text-sm text-white">Change password</p>
                  <p className="text-xs text-white/30">Update your account password</p>
                </div>
                <ExternalLink className="h-4 w-4 text-white/20" />
              </a>

              <a href={`${ACCOUNTS_URL}/self-service/settings/browser?return_to=${encodeURIComponent(window.location.origin + '/profile')}`}
                className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-surface-0 p-4 hover:border-white/[0.12] transition-colors">
                <div>
                  <p className="text-sm text-white">Connected accounts</p>
                  <p className="text-xs text-white/30">Manage GitHub and Google connections</p>
                </div>
                <ExternalLink className="h-4 w-4 text-white/20" />
              </a>
            </CardContent>
          </Card>
        </motion.div>

        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.2 }}>
          <Card>
            <CardContent className="pt-6">
              <Button variant="danger" onClick={handleLogout} className="w-full justify-center">
                Sign out
              </Button>
            </CardContent>
          </Card>
        </motion.div>
      </div>
    </PageWrapper>
  );
}

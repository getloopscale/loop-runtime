import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { motion } from 'framer-motion';
import { KeyRound, CheckCircle2, Github, Loader2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { PageWrapper } from '@/components/page-wrapper';
import { useRequireAuth } from '@/hooks/use-auth';
import { type Flow, getFlow, submitFlow, getCsrfToken, getFlowMessages, getFieldErrors } from '@/lib/kratos';

const ACCOUNTS_URL = import.meta.env.VITE_ACCOUNTS_URL || 'http://localhost:4433';

export function AccountPage() {
  useRequireAuth();
  const [searchParams] = useSearchParams();
  const [flow, setFlow] = useState<Flow | null>(null);
  const [newPassword, setNewPassword] = useState('');
  const [loading, setLoading] = useState(false);
  const [success, setSuccess] = useState(false);
  const [errorMessage, setErrorMessage] = useState('');

  useEffect(() => {
    const flowId = searchParams.get('flow');
    if (flowId) {
      getFlow('settings', flowId).then(setFlow).catch(() => {
        // Flow expired — start new
        window.location.href = `${ACCOUNTS_URL}/self-service/settings/browser?return_to=${encodeURIComponent(window.location.origin + '/account')}`;
      });
    } else {
      window.location.href = `${ACCOUNTS_URL}/self-service/settings/browser?return_to=${encodeURIComponent(window.location.origin + '/account')}`;
    }
  }, [searchParams]);

  const handlePasswordChange = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!flow) return;
    setLoading(true);
    setErrorMessage('');
    setSuccess(false);
    try {
      const result = await submitFlow(flow.ui.action, flow.ui.method, {
        csrf_token: getCsrfToken(flow),
        password: newPassword,
        method: 'password',
      });
      const messages = getFlowMessages(result);
      const errors = getFieldErrors(result, 'password');
      if (errors.length > 0 || messages.some(m => m.includes('error'))) {
        setFlow(result);
        setErrorMessage(errors[0] || messages[0] || 'Failed to update password.');
      } else {
        setSuccess(true);
        setNewPassword('');
      }
    } catch {
      setErrorMessage('Something went wrong. Please try again.');
    } finally {
      setLoading(false);
    }
  };

  if (!flow) {
    return (
      <PageWrapper title="Account" description="Manage your account settings">
        <div className="flex items-center justify-center py-20">
          <Loader2 className="h-6 w-6 text-accent animate-spin" />
        </div>
      </PageWrapper>
    );
  }

  // Extract OIDC provider nodes (linked accounts)
  const oidcNodes = flow.ui.nodes.filter(n => n.group === 'oidc');

  return (
    <PageWrapper title="Account" description="Manage your account settings">
      <div className="max-w-2xl space-y-6">
        {/* Change Password */}
        <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }}>
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <KeyRound className="h-4 w-4 text-accent/60" />
                Change password
              </CardTitle>
            </CardHeader>
            <CardContent>
              {success && (
                <div className="mb-4 rounded-lg bg-accent/10 border border-accent/20 p-3 text-sm text-accent flex items-center gap-2">
                  <CheckCircle2 className="h-4 w-4" /> Password updated successfully.
                </div>
              )}
              <form onSubmit={handlePasswordChange} className="space-y-4">
                <div>
                  <label className="block text-xs text-white/40 mb-1.5">New password</label>
                  <input type="password" value={newPassword} onChange={e => setNewPassword(e.target.value)} required placeholder="Min. 8 characters"
                    className="w-full rounded-lg border border-white/[0.08] bg-surface-0 px-3 py-2.5 text-sm text-white placeholder:text-white/20 focus:border-accent/30 focus:outline-none" />
                </div>
                {errorMessage && <p className="text-xs text-danger">{errorMessage}</p>}
                <Button type="submit" size="sm" disabled={loading}>
                  {loading ? 'Updating...' : 'Update password'}
                </Button>
              </form>
            </CardContent>
          </Card>
        </motion.div>

        {/* Connected Accounts */}
        {oidcNodes.length > 0 && (
          <motion.div initial={{ opacity: 0, y: 12 }} animate={{ opacity: 1, y: 0 }} transition={{ delay: 0.1 }}>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Github className="h-4 w-4 text-accent/60" />
                  Connected accounts
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  {oidcNodes.map((node, i) => (
                    <div key={i} className="flex items-center justify-between rounded-lg border border-white/[0.06] bg-surface-0 p-3">
                      <span className="text-sm text-white/60 capitalize">{node.attributes.value || node.meta.label?.text || 'Social account'}</span>
                      <span className="text-xs text-accent/60">Connected</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </motion.div>
        )}
      </div>
    </PageWrapper>
  );
}

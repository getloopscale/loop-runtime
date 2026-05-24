import { useState } from 'react';
import { motion } from 'framer-motion';
import { Globe, Shield, Clock, AlertCircle, Loader2, RefreshCw } from 'lucide-react';
import { Card } from '@loopzero/ui';
import { Badge } from '@loopzero/ui';
import { Button } from '@loopzero/ui';
import { PageWrapper } from '@/components/page-wrapper';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';

interface Domain {
  id: string;
  domain: string;
  project: string;
  app: string;
  environment: string;
  status: string;
  tls?: { status: string; expires_at?: string };
  verification?: { txt_name: string; txt_value: string };
  cname_target: string;
  created_at: string;
  verified_at?: string;
}

interface DomainsResponse {
  domains: Domain[];
  total: number;
}

const container = {
  hidden: { opacity: 0 },
  show: { opacity: 1, transition: { staggerChildren: 0.08 } },
};

const item = {
  hidden: { opacity: 0, y: 12 },
  show: { opacity: 1, y: 0, transition: { duration: 0.4, ease: [0.25, 0.46, 0.45, 0.94] } },
};

function statusColor(status: string): string {
  switch (status) {
    case 'active': return 'bg-green-500/10 text-green-400 border-green-500/20';
    case 'verified':
    case 'provisioning': return 'bg-blue-500/10 text-blue-400 border-blue-500/20';
    case 'pending_verification': return 'bg-yellow-500/10 text-yellow-400 border-yellow-500/20';
    case 'failed': return 'bg-red-500/10 text-red-400 border-red-500/20';
    case 'removing': return 'bg-white/5 text-white/40 border-white/10';
    default: return 'bg-white/5 text-white/40 border-white/10';
  }
}

function statusLabel(status: string): string {
  switch (status) {
    case 'pending_verification': return 'Pending DNS';
    case 'verified': return 'Verified';
    case 'provisioning': return 'Provisioning';
    case 'active': return 'Active';
    case 'failed': return 'Failed';
    case 'removing': return 'Removing';
    default: return status;
  }
}

export function DomainsPage() {
  useRequireAuth();
  const { data, loading } = useEngineApi<DomainsResponse>('/api/v1/domains');
  const domains = data?.domains || [];
  const [verifying, setVerifying] = useState<string | null>(null);

  const handleVerify = async (domain: string) => {
    setVerifying(domain);
    try {
      await engineFetch(`/api/v1/domains/${encodeURIComponent(domain)}/verify`, { method: 'POST' });
      window.location.reload();
    } finally {
      setVerifying(null);
    }
  };

  return (
    <PageWrapper title="Custom Domains" description="Map your own domains to deployed apps.">
      {loading ? (
        <Card><Card.Body className="py-16 text-center"><Loader2 className="mx-auto h-6 w-6 text-white/20 animate-spin" /></Card.Body></Card>
      ) : domains.length === 0 ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <Globe className="mx-auto h-10 w-10 text-white/10 mb-4" />
            <p className="text-white/40 mb-2">No custom domains configured</p>
            <p className="text-sm text-white/20">
              Run <code className="font-mono bg-surface-3 px-2 py-0.5 rounded-sm text-accent/70">loopzero domain add &lt;domain&gt; --project &lt;p&gt; --app &lt;a&gt; --env &lt;e&gt;</code> to add one
            </p>
          </Card.Body>
        </Card>
      ) : (
        <motion.div variants={container} initial="hidden" animate="show" className="space-y-3">
          {domains.map((d) => (
            <motion.div key={d.id} variants={item}>
              <Card className="group hover:border-accent/20">
                <Card.Body className="p-0">
                  <div className="flex items-center justify-between p-5">
                    <div className="flex items-center gap-4">
                      <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent/5 border border-accent/10">
                        <Globe className="h-4 w-4 text-accent/60" />
                      </div>
                      <div>
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-white/90">{d.domain}</span>
                          <Badge variant="outline" className={statusColor(d.status)}>
                            {statusLabel(d.status)}
                          </Badge>
                          {d.tls?.status === 'issued' && (
                            <Badge variant="outline" className="bg-green-500/5 text-green-400/60 border-green-500/10">
                              <Shield className="h-3 w-3 mr-1" />TLS
                            </Badge>
                          )}
                        </div>
                        <div className="flex items-center gap-3 mt-1 text-xs text-white/30">
                          <span>{d.project}/{d.app}</span>
                          <span>{d.environment}</span>
                          {d.verified_at && (
                            <span className="flex items-center gap-1">
                              <Clock className="h-3 w-3" />
                              Verified {new Date(d.verified_at).toLocaleDateString()}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-2">
                      {d.status === 'pending_verification' && (
                        <Button
                          size="sm"
                          variant="secondary"
                          className="text-xs"
                          disabled={verifying === d.domain}
                          onClick={() => handleVerify(d.domain)}
                        >
                          {verifying === d.domain ? (
                            <Loader2 className="h-3 w-3 animate-spin mr-1" />
                          ) : (
                            <RefreshCw className="h-3 w-3 mr-1" />
                          )}
                          Verify
                        </Button>
                      )}
                    </div>
                  </div>

                  {/* DNS instructions for pending domains */}
                  {d.status === 'pending_verification' && d.verification && (
                    <div className="border-t border-white/5 px-5 py-4 bg-surface-1/50">
                      <div className="flex items-start gap-2 mb-3">
                        <AlertCircle className="h-4 w-4 text-yellow-400/60 mt-0.5" />
                        <span className="text-xs text-white/50">Add these DNS records to activate your domain:</span>
                      </div>
                      <div className="font-mono text-xs space-y-2">
                        <div className="flex gap-4 bg-surface-2/50 rounded-sm p-2">
                          <span className="text-white/30 w-14">CNAME</span>
                          <span className="text-white/60">{d.domain}</span>
                          <span className="text-white/20">→</span>
                          <span className="text-accent/70">{d.cname_target}</span>
                        </div>
                        <div className="flex gap-4 bg-surface-2/50 rounded-sm p-2">
                          <span className="text-white/30 w-14">TXT</span>
                          <span className="text-white/60">{d.verification.txt_name}</span>
                          <span className="text-white/20">→</span>
                          <span className="text-accent/70">{d.verification.txt_value}</span>
                        </div>
                      </div>
                    </div>
                  )}
                </Card.Body>
              </Card>
            </motion.div>
          ))}
        </motion.div>
      )}
    </PageWrapper>
  );
}

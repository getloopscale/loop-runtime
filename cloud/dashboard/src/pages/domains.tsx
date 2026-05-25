import { useState } from 'react';
import { Globe, Loader2, RefreshCw, Shield } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CodeBlock,
  CopyButton,
  EmptyState,
  StatusDot,
  type StatusKind,
} from '@loopzero/ui';
import { ActionBar } from '@/components/layout';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { engineFetch } from '@/lib/api';
import { formatDate } from '@/lib/format';

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

function mapStatus(status: string): StatusKind {
  switch (status) {
    case 'active':
      return 'live';
    case 'verified':
    case 'provisioning':
      return 'building';
    case 'pending_verification':
      return 'warning';
    case 'failed':
      return 'failed';
    default:
      return 'idle';
  }
}

function statusLabel(status: string): string {
  const map: Record<string, string> = {
    pending_verification: 'Pending DNS',
    verified: 'Verified',
    provisioning: 'Provisioning',
    active: 'Active',
    failed: 'Failed',
    removing: 'Removing',
  };
  return map[status] ?? status;
}

export function DomainsPage() {
  useRequireAuth();
  useSetPageTitle('Domains');

  const { data, loading } = useEngineApi<DomainsResponse>('/api/v1/domains');
  const domains = data?.domains ?? [];
  const [verifying, setVerifying] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');

  const filtered = domains.filter((d) =>
    d.domain.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  const handleVerify = async (domain: string) => {
    setVerifying(domain);
    try {
      await engineFetch(`/api/v1/domains/${encodeURIComponent(domain)}/verify`, {
        method: 'POST',
      });
      window.location.reload();
    } finally {
      setVerifying(null);
    }
  };

  return (
    <>
      <ActionBar
        searchPlaceholder="Search domains..."
        searchValue={searchQuery}
        onSearchChange={setSearchQuery}
        primaryAction={{
          label: 'Add Domain',
          icon: Globe,
          onClick: () => {
            window.open('https://docs.loopzero.example/cli/domain', '_blank');
          },
        }}
      />

      {loading ? (
        <Card>
          <Card.Body className="py-16 text-center">
            <Loader2
              className="mx-auto size-6 text-muted-foreground animate-spin"
              aria-label="Loading domains"
            />
          </Card.Body>
        </Card>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon={Globe}
          title={
            searchQuery
              ? 'No matching domains'
              : 'No custom domains configured'
          }
          description={
            searchQuery ? (
              <>Try a different search term.</>
            ) : (
              <>
                Run{' '}
                <code className="bg-muted px-2 py-0.5 rounded font-mono">
                  loopzero domain add &lt;domain&gt;
                </code>{' '}
                to add one.
              </>
            )
          }
        />
      ) : (
        <div className="space-y-3">
          {filtered.map((d) => (
            <Card key={d.id}>
              <Card.Body className="p-0">
                <div className="flex items-center justify-between p-5">
                  <div className="flex items-center gap-4">
                    <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/5 border border-primary/10">
                      <Globe
                        className="size-4 text-primary"
                        aria-hidden="true"
                      />
                    </div>
                    <div>
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-foreground">
                          {d.domain}
                        </span>
                        <StatusDot
                          status={mapStatus(d.status)}
                          label={statusLabel(d.status)}
                        />
                        {d.tls?.status === 'issued' && (
                          <Badge variant="success">
                            <Shield
                              className="size-3 mr-1"
                              aria-hidden="true"
                            />
                            TLS
                          </Badge>
                        )}
                      </div>
                      <div className="flex items-center gap-3 mt-1 text-label text-muted-foreground">
                        <span>
                          {d.project}/{d.app}
                        </span>
                        <span>·</span>
                        <span>{d.environment}</span>
                        {d.verified_at && (
                          <>
                            <span>·</span>
                            <span>
                              Verified{' '}
                              {formatDate(d.verified_at, 'date')}
                            </span>
                          </>
                        )}
                      </div>
                    </div>
                  </div>

                  {d.status === 'pending_verification' && (
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={verifying === d.domain}
                      onClick={() => handleVerify(d.domain)}
                      className="gap-1.5"
                    >
                      {verifying === d.domain ? (
                        <Loader2
                          className="size-3 animate-spin"
                          aria-hidden="true"
                        />
                      ) : (
                        <RefreshCw className="size-3" aria-hidden="true" />
                      )}
                      Verify
                    </Button>
                  )}
                </div>

                {/* DNS records for pending domains (D-7: CopyButton + CodeBlock) */}
                {d.status === 'pending_verification' && d.verification && (
                  <div className="border-t border-border/40 px-5 py-4 bg-muted/20">
                    <p className="text-label text-muted-foreground mb-3">
                      Add these DNS records to activate your domain:
                    </p>
                    <div className="space-y-2">
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className="font-mono">
                          CNAME
                        </Badge>
                        <div className="flex-1 font-mono text-label text-foreground">
                          {d.domain} → {d.cname_target}
                        </div>
                        <CopyButton value={d.cname_target} label="Copy" />
                      </div>
                      <div className="flex items-center gap-2">
                        <Badge variant="outline" className="font-mono">
                          TXT
                        </Badge>
                        <div className="flex-1 font-mono text-label text-foreground">
                          {d.verification.txt_name} → {d.verification.txt_value}
                        </div>
                        <CopyButton
                          value={d.verification.txt_value}
                          label="Copy"
                        />
                      </div>
                    </div>
                    <CodeBlock
                      caption={`Or run via CLI:`}
                      code={`loopzero domain verify ${d.domain}`}
                      terminal
                      copyable
                      className="mt-3"
                    />
                  </div>
                )}
              </Card.Body>
            </Card>
          ))}
        </div>
      )}
    </>
  );
}

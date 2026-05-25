import { useState } from 'react';
import { Globe, Loader2, RefreshCw, Shield } from 'lucide-react';
import {
  Badge,
  Button,
  CodeBlock,
  CopyButton,
  DataTable,
  EmptyState,
  StatusDot,
  type DataTableColumn,
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

/**
 * DNS records inline expansion for pending_verification domains.
 * Rendered via DataTable expandable row. Closes Deep Review § 2.4 P1
 * inline-DNS-records pattern (DSS § 9.2).
 */
function DnsRecords({ domain }: { domain: Domain }) {
  if (!domain.verification) return null;
  return (
    <div className="px-4 py-3 bg-muted/20 border-t border-border/40">
      <p className="text-label text-muted-foreground mb-3">
        Add these DNS records to activate your domain:
      </p>
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="font-mono">CNAME</Badge>
          <div className="flex-1 font-mono text-label text-foreground">
            {domain.domain} → {domain.cname_target}
          </div>
          <CopyButton value={domain.cname_target} label="Copy" />
        </div>
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="font-mono">TXT</Badge>
          <div className="flex-1 font-mono text-label text-foreground">
            {domain.verification.txt_name} → {domain.verification.txt_value}
          </div>
          <CopyButton value={domain.verification.txt_value} label="Copy" />
        </div>
      </div>
      <CodeBlock
        caption="Or run via CLI:"
        code={`loopzero domain verify ${domain.domain}`}
        terminal
        copyable
        className="mt-3"
      />
    </div>
  );
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

  const handleVerify = async (domainName: string) => {
    setVerifying(domainName);
    try {
      await engineFetch(`/api/v1/domains/${encodeURIComponent(domainName)}/verify`, {
        method: 'POST',
      });
      window.location.reload();
    } finally {
      setVerifying(null);
    }
  };

  // DataTable column definitions per Brief #5 + DSS § 4.4 conventions.
  const columns: DataTableColumn<Domain>[] = [
    {
      key: 'domain',
      label: 'Domain',
      sortable: true,
      render: (d) => (
        <div className="flex items-center gap-2">
          <Globe className="size-4 text-muted-foreground shrink-0" aria-hidden="true" />
          <span className="font-medium text-foreground">{d.domain}</span>
          {d.tls?.status === 'issued' && (
            <Badge variant="success" className="text-label">
              <Shield className="size-3 mr-1" aria-hidden="true" />
              TLS
            </Badge>
          )}
        </div>
      ),
    },
    {
      key: 'project',
      label: 'Project / App',
      render: (d) => (
        <span className="text-body-sm text-muted-foreground font-mono">
          {d.project}/{d.app}
        </span>
      ),
    },
    {
      key: 'environment',
      label: 'Environment',
      render: (d) => <span className="text-body-sm">{d.environment}</span>,
    },
    {
      key: 'status',
      label: 'Status',
      sortable: true,
      render: (d) => (
        <StatusDot status={mapStatus(d.status)} label={statusLabel(d.status)} />
      ),
    },
    {
      key: 'verified_at',
      label: 'Verified',
      sortable: true,
      render: (d) => (
        <span className="text-body-sm text-muted-foreground tabular-nums">
          {d.verified_at ? formatDate(d.verified_at, 'date') : '—'}
        </span>
      ),
    },
    {
      // Inline Verify column (vs rowActions DropdownMenu wrap).
      // Verify is contextual single-action (only when pending_verification) —
      // hiding behind an ellipsis menu adds friction without information density gain.
      key: '__verify',
      label: '',
      align: 'right',
      render: (d) =>
        d.status === 'pending_verification' ? (
          <Button
            size="sm"
            variant="secondary"
            disabled={verifying === d.domain}
            onClick={() => handleVerify(d.domain)}
            className="gap-1.5"
          >
            {verifying === d.domain ? (
              <Loader2 className="size-3 animate-spin" aria-hidden="true" />
            ) : (
              <RefreshCw className="size-3" aria-hidden="true" />
            )}
            Verify
          </Button>
        ) : null,
    },
  ];

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

      <DataTable<Domain>
        columns={columns}
        data={filtered}
        rowKey={(d) => d.id}
        stickyHeader
        // DSS § 9.2 inline DNS records — Brief #5 DataTable expandable closes
        // Deep Review § 2.4 P1 (was card-grid with full-card expansion).
        expandable={(d) => (d.status === 'pending_verification' ? <DnsRecords domain={d} /> : null)}
        loading={loading}
        emptyState={
          <EmptyState
            icon={Globe}
            title={searchQuery ? 'No matching domains' : 'No custom domains configured'}
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
        }
      />
    </>
  );
}

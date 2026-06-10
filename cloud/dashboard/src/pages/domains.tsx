import { Fragment, useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AddDomainSheet } from "@/components/sheets/add-domain-sheet";
import { Globe, Loader2, RefreshCw, Shield, Trash2 } from 'lucide-react';
import {
  Badge,
  Button,
  Card,
  CodeBlock,
  ConfirmDialog,
  CopyButton,
  PageShell,
  StatusDot,
  Table,
  type StatusKind,
} from '@loopzerokit/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { useDeleteDomain, useVerifyDomain } from '@/hooks/mutations/use-domains';
import { TableSkeleton } from '@/components/data/table-skeleton';
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
 * Rendered as a second Table.Row with colSpan covering all columns —
 * matches the Table primitive pattern used in environments.tsx (no
 * DataTable expandable composite needed).
 */
function DnsRecordsRow({ domain, columnCount }: { domain: Domain; columnCount: number }) {
  if (!domain.verification) return null;
  return (
    <Table.Row>
      <Table.Cell colSpan={columnCount} className="bg-muted/20 p-0">
        <div className="px-4 py-3">
          <p className="text-label text-muted-foreground mb-3">
            Add these DNS records to activate your domain:
          </p>
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="font-mono">
                CNAME
              </Badge>
              <div className="flex-1 font-mono text-label text-foreground">
                {domain.domain} → {domain.cname_target}
              </div>
              <CopyButton value={domain.cname_target} label="Copy" />
            </div>
            <div className="flex items-center gap-2">
              <Badge variant="outline" className="font-mono">
                TXT
              </Badge>
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
      </Table.Cell>
    </Table.Row>
  );
}

/**
 * DomainsPage — visual consistency pattern: PageShell + Card + Table primitive,
 * matching environments.tsx style (per 2026-05-26 sponsor directive
 * "all tables must look like /environments"). DNS records appear inline as
 * a colspan row directly below pending_verification rows — no DataTable
 * expandable composite, no click-to-expand state. The information is
 * actionable so it always shows.
 */
export function DomainsPage() {
  useRequireAuth();
  useSetPageTitle('Domains');

  const { data, loading } = useEngineApi<DomainsResponse>('/api/v1/domains');
  const domains = data?.domains ?? [];
  const [verifying, setVerifying] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [addOpen, setAddOpen] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();

  // EC-03 consumer wiring (Sprint 2 T2.3): CommandPalette "Add domain"
  // navigates here with ?action=add. Open sheet + auto-clear param.
  useEffect(() => {
    if (searchParams.get('action') === 'add') {
      setAddOpen(true);
      const next = new URLSearchParams(searchParams);
      next.delete('action');
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);
  // T2.2: ConfirmDialog substitui native confirm()
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);

  const filtered = domains.filter((d) =>
    d.domain.toLowerCase().includes(searchQuery.toLowerCase()),
  );

  // T1.3: mutations invalidate domains cache → list re-renders sem reload.
  const verifyDomain = useVerifyDomain();
  const deleteDomain = useDeleteDomain();

  const handleVerify = (domainName: string) => {
    setVerifying(domainName);
    verifyDomain.mutate(domainName, { onSettled: () => setVerifying(null) });
  };

  const handleDelete = (domainName: string) => {
    setDeleteTarget(domainName);
  };

  // Column count for DNS records colspan row (must match HeaderCell count below).
  const COLUMN_COUNT = 6;

  return (
    <PageShell
      title="Domains"
      search={{
        placeholder: 'Search domains...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'Add Domain',
        icon: Globe,
        // SG-03 CLOSED (plan v1.0 T4.4, 2026-05-26): real AddDomainSheet wired
        // to POST /api/v1/domains. Returns DNS verification records inline.
        onClick: () => setAddOpen(true),
      }}
      loading={loading}
      loadingNode={<Card><TableSkeleton rows={5} columns={5} /></Card>}
      empty={
        !loading && filtered.length === 0
          ? searchQuery
            ? {
                icon: Globe,
                title: 'No domains match your search',
                description: `No domain contains "${searchQuery}". Try a different term or clear the search.`,
                action: { label: 'Clear search', onClick: () => setSearchQuery('') },
              }
            : {
                icon: Globe,
                title: 'Attach your first custom domain',
                description: 'LoopZero provisions and renews TLS certificates automatically, including SAN entries and DNS-01 validation. Add a domain, point your DNS to the verification record we show, and your app serves traffic at the new URL within minutes.',
                action: { label: 'Add domain', onClick: () => setAddOpen(true) },
              }
          : undefined
      }
    >
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Domain</Table.HeaderCell>
                <Table.HeaderCell>Project / App</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Environment
                </Table.HeaderCell>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Verified
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Actions</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filtered.map((d) => (
                <Fragment key={d.id}>
                  <Table.Row>
                    <Table.Cell>
                      <div className="flex items-center gap-2">
                        <Globe
                          className="size-4 text-muted-foreground shrink-0"
                          aria-hidden="true"
                        />
                        <span className="font-medium text-body-sm text-foreground">
                          {d.domain}
                        </span>
                        {d.tls?.status === 'issued' && (
                          <Badge variant="success" className="text-label">
                            <Shield className="size-3 mr-1" aria-hidden="true" />
                            TLS
                          </Badge>
                        )}
                      </div>
                    </Table.Cell>
                    <Table.Cell>
                      <span className="font-mono text-label text-muted-foreground">
                        {d.project}/{d.app}
                      </span>
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="font-mono text-label text-muted-foreground">
                        {d.environment}
                      </span>
                    </Table.Cell>
                    <Table.Cell>
                      <StatusDot
                        status={mapStatus(d.status)}
                        label={statusLabel(d.status)}
                      />
                    </Table.Cell>
                    <Table.Cell className="hidden md:table-cell">
                      <span className="text-body-sm text-muted-foreground tabular-nums">
                        {d.verified_at ? formatDate(d.verified_at, 'date') : '—'}
                      </span>
                    </Table.Cell>
                    <Table.Cell align="right">
                      <div className="inline-flex items-center gap-1">
                        {d.status === 'pending_verification' ? (
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
                        ) : null}
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => handleDelete(d.domain)}
                          aria-label={`Delete ${d.domain}`}
                        >
                          <Trash2
                            className="size-4 text-destructive"
                            aria-hidden="true"
                          />
                        </Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                  {d.status === 'pending_verification' && (
                    <DnsRecordsRow domain={d} columnCount={COLUMN_COUNT} />
                  )}
                </Fragment>
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <AddDomainSheet open={addOpen} onOpenChange={setAddOpen} />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
        title={`Remove "${deleteTarget ?? ''}"?`}
        description={
          <>
            This removes the domain from your workspace. DNS records at your
            provider stay in place — you'll need to remove them separately.
          </>
        }
        confirmLabel="Remove domain"
        cancelLabel="Cancel"
        intent="destructive"
        loading={deleteDomain.isPending}
        onConfirm={() => {
          if (deleteTarget) {
            deleteDomain.mutate(deleteTarget, {
              onSettled: () => setDeleteTarget(null),
            });
          }
        }}
      />
    </PageShell>
  );
}

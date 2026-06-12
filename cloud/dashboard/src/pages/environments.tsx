import { Fragment, useState } from 'react';
import { Box, Lock, Plus, Rocket, Trash2 } from 'lucide-react';
import { Button, Card, PageShell, StatusDot, Table } from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useEngineApi } from '@/hooks/use-api';
import { useRequireAuth } from '@/hooks/use-auth';
import { CreateEnvironmentSheet } from '@/components/sheets/create-environment-sheet';
import { DeleteEnvironmentDialog } from '@/components/dialogs/delete-environment-dialog';
import { PromoteDialog } from '@/components/dialogs/promote-dialog';
import { EnvironmentKPIRow } from '@/components/environments/environment-kpi-row';
import { PreviewRow, type PreviewSlot } from '@/components/environments/preview-row';

interface Environment {
  name: string;
  status?: string;
  // M2 v1.2 T5.2 (2026-06-07): per-(project, env) tuple. Empty/undefined
  // string = legacy production entry (ADR D2 backward compat).
  project?: string;
  projects?: number;
  protected?: boolean;
  domain?: string;
  // KPI fields (v1.1 — page-level SSE multiplex will populate when wired)
  replicas?: number | null;
  uptime?: number | null;
  last_deploy?: string | null;
  last_deploy_sha?: string | null;
}

interface EnvsResponse {
  environments: Environment[];
  // 2026-06-07 per-project model (migration 000028).
  // `permanent` is the list of permanent env names.
  // `previews` is one entry per PROJECT — the tenant can have N
  // projects, each with its own slot. Old API responses omit these
  // fields — the page degrades gracefully (legacy table only).
  permanent?: string[];
  previews?: PreviewSlot[];
}

type EnvStatusKind = 'live' | 'building' | 'failed' | 'idle' | 'warning';

function mapStatus(status?: string): EnvStatusKind {
  switch (status?.toLowerCase()) {
    case 'live':
    case 'active':
    case 'healthy':
      return 'live';
    case 'building':
    case 'deploying':
      return 'building';
    case 'failed':
    case 'error':
      return 'failed';
    case 'warning':
    case 'degraded':
      return 'warning';
    default:
      return 'idle';
  }
}

/**
 * Title-case a status string for display, handling snake_case (EC-9).
 *   'live'                  → 'Live'
 *   'pending_verification'  → 'Pending Verification'
 *   undefined / ''          → 'Idle'
 */
function statusLabel(s?: string): string {
  if (!s) return 'Idle';
  return s
    .split('_')
    .map((w) => (w.length > 0 ? w.charAt(0).toUpperCase() + w.slice(1) : w))
    .join(' ');
}

/**
 * EnvironmentsPage — Plan J-10 v1.1.
 *
 * Wire-up of:
 *   - CreateEnvironmentSheet (existing, hook-driven)
 *   - DeleteEnvironmentDialog (NEW T2.2 — type-to-confirm + cascade warning)
 *   - PromoteDialog (refactored T2.3 — dropdowns + create-on-the-fly)
 *   - EnvironmentKPIRow (NEW T2.4 — 3 signals + honest empty state)
 *   - "View Previews →" cross-link (NEW T2.5 — strict CRD separation ADR D5)
 *
 * The page does NOT need to wire PlanLimitUpgradeModal — that's mounted
 * top-level in app.tsx and listens for `loopzero:plan-limit` window events
 * from the 402 interceptor in lib/api.ts.
 */
export function EnvironmentsPage() {
  useRequireAuth();
  const { setPageTitle } = usePageMeta();
  const { data, loading } = useEngineApi<EnvsResponse>('/api/v1/environments');
  const [searchQuery, setSearchQuery] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Environment | null>(null);
  const [promoteOpen, setPromoteOpen] = useState(false);
  const [promoteFrom, setPromoteFrom] = useState<string | undefined>(undefined);

  // J-10 v1.1 ADR (LIVE catch 2026-06-06): projectName was hardcoded to
  // 'teste' which always 404'd in real tenants. Resolve dynamically:
  // 1. Prefer ?project=<name> URL search param (planned sub-route shape).
  // 2. Fallback to first project of the tenant (single-project case which
  //    is the common Free-tier shape today).
  // 3. Fallback to 'teste' literal so the legacy fixture flow keeps
  //    working in vitest + Playwright with VITE_USE_MOCKS=true.
  const { data: projectsData } = useEngineApi<{
    projects: Array<{ name: string }>;
  }>('/api/v1/projects');
  const urlSearchParams =
    typeof window !== 'undefined'
      ? new URLSearchParams(window.location.search)
      : new URLSearchParams();
  const projectName =
    urlSearchParams.get('project') ??
    projectsData?.projects?.[0]?.name ??
    'teste';

  const environments = data?.environments ?? [];

  // M2 v1.2 T5.2 (2026-06-07): search matches across both name AND
  // project so "web" surfaces every env in project=web, "staging"
  // surfaces every staging env across all projects, and "web/staging"
  // surfaces the exact tuple. Legacy entries (no project) match only
  // by name.
  const q = searchQuery.toLowerCase().trim();
  const filtered = environments.filter((e) => {
    if (!q) return true;
    const name = (e.name ?? '').toLowerCase();
    const proj = (e.project ?? '').toLowerCase();
    if (q.includes('/')) {
      const [qProj, qName] = q.split('/', 2);
      return proj === qProj && name.includes(qName ?? '');
    }
    return name.includes(q) || (proj !== '' && proj.includes(q));
  });

  // M2 v1.2 T5.2: group by project for display. Legacy envs (empty
  // project) land under a dedicated `__legacy__` bucket rendered first
  // with a "(legacy)" indicator per ADR D6.
  const LEGACY_KEY = '__legacy__';
  const grouped = filtered.reduce<Record<string, Environment[]>>((acc, env) => {
    const key = env.project && env.project !== '' ? env.project : LEGACY_KEY;
    if (!acc[key]) acc[key] = [];
    acc[key]!.push(env);
    return acc;
  }, {});
  // Stable group order: legacy first (familiar production-legacy entry),
  // then projects alphabetically.
  const groupKeys = Object.keys(grouped).sort((a, b) => {
    if (a === LEGACY_KEY) return -1;
    if (b === LEGACY_KEY) return 1;
    return a.localeCompare(b);
  });

  // 2026-06-07 per-project model: N preview rows, one per project
  // that has had a preview at any time. Search filters by project_id
  // OR by the literal "preview" tag.
  const previews: PreviewSlot[] = data?.previews ?? [];
  const filteredPreviews = previews.filter((p) => {
    if (!searchQuery) return true;
    const q = searchQuery.toLowerCase();
    return (
      'preview'.includes(q) ||
      p.project_id.toLowerCase().includes(q) ||
      (p.active_branch ?? '').toLowerCase().includes(q)
    );
  });
  const showPreviewRows = data?.previews !== undefined && filteredPreviews.length > 0;

  return (
    <PageShell
      title="Environments"
      onTitleChange={setPageTitle}
      search={{
        // T4.1 finding 1.5: discoverable hint for the "name/project"
        // search syntax M2 v1.2 added (e.g. "web/staging" filters to
        // staging envs inside the web project).
        placeholder: 'Search environments... (try project/env, e.g. web/staging)',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'New environment',
        icon: Plus,
        onClick: () => setCreateOpen(true),
      }}
      loading={loading}
      empty={
        // 2026-06-07: page is empty only when BOTH permanent envs +
        // every preview row are absent.
        !loading && filtered.length === 0 && !showPreviewRows
          ? {
              icon: Box,
              title: searchQuery
                ? 'No matching environments'
                : 'No environments yet',
              description: searchQuery
                ? 'Try a different search term.'
                : 'Environments are created automatically per project on first deploy.',
            }
          : undefined
      }
    >
      {/* Plan 2026-06-08-dashboard-screens-review-fixes T1.1 + ADR D1 —
          previous J-10 T2.5 "View Previews" cross-link dropped because
          /projects/:name/previews now redirects back to this page (the
          single preview slot under M2 model is rendered inline below via
          PreviewRow). A self-link would loop and is removed. */}

      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Name</Table.HeaderCell>
                <Table.HeaderCell>
                  Health
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Status
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Protected
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Actions</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {/* 2026-06-07 per-project model: one preview row per
                  project. Old backends omit `previews` → no rows
                  rendered here, legacy permanent rows still render
                  below. */}
              {showPreviewRows &&
                filteredPreviews.map((slot) => (
                  <PreviewRow key={slot.project_id} slot={slot} />
                ))}
              {/* M2 v1.2 T5.2: group envs by project. A group-header row
                  (full-width) precedes each project's envs so the user
                  can scan multi-project tenants quickly. Legacy entries
                  (no project) land under "(legacy)" first. */}
              {groupKeys.map((key) => {
                const envs = grouped[key] ?? [];
                const isLegacy = key === LEGACY_KEY;
                const groupLabel = isLegacy ? 'production (legacy)' : key;
                return (
                  <Fragment key={key}>
                    {/* T4.1 finding 1.4: group header row gets an
                        explicit aria-label so screen readers announce
                        the project context for the following rows.
                        colSpan=5 is still required by the table
                        layout — a11y hint is additive. */}
                    <Table.Row
                      data-testid={`env-group-header-${isLegacy ? 'legacy' : key}`}
                      aria-label={`Project group: ${groupLabel}`}
                      role="rowheader"
                    >
                      <Table.Cell colSpan={5} className="bg-muted/30">
                        <span className="text-label uppercase tracking-wide text-muted-foreground">
                          {isLegacy ? (
                            <>
                              <span className="font-semibold text-foreground">{groupLabel}</span>
                              <span className="ml-2 text-warning">— no project</span>
                            </>
                          ) : (
                            <span className="font-semibold text-foreground">
                              project / <span className="font-mono">{groupLabel}</span>
                            </span>
                          )}
                        </span>
                      </Table.Cell>
                    </Table.Row>
                    {envs.map((env) => (
                      <Table.Row key={`${key}::${env.name}`}>
                        <Table.Cell>
                          <span className="font-mono text-body-sm text-foreground">
                            {isLegacy
                              ? env.name
                              : (
                                <>
                                  <span className="text-muted-foreground">{env.project}/</span>
                                  {env.name}
                                </>
                              )}
                          </span>
                        </Table.Cell>
                        <Table.Cell>
                          <EnvironmentKPIRow
                            replicas={env.replicas}
                            uptime={env.uptime}
                            lastDeploy={env.last_deploy}
                            lastDeploySha={env.last_deploy_sha}
                          />
                        </Table.Cell>
                        <Table.Cell>
                          <StatusDot
                            status={mapStatus(env.status)}
                            label={statusLabel(env.status)}
                          />
                        </Table.Cell>
                        <Table.Cell className="hidden md:table-cell">
                          {env.protected ? (
                            <span className="inline-flex items-center gap-1 text-warning text-label">
                              <Lock className="size-3" aria-hidden="true" />
                              Protected
                            </span>
                          ) : (
                            <span className="text-muted-foreground text-label">—</span>
                          )}
                        </Table.Cell>
                        <Table.Cell align="right">
                          <div className="inline-flex items-center gap-1">
                            {(env.status ?? '').toLowerCase() === 'active' && (
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => {
                                  setPromoteFrom(env.name);
                                  setPromoteOpen(true);
                                }}
                                aria-label={`Promote release from ${env.name}`}
                              >
                                <Rocket className="size-4 text-primary" aria-hidden="true" />
                              </Button>
                            )}
                            {!env.protected && (
                              <Button
                                size="sm"
                                variant="ghost"
                                onClick={() => setDeleteTarget(env)}
                                aria-label={`Delete ${env.name}`}
                              >
                                <Trash2
                                  className="size-4 text-destructive"
                                  aria-hidden="true"
                                />
                              </Button>
                            )}
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))}
                  </Fragment>
                );
              })}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>

      <CreateEnvironmentSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
      />

      {deleteTarget && (
        <DeleteEnvironmentDialog
          open={deleteTarget !== null}
          onOpenChange={(open) => !open && setDeleteTarget(null)}
          envName={deleteTarget.name}
          // M2 v1.2 T5.3: propagate project so DELETE filters by tuple.
          // Empty/undefined project resolves to the production-legacy
          // lookup (ADR D2 backward compat).
          project={deleteTarget.project}
          cascade={{
            // v1.0: placeholder counts. Future: cascade-preview endpoint per EC-12.
            workloads: deleteTarget.projects ?? 0,
          }}
          onDeleted={() => setDeleteTarget(null)}
        />
      )}

      <PromoteDialog
        open={promoteOpen}
        onOpenChange={setPromoteOpen}
        projectName={projectName}
        environments={environments}
        initialFrom={promoteFrom}
        onCreateRequested={() => {
          // Open create sheet without closing promote dialog state.
          setCreateOpen(true);
        }}
        onPromoted={() => {
          setPromoteOpen(false);
          setPromoteFrom(undefined);
        }}
      />
    </PageShell>
  );
}

import { useEffect, useState, type MouseEvent } from 'react';
import { Link, useSearchParams, useNavigate, type NavigateFunction } from 'react-router-dom';
import { ExternalLink, FolderKanban, Plus } from 'lucide-react';
import { Badge, Button, Card, PageShell, StatusDot, Table } from '@loopzero/ui';
import type { StatusKind } from '@loopzero/ui';
import { useSetPageTitle } from '@/hooks/use-page-meta';
import { useRequireAuth } from '@/hooks/use-auth';
import { useProjects } from '@/hooks/use-projects';
import { CreateProjectSheet } from "@/components/sheets/create-project-sheet";
import { EmptyProjectsHero } from '@/components/onboarding/empty-projects-hero';
import { formatDate } from '@/lib/format';
import type { Project } from '@/lib/api/types';

// ProjectRow — T4.4 of plan 2026-06-11-code-review-remediation v1.2
// + ADR D6. Extracted from ProjectsPage to keep the page's CC tractable
// (pre-refactor CC=32 mixed per-row derivation + parent orchestration).
//
// Each row resolves 5 derived signals from one ProjectSummary:
//   - framework      (T5.3 LIVE bug fix 2026-06-08 fallback chain)
//   - appsCount      (apps_count > legacy apps?.length)
//   - projectStatus  (5-value last_deploy_status enum)
//   - statusKind     (mapping to @loopzero/ui StatusKind; queued→building)
//   - envCount       (environment_count > legacy singular environment)
//
// All resolution shapes pinned by the page's existing tests.
function ProjectRow({
  project: p,
  navigate,
}: {
  project: Project;
  navigate: NavigateFunction;
}) {
  // T5.3 LIVE-bug fix (2026-06-08): T2.1 dropped legacy apps[] array
  // from listing response, leaving these fields permanently — / 0 in
  // the rendered table. Backend now populates latest_framework +
  // apps_count via the latest_build LATERAL join (additive completion
  // of T2.1). Legacy `apps?.length` / `apps[0]?.framework` fallback
  // preserved for response shapes that pre-date the additive fields.
  const framework = p.latest_framework || p.apps?.[0]?.framework;
  const appsCount = p.apps_count ?? p.apps?.length ?? 0;
  // T4.2 finding 2.4 + T2.1 + ADR D2: status driven by backend-derived
  // last_deploy_status (5-value enum) instead of the legacy
  // "last_deploy presence" heuristic. FAILED builds no longer render
  // as Idle.
  const projectStatus = p.last_deploy_status ?? 'idle';
  // Map our 5-value enum onto @loopzero/ui's StatusKind (which lacks
  // 'queued'). Queued semantically belongs with `building` (both
  // in-flight, not-yet-live).
  const statusKind: StatusKind =
    projectStatus === 'queued' ? 'building' : projectStatus;
  const statusLabel =
    projectStatus === 'live'
      ? 'Live'
      : projectStatus.charAt(0).toUpperCase() + projectStatus.slice(1);
  // T4.2 finding 2.1 + T2.1: prefer the new server-derived
  // environment_count; fall back to 1 if only the legacy singular
  // `environment` is present.
  const envCount = p.environment_count ?? (p.environment ? 1 : 0);

  return (
    // T4.2 finding 2.3: full-row click navigation. onClick routes to
    // project detail; the "Open" cell button is kept for explicit
    // keyboard-affordance + visual discoverability.
    <Table.Row
      key={p.name}
      onClick={() => navigate(`/projects/${p.name}`)}
      className="cursor-pointer hover:bg-muted/30"
      aria-label={`Open project ${p.name}`}
    >
      <Table.Cell>
        <span className="font-medium text-body-sm text-foreground">
          {p.name}
        </span>
      </Table.Cell>
      <Table.Cell className="hidden md:table-cell">
        {framework ? (
          <Badge variant="outline" className="text-label uppercase">
            {framework}
          </Badge>
        ) : (
          <span className="text-body-sm text-muted-foreground">—</span>
        )}
      </Table.Cell>
      <Table.Cell
        align="right"
        numeric
        className="hidden md:table-cell"
      >
        {appsCount}
      </Table.Cell>
      <Table.Cell className="hidden md:table-cell">
        <span className="font-mono text-label text-muted-foreground tabular-nums">
          {envCount}
        </span>
      </Table.Cell>
      <Table.Cell>
        <StatusDot status={statusKind} label={statusLabel} />
      </Table.Cell>
      <Table.Cell className="hidden md:table-cell">
        {/* T4.2 finding 2.6: <time dateTime> wrap for screen reader
            + machine-readable timestamp. */}
        {p.last_deploy ? (
          <time
            dateTime={p.last_deploy}
            className="text-body-sm text-muted-foreground tabular-nums"
          >
            {formatDate(p.last_deploy, 'relative')}
          </time>
        ) : (
          <span className="text-body-sm text-muted-foreground">—</span>
        )}
      </Table.Cell>
      <Table.Cell
        align="right"
        // Stop propagation so the cell-level "Open" button doesn't
        // double-fire navigation from the row.
        onClick={(e: MouseEvent) => e.stopPropagation()}
      >
        <Button variant="ghost" size="sm" asChild>
          <Link to={`/projects/${p.name}`} aria-label={`Open ${p.name}`}>
            Open
            <ExternalLink className="size-3" aria-hidden="true" />
          </Link>
        </Button>
      </Table.Cell>
    </Table.Row>
  );
}

/**
 * ProjectsPage — entity list.
 *
 * Visual consistency pattern (per 2026-05-26 sponsor directive "all tables
 * must look like /environments"): PageShell + Card + Table primitive,
 * matching environments.tsx style. Was previously DataTable composite +
 * ActionBar; migrated to the simpler primitive so every list page in the
 * dashboard renders the same header chrome.
 *
 * Sorting was a DataTable freebie that the Table primitive doesn't provide;
 * accepted trade-off for visual consistency. Sortable columns can be
 * reintroduced later as an opt-in feature (likely upstream candidate).
 */
export function ProjectsPage() {
  useRequireAuth();
  useSetPageTitle('Projects');
  // T4.5 finding C.4: Load more pagination — mirrors the J-17 pattern
  // from deployments.tsx. PAGE_BUMP=20 matches tenant-tier scale
  // (Starter=3 / Pro=10 / Team=20). Hidden when pageLimit >= total.
  const PAGE_BUMP = 20;
  const [pageLimit, setPageLimit] = useState(PAGE_BUMP);
  const { projects, total, loading } = useProjects({ limit: pageLimit });
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [searchQuery, setSearchQuery] = useState(searchParams.get('q') ?? '');
  const [createOpen, setCreateOpen] = useState(false);

  // Sync ProjectsPage search input with `?q=` query param (sidebar search wire,
  // plan v1.2 T0.5). Updates on URL change so back/forward works.
  useEffect(() => {
    const q = searchParams.get('q') ?? '';
    setSearchQuery(q);
  }, [searchParams]);

  // EC-03 consumer wiring (Sprint 2 T2.3): CommandPalette "Create project"
  // navigates here with ?action=new. Open sheet + auto-clear param so re-renders
  // don't re-trigger.
  useEffect(() => {
    if (searchParams.get('action') === 'new') {
      setCreateOpen(true);
      const next = new URLSearchParams(searchParams);
      next.delete('action');
      setSearchParams(next, { replace: true });
    }
  }, [searchParams, setSearchParams]);

  // T4.2 finding 2.5: sort dropdown. URL-bound via ?sort=. Defaults to
  // name asc (matches the legacy alphabetical ordering).
  type SortKey = 'name' | 'last_deploy_desc' | 'env_count_desc';
  const sortRaw = searchParams.get('sort') ?? 'name';
  const sortKey: SortKey =
    sortRaw === 'last_deploy_desc' || sortRaw === 'env_count_desc'
      ? sortRaw
      : 'name';

  const filteredProjects = projects
    .filter((p) => p.name.toLowerCase().includes(searchQuery.toLowerCase()))
    .slice()
    .sort((a, b) => {
      if (sortKey === 'name') return a.name.localeCompare(b.name);
      if (sortKey === 'env_count_desc') {
        return (b.environment_count ?? 0) - (a.environment_count ?? 0);
      }
      // last_deploy_desc — projects with no deploys go last
      const aTs = a.last_deploy ? Date.parse(a.last_deploy) : 0;
      const bTs = b.last_deploy ? Date.parse(b.last_deploy) : 0;
      return bTs - aTs;
    });

  function setSort(next: SortKey) {
    const params = new URLSearchParams(searchParams);
    if (next === 'name') params.delete('sort');
    else params.set('sort', next);
    setSearchParams(params, { replace: true });
  }

  return (
    <PageShell
      title="Projects"
      search={{
        placeholder: 'Search projects...',
        value: searchQuery,
        onChange: setSearchQuery,
      }}
      primaryAction={{
        label: 'New Project',
        icon: Plus,
        // SG-01 CLOSED (plan v1.0 T4.1, 2026-05-26): real CreateProjectSheet
        // wired to POST /api/v1/projects. Full GitHub picker wizard lives in
        // a future iter — current dialog accepts name + creates the project.
        onClick: () => setCreateOpen(true),
      }}
      loading={loading}
      empty={
        // Only PageShell empty state for SEARCH no-match. For "no projects at all"
        // (Sprint 3 T3.3), render EmptyProjectsHero inline below — empty-state-as-
        // onboarding per ADR D2.
        !loading && searchQuery && filteredProjects.length === 0
          ? {
              icon: FolderKanban,
              title: 'No matching projects',
              description: 'Try a different search term.',
            }
          : undefined
      }
    >
      {!loading && projects.length === 0 ? (
        // Sprint 3 T3.3: empty-state-as-onboarding per ADR D2.
        <EmptyProjectsHero onCreateFromDashboard={() => setCreateOpen(true)} />
      ) : (
      <>
        {/* T4.2 finding 2.5: sort dropdown — URL-bound via ?sort= so
            back/forward and bookmarks preserve sort state. */}
        {filteredProjects.length > 1 && (
          <div className="mb-3 flex items-center justify-end gap-2">
            <label
              htmlFor="projects-sort"
              className="text-label text-muted-foreground"
            >
              Sort
            </label>
            <select
              id="projects-sort"
              data-testid="projects-sort"
              value={sortKey}
              onChange={(e) => setSort(e.target.value as SortKey)}
              className="rounded-md border border-input bg-background px-2 py-1 text-body-sm focus:outline-none focus:ring-2 focus:ring-ring"
            >
              <option value="name">Name (A-Z)</option>
              <option value="last_deploy_desc">Last deploy (newest)</option>
              <option value="env_count_desc">Live envs (most)</option>
            </select>
          </div>
        )}
      <Card>
        <Card.Body className="p-0">
          <Table>
            <Table.Header>
              <Table.Row>
                <Table.HeaderCell>Project</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Framework
                </Table.HeaderCell>
                <Table.HeaderCell
                  align="right"
                  className="hidden md:table-cell"
                >
                  {/* T4.2 finding 2.2: tooltip on Apps column. The
                      number is total deployable apps from loopzero.yaml;
                      operators sometimes wonder if it counts running
                      pods (it doesn't) or env-scoped apps (also no). */}
                  <span title="Total deployable apps declared in loopzero.yaml (not per-env runtime instances).">
                    Apps
                  </span>
                </Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  {/* T4.2 finding 2.1 + T2.1 + ADR D2: replaced singular
                      "Environment" column with "Live envs" count.
                      Post-M2 a project can have N envs per (project, env)
                      tuple; surfacing the count is the canonical signal. */}
                  Live envs
                </Table.HeaderCell>
                <Table.HeaderCell>Status</Table.HeaderCell>
                <Table.HeaderCell className="hidden md:table-cell">
                  Last deploy
                </Table.HeaderCell>
                <Table.HeaderCell align="right">
                  <span className="sr-only">Open project</span>
                </Table.HeaderCell>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {filteredProjects.map((p) => (
                <ProjectRow key={p.name} project={p} navigate={navigate} />
              ))}
            </Table.Body>
          </Table>
        </Card.Body>
      </Card>
      {/* T4.5 finding C.4: Load more affordance — mirrors J-17 pattern.
          Hidden when current page already covers total (Starter+Pro
          tiers usually fit in the default 20-row window). */}
      {total > projects.length && (
        <div className="mt-4 flex items-center justify-between gap-2 text-body-sm text-muted-foreground">
          <span data-testid="projects-showing">
            Showing {projects.length} of {total}
          </span>
          <Button
            variant="ghost"
            size="sm"
            onClick={() =>
              setPageLimit((prev) => Math.min(prev + PAGE_BUMP, total))
            }
            disabled={loading}
            data-testid="projects-load-more"
          >
            {loading ? 'Loading…' : 'Load more'}
          </Button>
        </div>
      )}
      </>
      )}

      <CreateProjectSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(p) => navigate(`/projects/${encodeURIComponent(p.name)}`)}
      />
    </PageShell>
  );
}

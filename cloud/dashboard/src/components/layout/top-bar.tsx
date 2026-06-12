import { useLocation, useNavigate, useParams } from 'react-router-dom';
import { FolderKanban, Search } from 'lucide-react';
import { Select } from '@loopzero/ui';
import { useProjects } from '@/hooks/use-projects';

interface TopBarProps {
  showProjectSelector?: boolean;
  /** Sprint 2 T2.6 — invoked when user clicks the search/command button. */
  onCommandPaletteOpen?: () => void;
}

// EC-13 platform-aware shortcut display
const isMac = typeof navigator !== 'undefined' && /Mac|iPod|iPhone|iPad/.test(navigator.platform);
const SHORTCUT_LABEL = isMac ? '⌘K' : 'Ctrl+K';

/**
 * TopBar — horizontal strip above main content (h-14, border-b).
 *
 * Composition:
 *   left:   project selector Select (hidden on Settings/Profile/Billing)
 *   right:  reserved slot for future quick actions (NotificationBell,
 *           CommandPalette trigger)
 *
 * The pageTitle text was removed 2026-05-26 — it duplicated the heading that
 * PageShell already renders in the page body, and on project-scoped routes
 * it duplicated the value shown by the Select itself. The `useSetPageTitle`
 * hook + PageMetaProvider stay in place for future consumers (e.g., browser
 * tab `document.title` sync).
 *
 * Project selector defaults to the current `:name` route param when present
 * so the dropdown reflects "where you are" instead of resetting to "All".
 *
 * Uses use-projects.ts cache (D-3 v1.1) — same fetch shared with Overview
 * and Projects page without duplicate network calls.
 */
export function TopBar({ showProjectSelector = true, onCommandPaletteOpen }: TopBarProps) {
  const { projects } = useProjects();
  const location = useLocation();
  const navigate = useNavigate();
  const { name: routeProjectName } = useParams<{ name: string }>();

  // Hide selector on routes that don't make project sense
  const hideOnPaths = ['/settings', '/profile', '/billing'];
  const shouldShowSelector =
    showProjectSelector && !hideOnPaths.some((p) => location.pathname.startsWith(p));

  const selectorValue = routeProjectName ?? 'all';

  return (
    <header className="h-14 border-b border-border/60 bg-background/95 backdrop-blur-sm sticky top-0 z-20 flex items-center gap-4 px-6">
      {/* Left slot — project selector (the primary nav widget for project-scoped
       * pages). Placed first per Vercel/Linear convention. */}
      {shouldShowSelector && (
        <Select
          value={selectorValue}
          onValueChange={(value) => {
            if (value === 'all') {
              navigate('/projects');
            } else {
              navigate(`/projects/${value}`);
            }
          }}
        >
          <Select.Trigger
            aria-label="Select project"
            className="w-[200px] gap-2"
          >
            <FolderKanban
              className="size-3.5 text-muted-foreground shrink-0"
              aria-hidden="true"
            />
            <Select.Value placeholder="All Projects" />
          </Select.Trigger>
          <Select.Content className="min-w-[220px]">
            <Select.Item value="all">All Projects</Select.Item>
            {projects.map((p) => (
              <Select.Item
                key={p.name}
                value={p.name}
                className="truncate overflow-hidden"
              >
                {p.name}
              </Select.Item>
            ))}
          </Select.Content>
        </Select>
      )}

      {/* Right slot — Sprint 2 T2.6: CommandPalette trigger button.
       * EC-13: shortcut label adapts to platform (⌘K em Mac, Ctrl+K em Win/Linux).
       * Sized +25% vs original per sponsor feedback (px-3 py-1.5 text-label →
       * px-4 py-2 text-body-sm + larger icon + min-width). Bigger tap target +
       * stronger visual affordance — search é uma surface principal de produto. */}
      <div className="ml-auto">
        {onCommandPaletteOpen && (
          <button
            type="button"
            onClick={onCommandPaletteOpen}
            className="inline-flex items-center gap-2.5 px-4 py-2 min-w-[200px] rounded-md border border-border/40 text-body-sm text-muted-foreground hover:text-foreground hover:border-border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2 focus-visible:ring-offset-background"
            aria-label={`Open command palette (${SHORTCUT_LABEL})`}
          >
            <Search className="size-4" aria-hidden="true" />
            <span className="flex-1 text-left">Search…</span>
            <kbd className="font-mono text-label">{SHORTCUT_LABEL}</kbd>
          </button>
        )}
      </div>
    </header>
  );
}

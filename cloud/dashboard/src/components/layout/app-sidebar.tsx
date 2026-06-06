import { useEffect, useRef, useState, type ElementType } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import {
  Activity,
  ArrowLeft,
  Bell,
  Box,
  BookOpen,
  Brain,
  ChevronRight,
  FolderKanban,
  Github,
  GitPullRequest,
  Globe,
  History,
  Key,
  KeyRound,
  LayoutGrid,
  Link2,
  ListChecks,
  Lock,
  Receipt,
  Rocket,
  ScrollText,
  Search,
  Settings,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  User,
  Users,
  Webhook,
} from 'lucide-react';
import { Input, Sidebar } from '@loopzero/ui';
import type { Session } from '@/lib/auth';
import { TeamSwitcher } from '@/components/team/team-switcher';
import { useOnboardingState } from '@/hooks/use-onboarding-state';
import { reportEvent } from '@/lib/rum';
import { withViewTransition } from '@/lib/view-transitions';

interface AppSidebarProps {
  session: Session;
}

interface MenuItem {
  label: string;
  path: string;
  icon: ElementType;
  /** When set, clicking the item replaces the sidebar contents with the
   * referenced submenu (Vercel drill-down pattern). The route is also
   * navigated so URL stays in sync. */
  drillsInto?: string;
}

interface MenuGroup {
  /** Optional uppercase section title rendered above the group's items
   * (e.g., "Billing", "Developer"). Omit for an untitled cluster — visually
   * still separated from the next group by Sidebar.Section spacing. */
  title?: string;
  items: MenuItem[];
}

interface MenuDefinition {
  id: string;
  /** Title shown in the back-button header of the submenu. */
  title: string;
  parent?: string;
  /** Groups render as separate Sidebar.Section blocks; this is the Vercel
   * pattern of clustering related items under labeled subheadings instead
   * of one flat list. */
  groups: MenuGroup[];
}

/**
 * Menu registry — drill-down sidebar (Vercel pattern, 2026-05-26).
 *
 * `main` is the root; submenus reference a `parent` so the back-button knows
 * where to return. URL pathnames drive which menu is active (no manual stack
 * state — URL is the source of truth), which means browser back/forward also
 * navigates the sidebar correctly.
 *
 * Each menu is broken into `groups`, each rendered as its own Sidebar.Section
 * (with optional title). This preserves the original "Billing" / footer-nav
 * separation that flat lists lose.
 *
 * Adding a new submenu:
 *   1. Add the menu definition here (id, groups, parent='main')
 *   2. Mark the parent item with `drillsInto: '<id>'`
 *   3. Add route patterns in app.tsx so `/<base>/*` resolves
 *   4. (Optional) Extend resolveActiveMenu() if the URL pattern is custom
 */
const MENUS: Record<string, MenuDefinition> = {
  main: {
    id: 'main',
    title: 'LoopZeroCloud',
    groups: [
      {
        items: [
          { label: 'Overview', path: '/', icon: LayoutGrid },
          { label: 'Projects', path: '/projects', icon: FolderKanban },
          { label: 'Environments', path: '/environments', icon: Box },
          { label: 'Domains', path: '/domains', icon: Globe },
          { label: 'Team', path: '/team', icon: Users },
        ],
      },
      {
        // Plan 1 of dashboard-memory-section-roadmap.md — Memory section.
        // Drills into a submenu so future plans (P2 entities, P3 exports/webhooks)
        // can add items without reflowing the root nav.
        title: 'Memory',
        items: [{ label: 'Memory', path: '/memory', icon: Brain, drillsInto: 'memory' }],
      },
      {
        title: 'Billing',
        items: [{ label: 'Billing', path: '/billing', icon: Receipt }],
      },
      {
        title: 'Activity',
        items: [{ label: 'Audit log', path: '/audit', icon: History }],
      },
      {
        items: [
          { label: 'Settings', path: '/settings', icon: Settings, drillsInto: 'settings' },
          { label: 'Profile', path: '/profile', icon: User, drillsInto: 'profile' },
        ],
      },
    ],
  },
  settings: {
    id: 'settings',
    title: 'Settings',
    parent: 'main',
    groups: [
      {
        title: 'Workspace',
        items: [
          { label: 'General', path: '/settings', icon: Settings },
          { label: 'Members', path: '/settings/members', icon: Users },
          { label: 'Billing', path: '/settings/billing', icon: Receipt },
        ],
      },
      {
        title: 'Developer',
        items: [
          { label: 'API Tokens', path: '/settings/tokens', icon: Key },
          { label: 'Webhooks', path: '/settings/webhooks', icon: Webhook },
          { label: 'Notifications', path: '/settings/notifications', icon: Bell },
        ],
      },
      {
        items: [
          { label: 'Danger Zone', path: '/settings/danger', icon: ShieldAlert },
        ],
      },
    ],
  },
  profile: {
    id: 'profile',
    title: 'Profile',
    parent: 'main',
    groups: [
      {
        title: 'Personal',
        items: [
          { label: 'Account', path: '/profile', icon: User },
          { label: 'Security', path: '/profile/security', icon: Lock },
          { label: 'Sessions', path: '/profile/sessions', icon: Link2 },
        ],
      },
      {
        items: [
          { label: 'Danger Zone', path: '/profile/danger', icon: ShieldAlert },
        ],
      },
    ],
  },
  // Memory drill-down — Plan 1 ships Overview + Memories; Plan 2 will add
  // Entities; Plan 3 will add Exports + Webhooks. Items added to this menu in
  // subsequent plans do NOT reflow the root sidebar — that's why we drill.
  memory: {
    id: 'memory',
    title: 'Memory',
    parent: 'main',
    groups: [
      {
        items: [
          { label: 'Overview', path: '/memory', icon: LayoutGrid },
          { label: 'Memories', path: '/memory/memories', icon: ListChecks },
          // Plan 2 — Memory Insights. Plan 3 will add Exports + Webhooks
          // here without reflowing the root sidebar.
          { label: 'Entities', path: '/memory/entities', icon: Users },
          // Plan 3 — Memory Operations.
          { label: 'Exports', path: '/memory/exports', icon: BookOpen },
          { label: 'Webhooks', path: '/memory/webhooks', icon: Webhook },
        ],
      },
    ],
  },
  // Project drill-down — Vercel/Linear pattern. When user enters /projects/<name>
  // (or any /projects/<name>/* sub-route), sidebar swaps to this menu. Items use
  // {project} placeholder, resolved at render time to the active project name
  // from URL. Title is also dynamic (shows the project name).
  project: {
    id: 'project',
    title: '{project}',
    parent: 'main',
    groups: [
      {
        items: [
          { label: 'Overview', path: '/projects/{project}', icon: LayoutGrid },
          { label: 'Deployments', path: '/projects/{project}/deployments', icon: Rocket },
          { label: 'Status', path: '/projects/{project}/status', icon: Activity },
          { label: 'Runtime logs', path: '/projects/{project}/logs', icon: ScrollText },
          { label: 'Scale', path: '/projects/{project}/scale', icon: SlidersHorizontal },
          { label: 'Readiness', path: '/projects/{project}/readiness', icon: ShieldCheck },
        ],
      },
      {
        title: 'Configuration',
        items: [
          { label: 'Secrets', path: '/projects/{project}/secrets', icon: KeyRound },
          { label: 'Previews', path: '/projects/{project}/previews', icon: GitPullRequest },
          { label: 'Repository', path: '/projects/{project}/connect', icon: Github },
        ],
      },
    ],
  },
};

interface ResolvedMenu {
  id: string;
  /** Set when active menu is project-scoped — used to resolve {project} placeholders. */
  projectName?: string;
}

// Reserved project slugs that should NOT trigger drill-down (collide with
// future static routes like /projects/new). Mirrors EC-01 from plan v1.2.
const RESERVED_PROJECT_SLUGS = new Set(['new', 'create', 'admin']);

/** URL → active menu id + context. URL is source of truth (no manual stack state). */
function resolveActiveMenu(pathname: string): ResolvedMenu {
  // Project drill-down: /projects/<name> or /projects/<name>/*
  // Explicitly does NOT match /projects (list page) — only when there's a name segment.
  const projectMatch = pathname.match(/^\/projects\/([^/]+)/);
  if (projectMatch && !RESERVED_PROJECT_SLUGS.has(projectMatch[1])) {
    return { id: 'project', projectName: decodeURIComponent(projectMatch[1]) };
  }
  if (pathname.startsWith('/settings')) return { id: 'settings' };
  if (pathname.startsWith('/profile')) return { id: 'profile' };
  if (pathname.startsWith('/memory')) return { id: 'memory' };
  return { id: 'main' };
}

/** Substitute {project} placeholder in path/title with the active project name. */
function resolvePlaceholders(value: string, projectName?: string): string {
  if (!projectName) return value;
  return value.replace(/\{project\}/g, encodeURIComponent(projectName));
}

/**
 * AppSidebar — drill-down stacked navigation (Vercel pattern).
 *
 * The sidebar contents swap entirely when the user enters a submenu (e.g.,
 * clicks "Settings"). A back button at the top returns to the parent menu.
 * Active menu is derived from URL (no manual state stack), so browser
 * back/forward "just works".
 *
 * Animation: CSS `transform: translateX()` + `transition`. Direction is
 * tracked so push (enter submenu) slides from right, pop (back) slides from
 * left — same affordance as Vercel/Linear.
 *
 * SPA navigation per D-1 v1.2 EC-1: NEVER use as="a" href={path} — produces
 * full page reload that loses JWT in memory. Use `as="button" + navigate()`.
 *
 * Plan tier currently hard-coded to "hobby" (EC-14 v1.1 acknowledged TODO —
 * resolves when BillingPage wires /v1/billing tier).
 */
export function AppSidebar({ session }: AppSidebarProps) {
  const location = useLocation();
  const navigate = useNavigate();
  const [searchValue, setSearchValue] = useState('');
  // Sprint 3 T3.6: Replay onboarding button visible when widget is dismissed.
  const { state: onboardingState, replay: replayOnboarding } = useOnboardingState();

  const resolved = resolveActiveMenu(location.pathname);
  const activeMenuId = resolved.id;
  const activeMenu = MENUS[activeMenuId];
  const parentMenu = activeMenu.parent ? MENUS[activeMenu.parent] : null;
  // Resolve {project} placeholder in menu title (e.g., "{project}" → "acme-web")
  const resolvedTitle = resolvePlaceholders(activeMenu.title, resolved.projectName);

  // Track previous menu id to drive slide direction. Push (entering submenu)
  // slides current panel left + new panel in from right; pop (back) reverses.
  const prevMenuIdRef = useRef(activeMenuId);
  const [slideDirection, setSlideDirection] = useState<'none' | 'push' | 'pop'>('none');
  useEffect(() => {
    const prev = prevMenuIdRef.current;
    if (prev === activeMenuId) return;
    const prevDepth = MENUS[prev]?.parent ? 1 : 0;
    const currentDepth = activeMenu.parent ? 1 : 0;
    setSlideDirection(currentDepth > prevDepth ? 'push' : 'pop');
    prevMenuIdRef.current = activeMenuId;
    // Animation duration matches CSS below (180ms standard motion token)
    const t = setTimeout(() => setSlideDirection('none'), 200);
    return () => clearTimeout(t);
  }, [activeMenuId, activeMenu.parent]);

  const userName =
    session.identity.traits.name ?? session.identity.traits.email;

  const isActive = (path: string) => {
    // Resolve placeholders in the target path (so /projects/{project}/secrets
    // becomes /projects/acme-web/secrets for matching)
    const resolvedPath = resolvePlaceholders(path, resolved.projectName);
    if (resolvedPath === '/') return location.pathname === '/';
    // Submenu items: exact match only (else "General" /settings would also light
    // up when on /settings/members because startsWith would match)
    if (activeMenu.id !== 'main') {
      return location.pathname === resolvedPath;
    }
    return location.pathname.startsWith(resolvedPath);
  };

  // T5.3: View Transitions API wraps navigation — smooth crossfade entre
  // menus em browsers que suportam (Chrome 111+/Safari 18+). Firefox cai
  // no fallback (chamada síncrona, sem animação extra), keyframes CSS
  // existentes continuam funcionando.
  const handleItemClick = (item: MenuItem) => {
    const resolvedPath = resolvePlaceholders(item.path, resolved.projectName);
    withViewTransition(() => navigate(resolvedPath));
  };

  const handleBack = () => {
    if (!parentMenu) return;
    withViewTransition(() => {
      // Pop to parent context. Project drill-down → /projects (list user came
      // from). Settings/profile drill-down → root (no natural "previous page").
      if (activeMenuId === 'project') {
        navigate('/projects');
      } else {
        navigate('/');
      }
    });
  };

  // Panel content (shared between push/pop animations)
  const renderPanel = (menu: MenuDefinition) => (
    <>
      {menu.parent ? (
        <Sidebar.Header className="p-3">
          <button
            type="button"
            onClick={handleBack}
            className="flex items-center gap-2 w-full px-2 py-1.5 rounded-md text-body-sm font-medium text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors"
            aria-label={`Back to ${MENUS[menu.parent].title}`}
          >
            <ArrowLeft className="size-4 shrink-0" aria-hidden="true" />
            <span>Back</span>
          </button>
          <p
            className="px-2 pt-2 text-label-caps uppercase tracking-wider text-muted-foreground truncate"
            title={resolvedTitle}
          >
            {resolvedTitle}
          </p>
        </Sidebar.Header>
      ) : (
        <Sidebar.Header className="p-3">
          <TeamSwitcher fallbackName={userName} />
        </Sidebar.Header>
      )}

      {/* Search only on root menu — submenus don't need project search */}
      {!menu.parent && (
        <Sidebar.Section className="px-3 py-2">
          <div className="relative">
            <Search
              className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 size-3.5 text-muted-foreground"
              aria-hidden="true"
            />
            <Input
              type="search"
              placeholder="Find a project..."
              aria-label="Find a project"
              className="pl-8"
              value={searchValue}
              onChange={(e) => setSearchValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  const q = searchValue.trim();
                  if (q) {
                    navigate(`/projects?q=${encodeURIComponent(q)}`);
                    setSearchValue('');
                  }
                }
              }}
            />
          </div>
        </Sidebar.Section>
      )}

      {/* Render each group as its own Sidebar.Section. Optional `title` shows
       * as uppercase subheading; untitled groups separate visually via section
       * spacing without a label (the "footer-nav" pattern: Settings/Profile
       * clustered apart from main nav without needing a heading). */}
      {menu.groups.map((group, groupIndex) => (
        <Sidebar.Section
          key={`${menu.id}-group-${groupIndex}`}
          title={group.title}
        >
          {group.items.map((item) => {
            const Icon = item.icon;
            return (
              <Sidebar.Item
                key={item.path + ':' + item.label}
                as="button"
                icon={Icon}
                active={isActive(item.path)}
                onClick={() => handleItemClick(item)}
                title={resolvePlaceholders(item.label, resolved.projectName)}
              >
                {/* Single child wrapper — Sidebar.Item composite renders children
                 * as a single block; passing two siblings (label + chevron) let
                 * the composite's internal layout stack them vertically. Wrapping
                 * in one flex container guarantees horizontal placement with the
                 * chevron pinned right via justify-between. */}
                <span className="flex w-full items-center justify-between gap-2">
                  <span className="truncate">{item.label}</span>
                  {item.drillsInto && (
                    <ChevronRight
                      className="size-3.5 shrink-0 text-muted-foreground"
                      aria-hidden="true"
                    />
                  )}
                </span>
              </Sidebar.Item>
            );
          })}
        </Sidebar.Section>
      ))}
    </>
  );

  // Slide-in classes: chosen per direction. CSS transitions defined inline so
  // we don't need a global stylesheet edit for this single component.
  const slideClass =
    slideDirection === 'push'
      ? 'animate-[slidein-right_180ms_ease-out]'
      : slideDirection === 'pop'
        ? 'animate-[slidein-left_180ms_ease-out]'
        : '';

  return (
    <>
      {/* Inline keyframes — kept here (not in index.css) so the StackedSidebar
       * concern is fully colocated. ~10 lines, no global pollution. */}
      <style>{`
        @keyframes slidein-right {
          from { transform: translateX(12%); opacity: 0; }
          to { transform: translateX(0); opacity: 1; }
        }
        @keyframes slidein-left {
          from { transform: translateX(-12%); opacity: 0; }
          to { transform: translateX(0); opacity: 1; }
        }
      `}</style>
      <Sidebar className="w-[220px]">
        <div key={activeMenuId} className={`flex flex-col h-full ${slideClass}`}>
          {renderPanel(activeMenu)}

          <Sidebar.Footer className="mt-auto border-t border-border/60">
            <div className="px-3 py-3 space-y-2">
              <a
                href="https://docs.loopzero.example"
                target="_blank"
                rel="noopener noreferrer"
                className="flex items-center gap-2 text-body-sm text-muted-foreground hover:text-foreground transition-colors whitespace-nowrap"
              >
                <BookOpen className="size-3.5 shrink-0" aria-hidden="true" />
                <span>Documentation</span>
              </a>
              {/* Sprint 3 T3.6: Replay onboarding link — visible only when
                  user dismissed widget. EC-06 ensures replay() works even when
                  all steps complete (resets DEFAULT_STATE). */}
              {onboardingState.dismissed && (
                <button
                  type="button"
                  onClick={() => {
                    replayOnboarding();
                    reportEvent('onboarding_replayed');
                  }}
                  className="flex items-center gap-2 text-body-sm text-muted-foreground hover:text-foreground transition-colors whitespace-nowrap"
                >
                  <BookOpen className="size-3.5 shrink-0" aria-hidden="true" />
                  <span>Replay onboarding</span>
                </button>
              )}
              <div
                className="flex items-center gap-2 text-label text-muted-foreground whitespace-nowrap"
                role="status"
                aria-label="System status: all systems operational"
              >
                <span className="relative flex size-2 shrink-0">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60" />
                  <span className="relative inline-flex size-2 rounded-full bg-success" />
                </span>
                <span>Operational</span>
              </div>
            </div>
          </Sidebar.Footer>
        </div>
      </Sidebar>
    </>
  );
}

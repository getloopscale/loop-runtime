import {
  Activity,
  FolderKanban,
  Globe,
  History,
  KeyRound,
  Plus,
  Receipt,
  Rocket,
  Settings,
  User,
  Users,
} from 'lucide-react';
import type { NavigateFunction } from 'react-router-dom';
import type { CommandItem } from './command-palette';

/**
 * Command registry (Sprint 2 T2.3) — 15 MVP commands.
 *
 * EC-03 fix: openSheet usa URL query param strategy. CommandPalette mount em
 * DashboardLayout (global), Sheets vivem dentro de pages. Cross-page comm:
 *   buildCommands(ctx).action 'Create project' → navigate('/projects?action=new')
 *   pages/projects.tsx reads useSearchParams().get('action') === 'new' → setCreateOpen
 *
 * Auto-clear searchParams após open evita re-trigger em re-renders.
 *
 * EC-11: perform errors são contained no CommandPalette dispatcher (try/catch
 * + reportError). Handlers aqui não precisam try/catch internos.
 */

export interface CommandContext {
  navigate: NavigateFunction;
  /** Top-N recent projects (sort: last_deploy DESC per EC-12). */
  recentProjects: { name: string; last_deploy?: string }[];
}

export function buildCommands(ctx: CommandContext): CommandItem[] {
  const { navigate, recentProjects } = ctx;

  // Navigate (8 commands)
  const navigateCommands: CommandItem[] = [
    { id: 'nav-projects', category: 'navigate', label: 'Projects', icon: FolderKanban, perform: () => navigate('/projects') },
    { id: 'nav-deployments', category: 'navigate', label: 'Deployments', icon: Rocket, keywords: ['builds', 'deploys'], perform: () => navigate('/projects') /* per-project — palette uses project context if any */ },
    { id: 'nav-audit', category: 'navigate', label: 'Audit log', icon: History, perform: () => navigate('/audit') },
    { id: 'nav-team', category: 'navigate', label: 'Team', icon: Users, keywords: ['members', 'invite'], perform: () => navigate('/team') },
    { id: 'nav-domains', category: 'navigate', label: 'Domains', icon: Globe, keywords: ['custom', 'dns'], perform: () => navigate('/domains') },
    { id: 'nav-billing', category: 'navigate', label: 'Billing', icon: Receipt, keywords: ['plan', 'stripe', 'invoice'], perform: () => navigate('/billing') },
    { id: 'nav-settings', category: 'navigate', label: 'Settings', icon: Settings, perform: () => navigate('/settings') },
    { id: 'nav-profile', category: 'navigate', label: 'Profile', icon: User, perform: () => navigate('/profile') },
  ];

  // Action (4 commands) — EC-03 URL query param strategy
  const actionCommands: CommandItem[] = [
    { id: 'act-new-project', category: 'action', label: 'Create project…', icon: Plus, keywords: ['add', 'new'], perform: () => navigate('/projects?action=new') },
    { id: 'act-invite-member', category: 'action', label: 'Invite member…', icon: Plus, keywords: ['add', 'team'], perform: () => navigate('/team?action=invite') },
    { id: 'act-add-domain', category: 'action', label: 'Add domain…', icon: Plus, keywords: ['new', 'custom'], perform: () => navigate('/domains?action=add') },
    { id: 'act-add-secret', category: 'action', label: 'Add secret…', icon: KeyRound, keywords: ['new', 'env'], perform: () => navigate('/projects') /* secrets are per-project; navigates to projects page for selection */ },
  ];

  // Project quick-jump (top 3 most recently deployed) — EC-12 sort
  const projectQuickJumps: CommandItem[] = recentProjects.slice(0, 3).map((p) => ({
    id: `proj-${p.name}`,
    category: 'project',
    label: p.name,
    icon: Activity,
    keywords: ['open', 'project'],
    perform: () => navigate(`/projects/${encodeURIComponent(p.name)}`),
  }));

  return [...navigateCommands, ...actionCommands, ...projectQuickJumps];
}

/**
 * EC-12 sort helper — projects sorted by last_deploy DESC, tiebreaker created_at DESC.
 * Used by CommandPalette consumer to feed recentProjects argument.
 */
export function sortByRecentDeploy<T extends { last_deploy?: string; created_at?: string; name: string }>(
  projects: T[],
): T[] {
  return [...projects].sort((a, b) => {
    const at = a.last_deploy ? new Date(a.last_deploy).getTime() : 0;
    const bt = b.last_deploy ? new Date(b.last_deploy).getTime() : 0;
    if (at !== bt) return bt - at;
    // Tiebreaker: created_at DESC
    const ac = a.created_at ? new Date(a.created_at).getTime() : 0;
    const bc = b.created_at ? new Date(b.created_at).getTime() : 0;
    return bc - ac;
  });
}

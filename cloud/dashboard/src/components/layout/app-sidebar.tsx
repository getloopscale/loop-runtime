import { useLocation, useNavigate } from 'react-router-dom';
import {
  Box,
  FolderKanban,
  Globe,
  LayoutGrid,
  Receipt,
  Search,
  Settings,
  User,
  Users,
} from 'lucide-react';
import { AccountMenu, Input, Sidebar, type PlanTier } from '@loopzero/ui';
import type { Session } from '@/lib/auth';

interface AppSidebarProps {
  session: Session;
}

interface NavItem {
  label: string;
  path: string;
  icon: typeof LayoutGrid;
}

const MAIN_NAV: NavItem[] = [
  { label: 'Overview', path: '/', icon: LayoutGrid },
  { label: 'Projects', path: '/projects', icon: FolderKanban },
  { label: 'Environments', path: '/environments', icon: Box },
  { label: 'Domains', path: '/domains', icon: Globe },
  { label: 'Team', path: '/team', icon: Users },
];

const BILLING_NAV: NavItem[] = [
  { label: 'Billing', path: '/billing', icon: Receipt },
];

const FOOTER_NAV: NavItem[] = [
  { label: 'Settings', path: '/settings', icon: Settings },
  { label: 'Profile', path: '/profile', icon: User },
];

/**
 * AppSidebar — LoopZeroCloud-specific sidebar shell. Uses @loopzero/ui Sidebar
 * primitive composed with AccountMenu (header) + Input (search) + Sidebar.Item
 * navigation rows + Sidebar.Footer (account-related routes).
 *
 * SPA navigation per D-1 v1.2 EC-1: NEVER use as="a" href={path} — produces
 * full page reload that loses JWT in memory. Instead `as="button" onClick={()
 * => navigate(path)}` — react-router SPA navigation preserves auth state.
 *
 * Routes are LoopZeroCloud real (per D1 ADR), NOT mockup Vercel template items
 * (AI Gateway / Firewall / CDN / Speed Insights are NOT implemented).
 *
 * Plan tier currently hard-coded to "hobby" (EC-14 v1.1 acknowledged TODO —
 * resolves when BillingPage wires /v1/billing tier in T-E.3).
 */
export function AppSidebar({ session }: AppSidebarProps) {
  const location = useLocation();
  const navigate = useNavigate();

  const isActive = (path: string) =>
    path === '/' ? location.pathname === '/' : location.pathname.startsWith(path);

  const userName =
    session.identity.traits.name ?? session.identity.traits.email;
  // TODO(T-E.3): replace hard-coded "hobby" with actual plan from /v1/billing
  const planTier: PlanTier = 'hobby';

  const renderItem = (item: NavItem) => (
    <Sidebar.Item
      key={item.path}
      as="button"
      icon={item.icon}
      active={isActive(item.path)}
      onClick={() => navigate(item.path)}
    >
      {item.label}
    </Sidebar.Item>
  );

  return (
    <Sidebar className="w-[220px]">
      <Sidebar.Header className="p-3">
        <AccountMenu
          name={userName}
          plan={planTier}
          onClick={() => {
            /* TODO: open account switcher (future) */
          }}
        />
      </Sidebar.Header>

      <Sidebar.Section className="px-3 py-2">
        <Input
          type="search"
          placeholder="Find a project..."
          aria-label="Find a project"
          className="pl-8"
        />
        <Search
          className="pointer-events-none absolute -mt-7 ml-2 size-3.5 text-muted-foreground"
          aria-hidden="true"
        />
      </Sidebar.Section>

      <Sidebar.Section>{MAIN_NAV.map(renderItem)}</Sidebar.Section>

      <Sidebar.Section title="Billing">{BILLING_NAV.map(renderItem)}</Sidebar.Section>

      <Sidebar.Footer>{FOOTER_NAV.map(renderItem)}</Sidebar.Footer>
    </Sidebar>
  );
}

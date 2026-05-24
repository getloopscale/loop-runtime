import { NavLink } from 'react-router-dom';
import { motion } from 'framer-motion';
import {
  LayoutDashboard,
  FolderKanban,
  Globe,
  Link2,
  Users,
  CreditCard,
  Settings,
  Terminal,
  Zap,
  User,
  LogOut,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { logout } from '@/lib/auth';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Overview' },
  { to: '/projects', icon: FolderKanban, label: 'Projects' },
  { to: '/environments', icon: Globe, label: 'Environments' },
  { to: '/domains', icon: Link2, label: 'Domains' },
  { to: '/team', icon: Users, label: 'Team' },
  { to: '/billing', icon: CreditCard, label: 'Billing' },
  { to: '/settings', icon: Settings, label: 'Settings' },
  { to: '/profile', icon: User, label: 'Profile' },
];

function UserFooter() {
  const email = ''; // Will be populated by session in future

  return (
    <div className="mx-3 mb-4 flex items-center justify-between rounded-lg border border-white/6 bg-surface-1 px-3 py-2.5">
      <div className="flex items-center gap-2 min-w-0">
        <div className="flex h-6 w-6 items-center justify-center rounded-full bg-accent/10 shrink-0">
          <User className="h-3 w-3 text-accent/60" />
        </div>
        <span className="text-xs text-white/40 truncate">{email || 'User'}</span>
      </div>
      <button onClick={logout} className="p-1 text-white/20 hover:text-white/50 transition-colors" title="Sign out">
        <LogOut className="h-3.5 w-3.5" />
      </button>
    </div>
  );
}

export function Sidebar() {
  return (
    <aside className="fixed left-0 top-0 z-40 flex h-screen w-[220px] flex-col border-r border-white/6 bg-surface-0">
      {/* Logo */}
      <div className="flex items-center gap-2.5 px-5 py-5">
        <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent/10 border border-accent/20">
          <Zap className="h-4 w-4 text-accent" />
        </div>
        <span className="font-display text-lg font-bold tracking-tight text-white">
          LoopZero
        </span>
      </div>

      {/* Nav */}
      <nav className="flex-1 space-y-0.5 px-3 pt-2">
        {navItems.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            end={item.to === '/'}
            className={({ isActive }) =>
              cn(
                'group relative flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-all duration-200',
                isActive
                  ? 'text-white bg-surface-3'
                  : 'text-white/40 hover:text-white/70 hover:bg-surface-2'
              )
            }
          >
            {({ isActive }) => (
              <>
                {isActive && (
                  <motion.div
                    layoutId="sidebar-indicator"
                    className="absolute left-0 top-1/2 -translate-y-1/2 h-5 w-[3px] rounded-full bg-accent"
                    transition={{ type: 'spring', stiffness: 500, damping: 35 }}
                  />
                )}
                <item.icon className={cn('h-4 w-4 transition-colors', isActive ? 'text-accent' : 'text-white/30 group-hover:text-white/50')} />
                <span>{item.label}</span>
              </>
            )}
          </NavLink>
        ))}
      </nav>

      {/* CLI hint */}
      <div className="mx-3 mb-3 rounded-lg border border-white/6 bg-surface-1 p-3">
        <div className="flex items-center gap-2 text-xs text-white/30">
          <Terminal className="h-3.5 w-3.5" />
          <span className="font-mono">loopzero deploy</span>
        </div>
        <p className="mt-1.5 text-[11px] text-white/20 leading-relaxed">
          CLI-first. Deploy from your terminal.
        </p>
      </div>

      {/* User + Logout */}
      <UserFooter />
    </aside>
  );
}

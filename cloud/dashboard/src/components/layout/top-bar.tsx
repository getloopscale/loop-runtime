import { useLocation, useNavigate } from 'react-router-dom';
import { Select } from '@loopzero/ui';
import { usePageMeta } from '@/hooks/use-page-meta';
import { useProjects } from '@/hooks/use-projects';

interface TopBarProps {
  showProjectSelector?: boolean;
}

/**
 * TopBar — horizontal strip above main content (h-14, border-b).
 *
 * Composition (Pattern 3 of dashboard design language):
 *   left:   project switcher Select (hidden on Settings/Profile/Billing
 *           via showProjectSelector={false})
 *   center: page title from usePageMeta() (set per page via useSetPageTitle)
 *   right:  reserved for future dropdown menu (per .llms.txt — Radix bundled)
 *
 * Uses use-projects.ts cache (D-3 v1.1) — same fetch shared with Overview
 * and Projects page without duplicate network calls.
 */
export function TopBar({ showProjectSelector = true }: TopBarProps) {
  const { pageTitle } = usePageMeta();
  const { projects } = useProjects();
  const location = useLocation();
  const navigate = useNavigate();

  // Hide selector on routes that don't make project sense
  const hideOnPaths = ['/settings', '/profile', '/billing'];
  const shouldShowSelector =
    showProjectSelector && !hideOnPaths.some((p) => location.pathname.startsWith(p));

  return (
    <header className="h-14 border-b border-border/40 flex items-center justify-between px-6">
      {shouldShowSelector ? (
        <Select
          defaultValue="all"
          onValueChange={(value) => {
            if (value === 'all') {
              navigate('/projects');
            } else {
              navigate(`/projects/${value}`);
            }
          }}
        >
          <Select.Trigger aria-label="Select project" className="w-48">
            <Select.Value placeholder="All Projects" />
          </Select.Trigger>
          <Select.Content>
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
      ) : (
        <div />
      )}

      {pageTitle && (
        <span className="text-body-sm font-medium text-foreground">{pageTitle}</span>
      )}

      {/* Right slot reserved for future DropdownMenu (Pattern 3) */}
      <div />
    </header>
  );
}

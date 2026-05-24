/* STUB — replaced by full implementation in T-C.1 (plan v1.2). */
import type { Session } from '@/lib/auth';

interface AppSidebarProps {
  session: Session;
}

export function AppSidebar(_props: AppSidebarProps) {
  return (
    <aside className="w-[220px] border-r border-border/40 bg-card text-muted-foreground p-3 text-body-sm">
      <p className="font-mono">[AppSidebar stub — T-C.1 pending]</p>
    </aside>
  );
}

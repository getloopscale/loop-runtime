/* STUB — replaced by full implementation in T-C.2 (plan v1.2). */
import { usePageMeta } from '@/hooks/use-page-meta';

export function TopBar() {
  const { pageTitle } = usePageMeta();
  return (
    <header className="h-14 border-b border-border/40 flex items-center justify-between px-6 text-body-sm">
      <span className="text-muted-foreground font-mono">[TopBar stub — T-C.2 pending]</span>
      <span className="font-medium text-foreground">{pageTitle}</span>
      <span />
    </header>
  );
}

import type { ReactNode } from 'react';
import { cn } from '@loopzero/ui';

export type GridColumns = '1' | '2-equal' | '2-asymmetric' | '3-equal';

interface ContentGridProps {
  columns: GridColumns;
  children: ReactNode;
  gap?: 4 | 6 | 8;
  className?: string;
}

const COLUMN_CLASSES: Record<GridColumns, string> = {
  '1': 'grid grid-cols-1',
  '2-equal': 'grid grid-cols-1 lg:grid-cols-2',
  '2-asymmetric': 'grid grid-cols-1 lg:grid-cols-[1.2fr_1fr]',
  '3-equal': 'grid grid-cols-1 md:grid-cols-3',
};

const GAP_CLASSES: Record<NonNullable<ContentGridProps['gap']>, string> = {
  4: 'gap-4',
  6: 'gap-6',
  8: 'gap-8',
};

/**
 * ContentGrid — page content grid wrapper. Mobile-first: single column
 * below lg breakpoint, then unfolds per `columns` variant.
 *
 * Pattern 5 of dashboard design language. Mockup default = 2-asymmetric
 * (1.2fr : 1fr) for Overview page.
 */
export function ContentGrid({
  columns,
  children,
  gap = 6,
  className,
}: ContentGridProps) {
  return (
    <div className={cn(COLUMN_CLASSES[columns], GAP_CLASSES[gap], className)}>
      {children}
    </div>
  );
}

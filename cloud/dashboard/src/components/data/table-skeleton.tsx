import { Skeleton } from '@loopzero/ui';

/**
 * TableSkeleton (T0.3) — table-shaped skeleton composite atop @loopzero/ui
 * Skeleton primitive. Substitui spinner genérico em PageShell.loading com
 * layout-matching skeleton per sponsor critério #14 ("skeletons devem
 * representar o layout real").
 *
 * EC-10 documented: width via percent + gap-3 — approximation visual, não
 * pixel-perfect alignment com real Table primitive. User não compara pixel
 * a pixel.
 */

interface TableSkeletonProps {
  rows?: number;
  columns?: number;
}

export function TableSkeleton({ rows = 5, columns = 4 }: TableSkeletonProps) {
  return (
    <div
      className="space-y-2 p-4"
      role="status"
      aria-live="polite"
      aria-label="Loading table"
    >
      {/* Header row — slightly taller bars */}
      <div className="flex gap-3 pb-2 border-b border-border/40">
        {Array.from({ length: columns }).map((_, i) => (
          <Skeleton
            key={`h-${i}`}
            aria-live="off"
            aria-hidden="true"
            className="h-3"
            style={{ width: `${100 / columns}%` }}
          />
        ))}
      </div>
      {/* Body rows */}
      {Array.from({ length: rows }).map((_, r) => (
        <div key={`r-${r}`} className="flex gap-3 py-2">
          {Array.from({ length: columns }).map((_, c) => (
            <Skeleton
              key={`c-${r}-${c}`}
              aria-live="off"
              aria-hidden="true"
              className="h-4"
              style={{ width: `${100 / columns}%` }}
            />
          ))}
        </div>
      ))}
    </div>
  );
}

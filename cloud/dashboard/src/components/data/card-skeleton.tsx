import { Skeleton } from '@loopzero/ui';

/**
 * CardSkeleton (T0.3) — card-shaped skeleton via @loopzero/ui Skeleton.
 * Mirrors Card title + body lines + optional footer.
 */

interface CardSkeletonProps {
  withFooter?: boolean;
}

export function CardSkeleton({ withFooter = false }: CardSkeletonProps) {
  return (
    <div
      className="rounded-md border border-border/40 p-4 space-y-3"
      role="status"
      aria-live="polite"
      aria-label="Loading card"
    >
      <Skeleton aria-live="off" aria-hidden="true" className="h-4 w-1/3" />
      <Skeleton aria-live="off" aria-hidden="true" className="h-3 w-full" />
      <Skeleton aria-live="off" aria-hidden="true" className="h-3 w-4/5" />
      {withFooter ? (
        <div className="pt-2 border-t border-border/30">
          <Skeleton aria-live="off" aria-hidden="true" className="h-8 w-24" />
        </div>
      ) : null}
    </div>
  );
}

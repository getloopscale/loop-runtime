import { RefreshCw } from 'lucide-react';
import { Button } from '@loopzero/ui';

/**
 * RefreshButton — Plan J-18 T3.4.
 *
 * Manual refresh affordance per ADR D4 (J-17 lesson #5): SSE realtime does
 * NOT eliminate the need for Refresh. Operator clicks when suspecting drift,
 * stale data, or wanting to force a snapshot.
 *
 * Renders a tertiary button with RefreshCw icon. When `isFetching` is true,
 * the icon spins via `animate-spin` and the button is disabled.
 *
 * Reuse pattern from J-17 era — consistent affordance across deployments,
 * status, secrets, environments pages.
 */
export interface RefreshButtonProps {
  /** True when the underlying query is fetching (TanStack `isFetching`). */
  isFetching: boolean;
  /** Click handler — typically invokes query.refetch(). */
  onRefresh: () => void;
  /** Accessibility label; default "Refresh". */
  label?: string;
  /** Optional className passthrough for layout. */
  className?: string;
}

export function RefreshButton({
  isFetching,
  onRefresh,
  label = 'Refresh',
  className,
}: RefreshButtonProps) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="sm"
      onClick={onRefresh}
      disabled={isFetching}
      aria-label={label}
      className={className}
    >
      <RefreshCw
        aria-hidden="true"
        className={`size-4 ${isFetching ? 'animate-spin' : ''}`}
      />
      <span>{label}</span>
    </Button>
  );
}

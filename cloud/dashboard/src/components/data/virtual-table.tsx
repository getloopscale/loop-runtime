import { useRef, type ReactNode } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';

/**
 * T4.1: VirtualTable composite — renders only visible rows + buffer using
 * @tanstack/react-virtual. Required for >500-row tables (audit log can grow
 * unbounded). Below threshold, regular Table primitive is simpler.
 *
 * Design tradeoffs (vs @loopzerokit/ui Table):
 * - Table's sticky header is replicated as a separate non-virtualized
 *   element at top (kept outside scroll container).
 * - Row height is FIXED (default 56px) — required by virtualizer for
 *   estimateSize. Variable height would require ResizeObserver per row.
 * - Header cells + body cells must share column-width strategy (CSS grid
 *   or fixed width). VirtualTable uses CSS grid via columns prop.
 *
 * Honest scope (Phase 4 acceptance "renders 10000 rows"):
 * - Tested for renders-without-throw and visible-row-count math.
 * - 10K stress is documented as production-validate (no 10K fixture today).
 */

export interface VirtualTableColumn<T> {
  key: string;
  header: ReactNode;
  /** Renders the cell for a given row. */
  cell: (row: T) => ReactNode;
  /** CSS grid template fragment (e.g., '1fr', '200px', 'minmax(120px, 1fr)'). */
  width: string;
  align?: 'left' | 'right' | 'center';
}

export interface VirtualTableProps<T> {
  rows: T[];
  columns: VirtualTableColumn<T>[];
  /** Stable key per row (avoid index — breaks virtualizer reuse on filter changes). */
  rowKey: (row: T) => string;
  /** Fixed row height in pixels. Default 56px (matches LoopZero Table density). */
  rowHeight?: number;
  /** Pre-render N rows above/below viewport for smooth scroll. */
  overscan?: number;
  /** Max scroll container height (default 600px so it fits typical card body). */
  maxHeight?: number;
}

export function VirtualTable<T>({
  rows,
  columns,
  rowKey,
  rowHeight = 56,
  overscan = 5,
  maxHeight = 600,
}: VirtualTableProps<T>) {
  const parentRef = useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => rowHeight,
    overscan,
  });

  const gridTemplate = columns.map((c) => c.width).join(' ');

  return (
    <div className="border border-border/40 rounded-md overflow-hidden">
      {/* Sticky header outside scroll container */}
      <div
        className="grid border-b border-border/40 bg-muted/30 text-label uppercase text-muted-foreground"
        style={{ gridTemplateColumns: gridTemplate }}
        role="row"
      >
        {columns.map((col) => (
          <div
            key={col.key}
            className={`px-3 py-2 ${
              col.align === 'right' ? 'text-right' : col.align === 'center' ? 'text-center' : 'text-left'
            }`}
            role="columnheader"
          >
            {col.header}
          </div>
        ))}
      </div>

      {/* Virtualized scroll container */}
      <div
        ref={parentRef}
        className="overflow-auto"
        style={{ maxHeight }}
        role="rowgroup"
      >
        <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
          {virtualizer.getVirtualItems().map((vRow) => {
            const row = rows[vRow.index];
            return (
              <div
                key={rowKey(row)}
                className="grid border-b border-border/20 hover:bg-muted/20 text-body-sm"
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  right: 0,
                  transform: `translateY(${vRow.start}px)`,
                  height: rowHeight,
                  gridTemplateColumns: gridTemplate,
                }}
                role="row"
              >
                {columns.map((col) => (
                  <div
                    key={col.key}
                    className={`px-3 flex items-center ${
                      col.align === 'right' ? 'justify-end' : col.align === 'center' ? 'justify-center' : 'justify-start'
                    }`}
                    role="cell"
                  >
                    {col.cell(row)}
                  </div>
                ))}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

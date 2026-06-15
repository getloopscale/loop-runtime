import { CircleCheck, CircleX, HelpCircle, TriangleAlert } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';

/**
 * J-21 Readiness — ADR D3 canonical icon + accessible-text mapping (EC-3
 * absorption). Single source of truth so the Checklist row, ScoreCard
 * legend, and FAQ row all render the same icon + the same screen-reader
 * announcement for each status.
 *
 * WCAG 1.4.1 (Use of Color) requires that color is not the SOLE conveyor
 * of meaning. Each status pairs a distinct color CLASS with a distinct
 * icon SHAPE and a distinct aria-label STRING — three orthogonal signals.
 */

export type ReadinessStatus = 'pass' | 'warn' | 'fail' | 'unknown';

export interface StatusVisual {
  /** lucide-react icon component to render. */
  icon: LucideIcon;
  /** Tailwind class for the icon's color. */
  colorClass: string;
  /** aria-label string read by screen readers. */
  accessibleText: string;
}

/**
 * Canonical mapping. Locked by ADR D3 of plan v1.1 — adding a new status
 * (e.g. "stale") requires extending this object AND the ReadinessStatus
 * union type above; both downstream consumers (Checklist + ScoreCard
 * legend) reach through this map and will compile-error on missing entries.
 */
export const STATUS_VISUALS: Record<ReadinessStatus, StatusVisual> = {
  pass: {
    icon: CircleCheck,
    colorClass: 'text-success',
    accessibleText: 'Ready',
  },
  warn: {
    icon: TriangleAlert,
    colorClass: 'text-warning',
    accessibleText: 'Needs attention',
  },
  fail: {
    icon: CircleX,
    colorClass: 'text-destructive',
    accessibleText: 'Action required',
  },
  unknown: {
    icon: HelpCircle,
    colorClass: 'text-muted-foreground',
    accessibleText: 'Not measurable right now',
  },
};

import { Filter, Search, type LucideIcon } from 'lucide-react';
import type { ReactNode } from 'react';
import { Button, Input, cn } from '@loopzero/ui';

interface PrimaryAction {
  label: ReactNode;
  onClick: () => void;
  icon?: LucideIcon;
}

interface ActionBarProps {
  searchPlaceholder?: string;
  searchValue?: string;
  onSearchChange?: (v: string) => void;
  onFilterClick?: () => void;
  primaryAction?: PrimaryAction;
  className?: string;
}

/**
 * ActionBar — page-level action strip. Pattern: search input + optional
 * filter button + optional primary CTA (right-aligned).
 *
 * Composition (Pattern 4 of dashboard design language):
 *   [🔍 Search...] [⚙ filter] ··· [+ Add New ▾]
 *
 * Per design-language doc, mt-6 mb-8 default; pages can override via className.
 */
export function ActionBar({
  searchPlaceholder = 'Search...',
  searchValue,
  onSearchChange,
  onFilterClick,
  primaryAction,
  className,
}: ActionBarProps) {
  const PrimaryIcon = primaryAction?.icon;
  return (
    <div className={cn('flex items-center gap-2 mt-6 mb-8', className)}>
      <div className="flex-1 relative">
        <Search
          className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground pointer-events-none"
          aria-hidden="true"
        />
        <Input
          type="search"
          placeholder={searchPlaceholder}
          value={searchValue ?? ''}
          onChange={(e) => onSearchChange?.(e.target.value)}
          className="pl-9"
        />
      </div>
      {onFilterClick && (
        <Button
          variant="secondary"
          size="icon"
          onClick={onFilterClick}
          aria-label="Open filters"
        >
          <Filter className="size-4" aria-hidden="true" />
        </Button>
      )}
      {primaryAction && (
        <Button variant="primary" onClick={primaryAction.onClick} className="gap-1.5">
          {PrimaryIcon && <PrimaryIcon className="size-4" aria-hidden="true" />}
          {primaryAction.label}
        </Button>
      )}
    </div>
  );
}

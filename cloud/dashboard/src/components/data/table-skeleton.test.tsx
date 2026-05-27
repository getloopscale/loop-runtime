import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import { TableSkeleton } from './table-skeleton';
import { CardSkeleton } from './card-skeleton';

describe('TableSkeleton (T0.3)', () => {
  it('test_renders_default_5_rows_4_columns', () => {
    const { container } = render(<TableSkeleton />);
    // Header row (1) + 5 body rows = 6 row containers
    const rowContainers = container.querySelectorAll('[role="status"] > div');
    expect(rowContainers.length).toBeGreaterThanOrEqual(6);
  });

  it('test_renders_custom_rows_columns', () => {
    const { container } = render(<TableSkeleton rows={2} columns={3} />);
    const rowContainers = container.querySelectorAll('[role="status"] > div');
    // Header + 2 body = 3
    expect(rowContainers.length).toBe(3);
  });

  it('test_has_status_role_for_a11y', () => {
    render(<TableSkeleton />);
    expect(screen.getByRole('status', { name: /loading table/i })).toBeInTheDocument();
  });
});

describe('CardSkeleton (T0.3)', () => {
  it('test_renders_card_shape', () => {
    render(<CardSkeleton />);
    expect(screen.getByRole('status', { name: /loading card/i })).toBeInTheDocument();
  });

  it('test_optional_footer', () => {
    const { container, rerender } = render(<CardSkeleton withFooter={false} />);
    const withoutFooter = container.querySelectorAll('div').length;
    rerender(<CardSkeleton withFooter={true} />);
    const withFooter = container.querySelectorAll('div').length;
    expect(withFooter).toBeGreaterThan(withoutFooter);
  });
});

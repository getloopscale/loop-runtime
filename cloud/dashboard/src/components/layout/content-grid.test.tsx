import { describe, it, expect } from 'vitest';
import { render, screen } from '@/test/render';
import { ContentGrid } from './content-grid';

/**
 * T-B.3 ContentGrid tests (3 planned in plan v1.2 TDD section).
 */
describe('ContentGrid', () => {
  it('test_content_grid_one_col_applies_grid_cols_1', () => {
    const { container } = render(
      <ContentGrid columns="1">
        <div>A</div>
      </ContentGrid>,
    );
    const grid = container.firstChild as HTMLElement;
    expect(grid.className).toMatch(/grid-cols-1/);
  });

  it('test_content_grid_two_asymmetric_applies_responsive_grid', () => {
    const { container } = render(
      <ContentGrid columns="2-asymmetric">
        <div>L</div>
        <div>R</div>
      </ContentGrid>,
    );
    const grid = container.firstChild as HTMLElement;
    expect(grid.className).toMatch(/lg:grid-cols-\[1\.2fr_1fr\]/);
  });

  it('test_content_grid_renders_children_in_order', () => {
    render(
      <ContentGrid columns="2-equal">
        <div data-testid="first">First</div>
        <div data-testid="second">Second</div>
      </ContentGrid>,
    );
    const first = screen.getByTestId('first');
    const second = screen.getByTestId('second');
    expect(first.compareDocumentPosition(second)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);
  });
});

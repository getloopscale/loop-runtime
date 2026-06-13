import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DroppedLinesIndicator } from './dropped-lines-indicator';

describe('DroppedLinesIndicator (J-19 iter 15 productive-idle — dedicated unit tests for extracted indicator)', () => {
  it('test_dropped_lines_indicator_renders_singular_for_one', () => {
    render(<DroppedLinesIndicator count={1} />);
    expect(screen.getByText('1 older line dropped from buffer')).toBeInTheDocument();
  });

  it('test_dropped_lines_indicator_renders_plural_for_many', () => {
    render(<DroppedLinesIndicator count={42} />);
    expect(screen.getByText('42 older lines dropped from buffer')).toBeInTheDocument();
  });

  it('test_dropped_lines_indicator_hides_when_count_zero', () => {
    const { container } = render(<DroppedLinesIndicator count={0} />);
    expect(container.firstChild).toBeNull();
  });

  it('test_dropped_lines_indicator_hides_when_count_negative', () => {
    // Defensive: negative count should not render (invariant invariably positive)
    const { container } = render(<DroppedLinesIndicator count={-1} />);
    expect(container.firstChild).toBeNull();
  });

  it('test_dropped_lines_indicator_has_role_status_and_aria_live', () => {
    render(<DroppedLinesIndicator count={5} />);
    const indicator = screen.getByRole('status');
    expect(indicator).toBeInTheDocument();
    expect(indicator.getAttribute('aria-live')).toBe('polite');
  });

  it('test_dropped_lines_indicator_renders_very_large_count', () => {
    render(<DroppedLinesIndicator count={1_000_000} />);
    expect(screen.getByText('1000000 older lines dropped from buffer')).toBeInTheDocument();
  });
});

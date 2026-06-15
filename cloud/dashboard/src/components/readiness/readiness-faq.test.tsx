import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ReadinessFAQ } from './readiness-faq';

describe('ReadinessFAQ', () => {
  it('test_faq_renders_8_collapsed_rows_and_expands_on_click', () => {
    render(<ReadinessFAQ />);
    const rows = screen.getAllByRole('button');
    expect(rows.length).toBe(8);
    // All rows are collapsed initially.
    expect(rows.every((b) => b.getAttribute('aria-expanded') === 'false')).toBe(true);
    // Click the first row → aria-expanded flips.
    fireEvent.click(rows[0]);
    expect(rows[0].getAttribute('aria-expanded')).toBe('true');
  });

  it('test_faq_defaultOpen_expands_named_row (T3.2 deep-link)', () => {
    render(<ReadinessFAQ defaultOpen="hpa_configured" />);
    const hpaButton = screen
      .getAllByRole('button')
      .find((b) => b.textContent?.includes('Autoscaling configured'));
    expect(hpaButton?.getAttribute('aria-expanded')).toBe('true');
  });
});

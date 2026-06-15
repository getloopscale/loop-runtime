/**
 * refresh-button.test.tsx — vitest hermetic tests for T3.4 J-18.
 *
 * Asserts:
 *  - Renders label text + RefreshCw icon
 *  - animate-spin class present when isFetching=true
 *  - Click invokes onRefresh callback
 *  - Disabled when isFetching=true
 *  - Custom label propagates to aria-label
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

import { RefreshButton } from './refresh-button';

describe('RefreshButton — T3.4 J-18', () => {
  it('renders label text', () => {
    render(<RefreshButton isFetching={false} onRefresh={() => {}} />);
    expect(screen.getByText('Refresh')).toBeInTheDocument();
  });

  it('does NOT have animate-spin class when isFetching=false', () => {
    const { container } = render(<RefreshButton isFetching={false} onRefresh={() => {}} />);
    const icon = container.querySelector('svg');
    expect(icon).toBeInTheDocument();
    expect(icon!.classList.contains('animate-spin')).toBe(false);
  });

  it('has animate-spin class when isFetching=true', () => {
    const { container } = render(<RefreshButton isFetching={true} onRefresh={() => {}} />);
    const icon = container.querySelector('svg');
    expect(icon).toBeInTheDocument();
    expect(icon!.classList.contains('animate-spin')).toBe(true);
  });

  it('invokes onRefresh when clicked', () => {
    const onRefresh = vi.fn();
    render(<RefreshButton isFetching={false} onRefresh={onRefresh} />);
    fireEvent.click(screen.getByRole('button'));
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it('is disabled when isFetching=true', () => {
    render(<RefreshButton isFetching={true} onRefresh={() => {}} />);
    expect(screen.getByRole('button')).toBeDisabled();
  });

  it('does NOT invoke onRefresh when disabled', () => {
    const onRefresh = vi.fn();
    render(<RefreshButton isFetching={true} onRefresh={onRefresh} />);
    fireEvent.click(screen.getByRole('button'));
    expect(onRefresh).not.toHaveBeenCalled();
  });

  it('custom label propagates to aria-label', () => {
    render(
      <RefreshButton isFetching={false} onRefresh={() => {}} label="Recarregar dados" />,
    );
    expect(screen.getByRole('button', { name: 'Recarregar dados' })).toBeInTheDocument();
  });
});

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { ReactElement } from 'react';
import { render, screen } from '@/test/render';
import { userEvent } from '@testing-library/user-event';

const mockReportError = vi.fn();
vi.mock('@/lib/error-reporter', () => ({
  reportError: (error: Error, info: { componentStack?: string; routePath?: string }) =>
    mockReportError(error, info),
}));

import { SheetBoundary } from './sheet-boundary';

// Annotated return ReactElement (never reached) so TSC sees Bomb as a valid
// JSX component despite the unconditional throw.
function Bomb(): ReactElement {
  throw new Error('sheet form crashed');
}

describe('SheetBoundary', () => {
  const originalConsoleError = console.error;

  beforeEach(() => {
    mockReportError.mockReset();
    console.error = vi.fn();
  });

  afterEach(() => {
    console.error = originalConsoleError;
  });

  it('test_renders_inline_fallback_with_close_action', () => {
    const onClose = vi.fn();
    render(
      <SheetBoundary onClose={onClose}>
        <Bomb />
      </SheetBoundary>,
    );
    // Specific match — "This form crashed." headline (not the descriptive
    // line that also contains "form").
    expect(screen.getByText('This form crashed.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /close/i })).toBeInTheDocument();
  });

  it('test_close_button_invokes_onClose_handler', async () => {
    const onClose = vi.fn();
    render(
      <SheetBoundary onClose={onClose}>
        <Bomb />
      </SheetBoundary>,
    );
    await userEvent.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('test_reports_error_with_sheet_context', () => {
    const onClose = vi.fn();
    render(
      <SheetBoundary onClose={onClose} sheetName="create-project">
        <Bomb />
      </SheetBoundary>,
    );
    expect(mockReportError).toHaveBeenCalledTimes(1);
    const [errArg, infoArg] = mockReportError.mock.calls[0];
    expect(errArg.message).toBe('sheet form crashed');
    expect(infoArg.routePath).toBe('sheet:create-project');
  });
});

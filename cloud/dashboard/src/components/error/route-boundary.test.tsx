import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@/test/render';
import { userEvent } from '@testing-library/user-event';

const mockReportError = vi.fn();
vi.mock('@/lib/error-reporter', () => ({
  reportError: (error: Error, info: { componentStack?: string; routePath?: string }) =>
    mockReportError(error, info),
}));

import { RouteBoundary } from './route-boundary';

function Bomb({ shouldThrow = true }: { shouldThrow?: boolean }) {
  if (shouldThrow) throw new Error('boom in render');
  return <div>safe child</div>;
}

describe('RouteBoundary', () => {
  const originalConsoleError = console.error;

  beforeEach(() => {
    mockReportError.mockReset();
    // Silence React 19 dev-only error logging during throw — boundary catches it,
    // but React still logs to console.error in dev StrictMode. EC-13 risk acknowledged.
    console.error = vi.fn();
  });

  afterEach(() => {
    console.error = originalConsoleError;
  });

  it('test_renders_children_when_no_error', () => {
    render(
      <RouteBoundary routePath="/foo">
        <Bomb shouldThrow={false} />
      </RouteBoundary>,
    );
    expect(screen.getByText('safe child')).toBeInTheDocument();
  });

  it('test_renders_fallback_on_throw', () => {
    render(
      <RouteBoundary routePath="/foo">
        <Bomb />
      </RouteBoundary>,
    );
    expect(screen.getByText('This page crashed')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
  });

  it('test_retry_button_resets_boundary', async () => {
    let throwFlag = true;
    function Toggle() {
      if (throwFlag) throw new Error('toggle boom');
      return <div>recovered</div>;
    }
    render(
      <RouteBoundary routePath="/foo">
        <Toggle />
      </RouteBoundary>,
    );
    expect(screen.getByText('This page crashed')).toBeInTheDocument();
    throwFlag = false;
    await userEvent.click(screen.getByRole('button', { name: /retry/i }));
    expect(screen.getByText('recovered')).toBeInTheDocument();
  });

  it('test_reports_error_to_telemetry_with_routePath', () => {
    render(
      <RouteBoundary routePath="/deployments">
        <Bomb />
      </RouteBoundary>,
    );
    expect(mockReportError).toHaveBeenCalledTimes(1);
    const [errArg, infoArg] = mockReportError.mock.calls[0];
    expect(errArg.message).toBe('boom in render');
    expect(infoArg.routePath).toBe('/deployments');
  });

  it('test_shows_error_message_in_dev_mode', () => {
    // import.meta.env.DEV is true in vitest by default
    render(
      <RouteBoundary routePath="/foo">
        <Bomb />
      </RouteBoundary>,
    );
    // Dev mode shows actual error message for debugging
    expect(screen.getByText(/boom in render/)).toBeInTheDocument();
  });
});

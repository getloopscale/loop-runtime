import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { RuntimeLogsPage } from './runtime-logs';

type UseLogStreamReturn = {
  lines: never[];
  connected: boolean;
  error: boolean;
  clear: () => void;
};

const mockUseLogStream = vi.fn(
  (_path: string | null, _opts: { paused?: boolean }): UseLogStreamReturn => ({
    lines: [],
    connected: false,
    error: false,
    clear: vi.fn(),
  }),
);

vi.mock('@/hooks/use-log-stream', () => ({
  useLogStream: (path: string | null, opts: { paused?: boolean }) => mockUseLogStream(path, opts),
}));
vi.mock('@/hooks/use-auth', () => ({ useRequireAuth: vi.fn() }));
vi.mock('@/hooks/use-page-meta', () => ({ useSetPageTitle: vi.fn() }));

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/projects/my-app/logs']}>
      <Routes>
        <Route path="/projects/:name/logs" element={<RuntimeLogsPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('RuntimeLogsPage (J-19 T1.1 — R1 TimeRangePicker wire)', () => {
  beforeEach(() => mockUseLogStream.mockClear());

  // 2026-06-13 LIVE walkthrough fix gates (BLOCKER F-tests-1 closure):
  // Default range was changed from '1h' → '5m' (commit e80d7c4f sponsor UX).
  // env=production was added (commit 2acc983b) — backend
  // StreamRuntimeLogs requires env query param (returns 400 MISSING_FIELD).
  // Hermetic test MUST assert BOTH to prevent regression of the LIVE catch.
  it('test_runtime_logs_default_stream_path_includes_env_production_and_since_5m', () => {
    renderPage();
    expect(mockUseLogStream).toHaveBeenCalled();
    const path = mockUseLogStream.mock.calls[0][0];
    expect(path).toMatch(/[?&]env=production(&|$)/);
    expect(path).toMatch(/[?&]since=5m(&|$)/);
  });

  it('test_runtime_logs_stream_path_env_precedes_since_canonical_query_order', () => {
    // Canonical query order: env first (backend-required), then since (optional).
    // Defensive — backend parseSinceParam is order-independent, but the canonical
    // order documented in api/internal/routes/logs_stream.go locks env-first.
    renderPage();
    const path = mockUseLogStream.mock.calls[0][0];
    expect(path).not.toBeNull();
    const envIdx = path!.indexOf('env=');
    const sinceIdx = path!.indexOf('since=');
    expect(envIdx).toBeGreaterThan(-1);
    expect(sinceIdx).toBeGreaterThan(-1);
    expect(envIdx).toBeLessThan(sinceIdx);
  });

  it('test_runtime_logs_change_preset_keeps_env_production_and_updates_since', async () => {
    const user = userEvent.setup();
    renderPage();
    const trigger = screen.getByRole('combobox', { name: /time range/i });
    await user.click(trigger);
    const item6h = await screen.findByRole('option', { name: /6 hours/i });
    await user.click(item6h);
    const lastCall = mockUseLogStream.mock.calls.at(-1);
    // env=production MUST persist across preset changes (regression gate for F-tests-1).
    expect(lastCall![0]).toMatch(/[?&]env=production(&|$)/);
    expect(lastCall![0]).toMatch(/[?&]since=6h(&|$)/);
  });
});

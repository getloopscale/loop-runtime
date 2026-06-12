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

  it('test_runtime_logs_default_stream_path_includes_since_1h', () => {
    renderPage();
    expect(mockUseLogStream).toHaveBeenCalled();
    const path = mockUseLogStream.mock.calls[0][0];
    expect(path).toMatch(/[?&]since=1h/);
  });

  it('test_runtime_logs_change_preset_updates_stream_path_since', async () => {
    const user = userEvent.setup();
    renderPage();
    const trigger = screen.getByRole('combobox', { name: /time range/i });
    await user.click(trigger);
    const item6h = await screen.findByRole('option', { name: /6 hours/i });
    await user.click(item6h);
    const lastCall = mockUseLogStream.mock.calls.at(-1);
    expect(lastCall![0]).toMatch(/[?&]since=6h/);
  });
});

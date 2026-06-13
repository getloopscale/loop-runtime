import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { Build } from '@/lib/api/types';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import { CancelBuildDialog } from './cancel-build-dialog';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function makeBuild(overrides: Partial<Build> = {}): Build {
  return {
    build_id: 'b-1',
    project_id: 'p-1',
    environment: 'production',
    status: 'running',
    commit_sha: 'aaaaaaa1234567890',
    created_by: 'paulo@loopzero.test',
    created_at: '2026-06-06T00:00:00Z',
    ...overrides,
  };
}

function renderDialog(build: Build = makeBuild(), onCancelled = vi.fn()) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return render(
    <QueryClientProvider client={client}>
      <CancelBuildDialog
        open
        onOpenChange={vi.fn()}
        build={build}
        onCancelled={onCancelled}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('CancelBuildDialog', () => {
  it('test_dialog_shows_build_sha_and_environment_for_confirm', () => {
    renderDialog(
      makeBuild({ commit_sha: 'aaaaaaa1234567890', environment: 'staging' }),
    );
    // SHA appears in title (sliced 7).
    expect(screen.getAllByText(/aaaaaaa/i).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText(/staging/i)).toBeInTheDocument();
  });

  it('test_cancel_button_disables_during_request', async () => {
    // Hold the request open so we can observe the in-flight disabled state.
    let resolve: ((v: unknown) => void) | null = null;
    mockedEngineFetch.mockImplementation(
      () => new Promise((r) => { resolve = r; }),
    );
    renderDialog();

    const confirmBtn = screen.getByRole('button', { name: /^cancel build$/i });
    await act(async () => {
      await userEvent.click(confirmBtn);
    });
    expect(screen.getByRole('button', { name: /cancelling/i })).toBeDisabled();
    // Let the mutation settle so React Query stops complaining.
    await act(async () => {
      resolve?.(null);
    });
  });

  it('test_success_fires_onCancelled_callback', async () => {
    mockedEngineFetch.mockResolvedValue(null);
    const onCancelled = vi.fn();
    renderDialog(makeBuild(), onCancelled);

    const confirmBtn = screen.getByRole('button', { name: /^cancel build$/i });
    await act(async () => {
      await userEvent.click(confirmBtn);
    });
    await waitFor(() => expect(onCancelled).toHaveBeenCalled());
  });

  it('test_renders_error_when_request_fails', async () => {
    mockedEngineFetch.mockRejectedValue(new Error('BUILD_ALREADY_COMPLETED'));
    renderDialog();

    const confirmBtn = screen.getByRole('button', { name: /^cancel build$/i });
    await act(async () => {
      await userEvent.click(confirmBtn);
    });
    await waitFor(() =>
      expect(screen.getByText(/BUILD_ALREADY_COMPLETED/i)).toBeInTheDocument(),
    );
  });
});

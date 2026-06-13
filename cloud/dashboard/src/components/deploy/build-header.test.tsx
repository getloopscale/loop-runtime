import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { describe, expect, it, vi } from 'vitest';
import { BuildHeader } from './build-header';
import type { Build } from '@/lib/api/types';

function makeBuild(overrides: Partial<Build> = {}): Build {
  return {
    build_id: 'b-1',
    project_id: 'p-1',
    environment: 'production',
    status: 'succeeded',
    commit_sha: '1234567890abcdef1234567890abcdef12345678',
    created_by: 'paulo@loopzero.test',
    created_at: '2026-06-06T00:00:00Z',
    started_at: '2026-06-06T00:00:00Z',
    finished_at: '2026-06-06T00:02:00Z',
    duration_seconds: 120,
    ...overrides,
  };
}

function renderHeader(
  build: Build,
  opts: { branch?: string; onCancel?: () => void; onRollback?: () => void; onRedeploy?: () => void } = {},
) {
  return render(
    <MemoryRouter>
      <BuildHeader
        build={build}
        branch={opts.branch ?? 'main'}
        onCancel={opts.onCancel ?? vi.fn()}
        onRollback={opts.onRollback ?? vi.fn()}
        onRedeploy={opts.onRedeploy ?? vi.fn()}
      />
    </MemoryRouter>,
  );
}

describe('BuildHeader', () => {
  it('test_renders_sha_and_branch', () => {
    const build = makeBuild({ commit_sha: '1234567890abcdef1234567890abcdef12345678' });
    renderHeader(build, { branch: 'feature/x' });

    // SHA shown as 7-char short form.
    expect(screen.getByText('1234567')).toBeInTheDocument();
    expect(screen.getByText('feature/x')).toBeInTheDocument();
  });

  it('test_cancel_button_only_when_running', async () => {
    // RUNNING → button visible
    const onCancel = vi.fn();
    const { unmount } = renderHeader(makeBuild({ status: 'running' }), { onCancel });
    const cancelBtn = screen.getByRole('button', { name: /cancel build/i });
    expect(cancelBtn).toBeInTheDocument();
    await userEvent.click(cancelBtn);
    expect(onCancel).toHaveBeenCalledOnce();
    unmount();

    // SUCCEEDED → button absent
    renderHeader(makeBuild({ status: 'succeeded' }));
    expect(screen.queryByRole('button', { name: /cancel build/i })).not.toBeInTheDocument();
  });

  it('test_rollback_button_only_when_succeeded', async () => {
    // SUCCEEDED → visible
    const onRollback = vi.fn();
    const { unmount } = renderHeader(makeBuild({ status: 'succeeded' }), { onRollback });
    const rollbackBtn = screen.getByRole('button', { name: /rollback/i });
    expect(rollbackBtn).toBeInTheDocument();
    await userEvent.click(rollbackBtn);
    expect(onRollback).toHaveBeenCalledOnce();
    unmount();

    // RUNNING → absent
    renderHeader(makeBuild({ status: 'running' }));
    expect(screen.queryByRole('button', { name: /rollback/i })).not.toBeInTheDocument();

    // FAILED → absent
    const { unmount: unmount2 } = renderHeader(makeBuild({ status: 'failed' }));
    expect(screen.queryByRole('button', { name: /rollback/i })).not.toBeInTheDocument();
    unmount2();
  });

  it('test_redeploy_button_always_visible', async () => {
    const onRedeploy = vi.fn();
    for (const status of ['queued', 'running', 'succeeded', 'failed', 'cancelled', 'cancelling'] as const) {
      const { unmount } = renderHeader(makeBuild({ status }), { onRedeploy });
      const btn = screen.getByRole('button', { name: /redeploy/i });
      expect(btn).toBeInTheDocument();
      // Disabled while in-flight (queued / running / cancelling) — fires only on terminal.
      if (status === 'queued' || status === 'running' || status === 'cancelling') {
        expect(btn).toBeDisabled();
      } else {
        expect(btn).toBeEnabled();
        await userEvent.click(btn);
      }
      unmount();
    }
    // Three terminal statuses fire onRedeploy (succeeded + failed + cancelled).
    expect(onRedeploy).toHaveBeenCalledTimes(3);
  });
});

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';

const mockUseDeployments = vi.fn();
vi.mock('@/hooks/use-deployments', () => ({
  useDeployments: () => mockUseDeployments(),
}));
vi.mock('@/lib/rum', () => ({ reportEvent: vi.fn() }));
vi.mock('canvas-confetti', () => ({ default: vi.fn() }));

import { useFirstDeployTrigger } from './use-first-deploy-trigger';

beforeEach(() => {
  localStorage.clear();
  mockUseDeployments.mockReset();
});

describe('useFirstDeployTrigger (Sprint 3 T3.4 / EC-05)', () => {
  it('test_does_not_fire_for_historical_build', () => {
    // Build completed BEFORE mount — historical, should NOT fire
    const historical = {
      build_id: 'b-old',
      status: 'succeeded',
      finished_at: new Date(Date.now() - 60 * 60 * 1000).toISOString(), // 1h ago
    };
    mockUseDeployments.mockReturnValue({ builds: [historical], loading: false, error: false });
    const toast = { show: vi.fn() };
    renderHook(() => useFirstDeployTrigger({ projectName: 'p1', toast }));
    expect(toast.show).not.toHaveBeenCalled();
  });

  it('test_does_not_fire_when_build_lacks_finished_at', () => {
    // Defensive: builds in running/queued status have no finished_at
    const running = { build_id: 'b-running', status: 'running' };
    mockUseDeployments.mockReturnValue({ builds: [running], loading: false, error: false });
    const toast = { show: vi.fn() };
    renderHook(() => useFirstDeployTrigger({ projectName: 'p1', toast }));
    expect(toast.show).not.toHaveBeenCalled();
  });

  it('test_does_not_fire_again_after_localStorage_flag_set', () => {
    // Pre-set the celebrated flag — subsequent builds shouldn't re-fire
    localStorage.setItem('loopzero-first-deploy-p1-celebrated', '1');
    const future = {
      build_id: 'b-new',
      status: 'succeeded',
      finished_at: new Date(Date.now() + 1000).toISOString(),
    };
    mockUseDeployments.mockReturnValue({ builds: [future], loading: false, error: false });
    const toast = { show: vi.fn() };
    renderHook(() => useFirstDeployTrigger({ projectName: 'p1', toast }));
    expect(toast.show).not.toHaveBeenCalled();
  });

  it('test_fires_when_build_transitions_during_session', () => {
    const realtime = {
      build_id: 'b-realtime',
      status: 'succeeded',
      // Completed AFTER mountedAt (we just rendered hook)
      finished_at: new Date(Date.now() + 5000).toISOString(),
    };
    mockUseDeployments.mockReturnValue({ builds: [realtime], loading: false, error: false });
    const toast = { show: vi.fn() };
    renderHook(() =>
      useFirstDeployTrigger({ projectName: 'p1', toast, liveUrl: 'https://app.example.com' }),
    );
    expect(toast.show).toHaveBeenCalledWith('Your app is live at https://app.example.com 🎉');
    expect(localStorage.getItem('loopzero-first-deploy-p1-celebrated')).toBe('1');
  });
});

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';

const mockUseWhoami = vi.fn();
const mockUseProjects = vi.fn();
const mockUseEngineApi = vi.fn();
const mockUseDeployments = vi.fn();

vi.mock('@/hooks/use-whoami', () => ({ useWhoami: () => mockUseWhoami() }));
vi.mock('@/hooks/use-projects', () => ({ useProjects: () => mockUseProjects() }));
vi.mock('@/hooks/use-api', () => ({
  useEngineApi: (path: string) => mockUseEngineApi(path),
  useCloudApi: () => ({ data: null, loading: false, error: false }),
}));
vi.mock('@/hooks/use-deployments', () => ({
  useDeployments: () => mockUseDeployments(),
}));

import { useOnboardingState, ONBOARDING_KEY } from './use-onboarding-state';

beforeEach(() => {
  localStorage.clear();
  mockUseWhoami.mockReset();
  mockUseProjects.mockReset();
  mockUseEngineApi.mockReset();
  mockUseDeployments.mockReset();
  mockUseWhoami.mockReturnValue({ whoami: null, loading: false, error: false });
  mockUseProjects.mockReturnValue({ projects: [], loading: false });
  mockUseEngineApi.mockReturnValue({ data: { domains: [] }, loading: false, error: false });
  mockUseDeployments.mockReturnValue({ builds: [], loading: false, error: false });
});

describe('useOnboardingState (Sprint 3 T3.1)', () => {
  it('test_returns_zero_completed_for_fresh_session', () => {
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.completed).toBe(0);
    expect(result.current.total).toBe(5);
    expect(result.current.percent).toBe(0);
  });

  it('test_marks_logged_in_when_session_active', () => {
    mockUseWhoami.mockReturnValue({ whoami: { email: 'a@b.com' }, loading: false, error: false });
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.state.steps.logged_in).toBe(true);
    expect(result.current.completed).toBeGreaterThanOrEqual(1);
  });

  it('test_marks_project_initialized_when_projects_nonempty', () => {
    mockUseProjects.mockReturnValue({ projects: [{ name: 'p1' }], loading: false });
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.state.steps.project_initialized).toBe(true);
  });

  it('test_marks_deploy_succeeded_when_any_succeeded_build', () => {
    mockUseDeployments.mockReturnValue({
      builds: [{ status: 'succeeded' }],
      loading: false,
      error: false,
    });
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.state.steps.deploy_succeeded).toBe(true);
  });

  it('test_dismiss_persists_in_localStorage', () => {
    const { result } = renderHook(() => useOnboardingState());
    act(() => result.current.dismiss());
    expect(result.current.state.dismissed).toBe(true);
    expect(localStorage.getItem(ONBOARDING_KEY)).toContain('"dismissed":true');
  });

  it('test_replay_clears_dismissed_flag', () => {
    const { result } = renderHook(() => useOnboardingState());
    act(() => result.current.dismiss());
    act(() => result.current.replay());
    expect(result.current.state.dismissed).toBe(false);
  });

  it('test_isVisible_false_when_dismissed', () => {
    const { result } = renderHook(() => useOnboardingState());
    act(() => result.current.dismiss());
    expect(result.current.isVisible).toBe(false);
  });

  it('test_isVisible_false_when_all_complete', () => {
    mockUseWhoami.mockReturnValue({ whoami: { email: 'a@b.com' }, loading: false, error: false });
    mockUseProjects.mockReturnValue({ projects: [{ name: 'p' }], loading: false });
    mockUseDeployments.mockReturnValue({ builds: [{ status: 'succeeded' }], loading: false, error: false });
    mockUseEngineApi.mockReturnValue({
      data: { domains: [{ domain: 'a.com' }] },
      loading: false,
      error: false,
    });
    const { result } = renderHook(() => useOnboardingState());
    act(() => result.current.markStepDone('cli_installed'));
    expect(result.current.completed).toBe(5);
    expect(result.current.isVisible).toBe(false);
  });

  // EC-04: localStorage failures handled gracefully
  it('test_safeGet_returns_default_when_localStorage_throws', () => {
    const original = Storage.prototype.getItem;
    Storage.prototype.getItem = vi.fn(() => {
      throw new Error('SecurityError');
    });
    try {
      const { result } = renderHook(() => useOnboardingState());
      expect(result.current.completed).toBe(0);
      expect(result.current.state.dismissed).toBe(false);
    } finally {
      Storage.prototype.getItem = original;
    }
  });

  it('test_safeGet_returns_default_when_json_corrupted', () => {
    localStorage.setItem(ONBOARDING_KEY, 'not-valid-json{{{');
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.state.dismissed).toBe(false);
  });

  it('test_safeSet_does_not_throw_on_quotaExceeded', () => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = vi.fn(() => {
      throw new Error('QuotaExceeded');
    });
    try {
      const { result } = renderHook(() => useOnboardingState());
      expect(() => act(() => result.current.dismiss())).not.toThrow();
    } finally {
      Storage.prototype.setItem = original;
    }
  });

  // EC-06: replay must show widget even when completed === total
  it('test_replay_resets_completion_when_already_complete', () => {
    mockUseWhoami.mockReturnValue({ whoami: { email: 'a@b.com' }, loading: false, error: false });
    mockUseProjects.mockReturnValue({ projects: [{ name: 'p' }], loading: false });
    mockUseDeployments.mockReturnValue({ builds: [{ status: 'succeeded' }], loading: false, error: false });
    mockUseEngineApi.mockReturnValue({
      data: { domains: [{ domain: 'a.com' }] },
      loading: false,
      error: false,
    });
    const { result } = renderHook(() => useOnboardingState());
    act(() => result.current.markStepDone('cli_installed'));
    expect(result.current.completed).toBe(5);
    expect(result.current.isVisible).toBe(false);
    act(() => result.current.replay());
    // After replay: cli_installed reset to false (user-confirmed step),
    // but auto-derived steps remain. completed=4, isVisible=true.
    expect(result.current.state.steps.cli_installed).toBe(false);
    expect(result.current.isVisible).toBe(true);
  });

  // EC-14: cli_installed must NOT auto-mark
  it('test_cli_installed_only_via_markStepDone_not_auto', () => {
    mockUseWhoami.mockReturnValue({ whoami: { email: 'a@b.com' }, loading: false, error: false });
    const { result } = renderHook(() => useOnboardingState());
    expect(result.current.state.steps.cli_installed).toBe(false);
    act(() => result.current.markStepDone('cli_installed'));
    expect(result.current.state.steps.cli_installed).toBe(true);
  });
});

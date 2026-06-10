import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

const mockUseProjects = vi.fn();
const mockUseEngineApi = vi.fn();
const mockUseDeployments = vi.fn();
const mockUseProjectStatus = vi.fn();

vi.mock('@/hooks/use-projects', () => ({ useProjects: () => mockUseProjects() }));
vi.mock('@/hooks/use-api', () => ({
  useEngineApi: (path: string) => mockUseEngineApi(path),
  useCloudApi: () => ({ data: null, loading: false, error: false }),
}));
vi.mock('@/hooks/use-deployments', () => ({
  useDeployments: (name: string | undefined) => mockUseDeployments(name),
}));
vi.mock('@/hooks/use-project-status', () => ({
  useProjectStatus: (name: string | undefined) => mockUseProjectStatus(name),
}));

import { useReadinessSignals } from './use-readiness-signals';

function makeClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}
const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={makeClient()}>{children}</QueryClientProvider>
);

function withDefaults(overrides: Record<string, unknown> = {}) {
  mockUseProjects.mockReturnValue({ projects: [{ name: 'p1' }], loading: false });
  mockUseEngineApi.mockImplementation((path: string) => {
    if (path.includes('/domains')) return { data: { domains: [] }, loading: false, error: false };
    if (path.includes('/secrets')) return { data: { secrets: [] }, loading: false, error: false };
    return { data: null, loading: false, error: false };
  });
  mockUseDeployments.mockReturnValue({ builds: [], loading: false, error: false });
  mockUseProjectStatus.mockReturnValue({ status: null, loading: false, error: false });
  Object.assign({}, overrides);
}

describe('useReadinessSignals (Sprint 1 T1.3)', () => {
  beforeEach(() => {
    mockUseProjects.mockReset();
    mockUseEngineApi.mockReset();
    mockUseDeployments.mockReset();
    mockUseProjectStatus.mockReset();
    withDefaults();
  });

  it('test_returns_8_signals', () => {
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals).toHaveLength(8);
  });

  // EC-01: builds=[] should not crash signal 6
  it('test_signal_6_returns_unknown_when_no_builds', () => {
    mockUseDeployments.mockReturnValue({ builds: [], loading: false, error: false });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    const sig6 = result.current.signals.find((s) => s.id === 'last-build');
    expect(sig6?.status).toBe('unknown');
  });

  it('test_signal_6_returns_unknown_for_empty_builds_array', () => {
    mockUseDeployments.mockReturnValue({ builds: [], loading: false, error: false });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'last-build')?.status).toBe('unknown');
  });

  it('test_status_ok_when_domain_verified', () => {
    mockUseEngineApi.mockImplementation((path: string) => {
      if (path.includes('/domains')) {
        return {
          data: { domains: [{ domain: 'app.example.com', project: 'p1', status: 'active' }] },
          loading: false,
          error: false,
        };
      }
      return { data: { secrets: [] }, loading: false, error: false };
    });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'domain-attached')?.status).toBe('ok');
  });

  it('test_status_warning_when_no_secrets', () => {
    mockUseEngineApi.mockImplementation((path: string) => {
      if (path.includes('/secrets')) return { data: { secrets: [] }, loading: false, error: false };
      return { data: { domains: [] }, loading: false, error: false };
    });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'secrets-defined')?.status).toBe('warning');
  });

  it('test_status_fail_when_zero_replicas_ready', () => {
    mockUseProjectStatus.mockReturnValue({
      status: { replicas_ready: 0, replicas_desired: 3 },
      loading: false,
      error: false,
    });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'deployment-healthy')?.status).toBe('fail');
  });

  // EC-08: domain filter by project
  it('test_domain_signal_filters_by_project_name', () => {
    mockUseEngineApi.mockImplementation((path: string) => {
      if (path.includes('/domains')) {
        return {
          data: {
            domains: [
              { domain: 'a.com', project: 'other-1', status: 'active' },
              { domain: 'b.com', project: 'other-2', status: 'active' },
              { domain: 'c.com', project: 'p1', status: 'active' },
            ],
          },
          loading: false,
          error: false,
        };
      }
      return { data: { secrets: [] }, loading: false, error: false };
    });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'domain-attached')?.status).toBe('ok');
  });

  it('test_domain_signal_warning_when_zero_for_target_project', () => {
    mockUseEngineApi.mockImplementation((path: string) => {
      if (path.includes('/domains')) {
        return {
          data: {
            domains: [
              { domain: 'a.com', project: 'other-1', status: 'active' },
              { domain: 'b.com', project: 'other-2', status: 'active' },
            ],
          },
          loading: false,
          error: false,
        };
      }
      return { data: { secrets: [] }, loading: false, error: false };
    });
    const { result } = renderHook(() => useReadinessSignals('p1'), { wrapper });
    expect(result.current.signals.find((s) => s.id === 'domain-attached')?.status).toBe('warning');
  });
});

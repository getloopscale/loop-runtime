import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({ engineFetch: vi.fn() }));
vi.mock('@/lib/config', () => ({ API_URL: '' }));
vi.mock('@/hooks/use-auth', () => ({ useRequireAuth: vi.fn() }));
vi.mock('@/hooks/use-page-meta', () => ({ useSetPageTitle: vi.fn() }));

import { SecretsPage } from './secrets';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function renderAt(initialPath: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route path="/projects/:name/secrets" element={children} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
  return render(<SecretsPage />, { wrapper: Wrapper });
}

beforeEach(() => {
  vi.clearAllMocks();
  // Route the mock by URL — `secrets.tsx` now calls BOTH /api/v1/secrets
  // (existing T4.2 contract) AND /api/v1/environments (T1.4 dynamic envs).
  // Returning a single resolved value used to suffice; post-T1.4 we
  // dispatch by inspecting the URL.
  mockedEngineFetch.mockImplementation(async (url: string) => {
    if (url.startsWith('/api/v1/environments')) {
      // Default tier shape — production + staging.
      return {
        environments: [{ name: 'production' }, { name: 'staging' }],
        permanent: ['production', 'staging'],
      } as unknown;
    }
    // /api/v1/secrets — preserve the original fixture so existing
    // assertions on PageShell empty-state vs populated remain valid.
    return {
      secrets: [{ name: 'DATABASE_URL', environments: ['production'] }],
    } as unknown;
  });
});

describe('SecretsPage — J-22 T4.2 URL state', () => {
  it('test_default_env_filter_is_all_fans_out_with_project', async () => {
    renderAt('/projects/teste/secrets');
    const select = (await screen.findByLabelText(/Environment/i)) as HTMLSelectElement;
    expect(select.value).toBe('all');
    const calls = mockedEngineFetch.mock.calls.map((c) => c[0]);
    expect(calls).toContain('/api/v1/secrets?project=teste&env=production');
    expect(calls).toContain('/api/v1/secrets?project=teste&env=staging');
  });

  it('test_url_query_param_preselects_filter', async () => {
    renderAt('/projects/teste/secrets?env=production');
    const select = (await screen.findByLabelText(/Environment/i)) as HTMLSelectElement;
    expect(select.value).toBe('production');
    expect(mockedEngineFetch).toHaveBeenCalledWith(
      '/api/v1/secrets?project=teste&env=production',
    );
  });

  it('test_toggle_filter_triggers_refetch_with_new_url', async () => {
    renderAt('/projects/teste/secrets');
    const select = await screen.findByLabelText(/Environment/i);
    fireEvent.change(select, { target: { value: 'staging' } });
    await new Promise((r) => setTimeout(r, 100));
    const calls = mockedEngineFetch.mock.calls.map((c) => c[0]);
    expect(calls).toContain('/api/v1/secrets?project=teste&env=staging');
  });
});

describe('SecretsPage — Plan 2026-06-11 T1.4 (closes J-22↔J-10)', () => {
  it('test_renders_custom_env_so_hardcoded_list_no_longer_drops_it', async () => {
    // Tenant uses a non-default env name `staging-eu-1`. Before T1.4 the
    // dropdown literal `['all', 'production', 'staging']` silently
    // dropped it. Post-T1.4 the dropdown derives from useEnvironments.
    mockedEngineFetch.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/v1/environments')) {
        return {
          environments: [{ name: 'production' }, { name: 'staging-eu-1' }],
          permanent: ['production', 'staging-eu-1'],
        } as unknown;
      }
      return {
        secrets: [{ name: 'DATABASE_URL', environments: ['production'] }],
      } as unknown;
    });
    renderAt('/projects/teste/secrets');
    const select = (await screen.findByLabelText(/Environment/i)) as HTMLSelectElement;
    // Find the option element with the custom env value.
    const opts = Array.from(select.options).map((o) => o.value);
    expect(opts).toContain('staging-eu-1');
    expect(opts).toContain('production');
    expect(opts).toContain('all');
    // The hardcoded legacy 'staging' MUST NOT appear when the tenant only
    // owns 'staging-eu-1' — that was the very J-22↔J-10 break.
    expect(opts).not.toContain('staging');
  });

  it('test_fallback_on_hook_error_renders_default_env_and_warning_ec5', async () => {
    // EC-5 absorption: the hook surfaces an error so the page MUST render
    // a visible warning and still let the user select the canonical
    // permanent env (the fallback). UI must not collapse silently.
    mockedEngineFetch.mockImplementation(async (url: string) => {
      if (url.startsWith('/api/v1/environments')) {
        throw new Error('network down');
      }
      return {
        secrets: [{ name: 'DATABASE_URL', environments: ['production'] }],
      } as unknown;
    });
    renderAt('/projects/teste/secrets');
    const select = (await screen.findByLabelText(/Environment/i)) as HTMLSelectElement;
    const opts = Array.from(select.options).map((o) => o.value);
    // Fallback contains 'all' (synthetic) + the default permanent env.
    expect(opts).toContain('all');
    expect(opts).toContain('production');
    // Warning visible to the user.
    const warning = await screen.findByRole('alert');
    expect(warning.textContent).toMatch(/default env only/i);
  });
});

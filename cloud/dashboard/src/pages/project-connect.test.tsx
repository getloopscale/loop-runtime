import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@/test/render';
import { Route, Routes } from 'react-router-dom';

vi.mock('@/hooks/use-auth', () => ({
  useRequireAuth: () => ({ id: 'test-user', email: 'test@example.com' }),
}));

const engineFetchMock = vi.fn();
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
  return {
    ...actual,
    engineFetch: (path: string, opts?: RequestInit) => engineFetchMock(path, opts),
  };
});

import { ProjectConnectPage } from './project-connect';

// Plan J-09 T1.2 + T2.2 + T3.1 (2026-06-05).
// T3.1 — POST payload alignment.
// T1.2 — Install-button FSM, polling, visibility refetch, ?updated=1 banner.
// T2.2 — Repo picker UI fetched from
//        /api/v1/integrations/github/installations/{id}/repositories.

function renderConnect(name = 'my-api', search = '') {
  const initialEntry = `/projects/${name}/connect${search}`;
  return render(
    <Routes>
      <Route path="/projects/:name/connect" element={<ProjectConnectPage />} />
    </Routes>,
    { initialEntries: [initialEntry] },
  );
}

// defaultRepoList returns the canonical 2-repo payload tests use to
// drive the picker. Tests overriding the engineFetchMock should call
// this to mirror the production shape.
function defaultRepoList() {
  return [
    { full_name: 'owner/api', default_branch: 'main', private: false, language: 'Go' },
    { full_name: 'owner/web', default_branch: 'develop', private: true, language: 'TypeScript' },
  ];
}

describe('ProjectConnectPage — Plan J-09 T3.1 + T2.2', () => {
  let posts: Array<{ url: string; body: Record<string, unknown> }>;

  beforeEach(() => {
    posts = [];
    engineFetchMock.mockImplementation(async (path: string, opts?: RequestInit) => {
      const method = (opts?.method ?? 'GET').toUpperCase();
      if (method === 'GET') {
        if (path.includes('/integrations/github/installations/')) {
          return { repositories: defaultRepoList() };
        }
        return { connected: false, installation_id: 42 };
      }
      if (method === 'POST') {
        const parsed = opts?.body ? JSON.parse(String(opts.body)) : {};
        posts.push({ url: path, body: parsed });
        return { ok: true, repo: parsed.repo, branch: parsed.branch, auto_deploy: parsed.auto_deploy };
      }
      if (method === 'DELETE') return null;
      return null;
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('test_picker_renders_repo_list — once installation_id known, picker fetches and renders options', async () => {
    renderConnect('demo');
    await waitFor(() => expect(screen.getByLabelText(/^repository$/i)).toBeInTheDocument());
    expect(screen.getByRole('option', { name: /owner\/api/i })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: /owner\/web/i })).toBeInTheDocument();
  });

  it('test_picker_pre_fills_default_branch — selecting a repo updates the branch input', async () => {
    renderConnect('demo');
    await waitFor(() => expect(screen.getByLabelText(/^repository$/i)).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText(/^repository$/i, { selector: 'select' }), {
      target: { value: 'owner/web' },
    });
    await waitFor(() => expect((screen.getByLabelText(/branch/i) as HTMLInputElement).value).toBe('develop'));
  });

  it('test_picker_empty_state_shows_configure_link — empty list surfaces GitHub settings link (EC-6)', async () => {
    engineFetchMock.mockImplementation(async (path: string, opts?: RequestInit) => {
      const method = (opts?.method ?? 'GET').toUpperCase();
      if (method === 'GET') {
        if (path.includes('/integrations/github/installations/')) {
          return { repositories: [] };
        }
        return { connected: false, installation_id: 99 };
      }
      return null;
    });
    renderConnect('demo');
    await waitFor(() =>
      expect(screen.getByText(/no repositories available/i)).toBeInTheDocument(),
    );
    const link = screen.getByRole('link', { name: /configure app access/i }) as HTMLAnchorElement;
    expect(link.href).toBe('https://github.com/settings/installations/99');
  });

  it('test_connect_post_body_shape — picker selection drives canonical {repo, installation_id, branch, auto_deploy} payload', async () => {
    renderConnect('my-api');
    await waitFor(() => expect(screen.getByLabelText(/^repository$/i)).toBeInTheDocument());
    fireEvent.change(screen.getByLabelText(/^repository$/i, { selector: 'select' }), {
      target: { value: 'owner/api' },
    });
    fireEvent.click(screen.getByRole('button', { name: /connect repository/i }));

    await waitFor(() => expect(posts.length).toBeGreaterThanOrEqual(1));
    const lastPost = posts[posts.length - 1];

    expect(lastPost.body).toHaveProperty('repo', 'owner/api');
    expect(lastPost.body).not.toHaveProperty('repo_url');
    expect(typeof lastPost.body.installation_id).toBe('number');
    expect(lastPost.body.installation_id).toBe(42);
    expect(lastPost.body).toHaveProperty('branch', 'main');
    expect(lastPost.body).toHaveProperty('auto_deploy');
  });

  it('test_connect_blocks_when_no_installation_id', async () => {
    engineFetchMock.mockImplementation(async (path: string, opts?: RequestInit) => {
      const method = (opts?.method ?? 'GET').toUpperCase();
      if (method === 'GET') return { connected: false };
      if (method === 'POST') {
        posts.push({ url: path, body: opts?.body ? JSON.parse(String(opts.body)) : {} });
        return { ok: true };
      }
      return null;
    });

    renderConnect();
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /install github app/i }),
      ).toBeInTheDocument(),
    );
    expect(posts.length).toBe(0);
  });
});

describe('ProjectConnectPage — Plan J-09 T1.2 install FSM', () => {
  let getCount: number;
  let openSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    getCount = 0;
    engineFetchMock.mockImplementation(async (_path: string, opts?: RequestInit) => {
      const method = (opts?.method ?? 'GET').toUpperCase();
      if (method === 'GET') {
        getCount++;
        // Stays in 'idle' until the test mutates the mock.
        return { connected: false };
      }
      return null;
    });
    openSpy = vi
      .spyOn(window, 'open')
      .mockImplementation(() => ({ focus: () => undefined }) as unknown as Window);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
    openSpy.mockRestore();
  });

  it('test_install_button_opens_new_tab', async () => {
    renderConnect('demo');
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /install github app/i }),
      ).toBeInTheDocument(),
    );
    fireEvent.click(screen.getByRole('button', { name: /install github app/i }));

    expect(openSpy).toHaveBeenCalledTimes(1);
    const [url, target, features] = openSpy.mock.calls[0] as [string, string, string];
    expect(url).toBe(
      `/v1/integrations/github/install?return_to=${encodeURIComponent('/projects/demo/connect')}`,
    );
    expect(target).toBe('_blank');
    expect(features).toBe('noopener,noreferrer');
  });

  it('test_polling_stops_on_installation_id_received', async () => {
    renderConnect('demo');
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /install github app/i }),
      ).toBeInTheDocument(),
    );
    // Install fake timers AFTER initial render so the async useEffect
    // microtask queue has already drained. shouldAdvanceTime keeps the
    // microtask queue flowing even while we control setInterval ticks.
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const beforeClick = getCount;
    fireEvent.click(screen.getByRole('button', { name: /install github app/i }));
    // Immediate refetch fired by the click handler.
    await waitFor(() => expect(getCount).toBeGreaterThan(beforeClick));

    // From the next poll tick onwards the backend reports installation_id
    // AND the picker fetch starts succeeding with the canonical list.
    engineFetchMock.mockImplementation(async (path: string, opts?: RequestInit) => {
      if ((opts?.method ?? 'GET') === 'GET') {
        if (path.includes('/integrations/github/installations/')) {
          return { repositories: defaultRepoList() };
        }
        getCount++;
        return { connected: false, installation_id: 99 };
      }
      return null;
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_500);
    });

    // FSM transitions to has_installation -> picker appears.
    await waitFor(() =>
      expect(screen.getByLabelText(/^repository$/i)).toBeInTheDocument(),
    );

    const countAfterTransition = getCount;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(6_000);
    });
    // Polling MUST have stopped -- no further GETs.
    expect(getCount).toBe(countAfterTransition);
  });

  it('test_polling_max_5min_then_error', async () => {
    renderConnect('demo');
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /install github app/i }),
      ).toBeInTheDocument(),
    );
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.click(screen.getByRole('button', { name: /install github app/i }));

    // Advance past the 5min cap -- installation_id never arrives.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(POLL_TIMEOUT_PLUS_TICK);
    });

    await waitFor(() =>
      expect(screen.getByText(/didn.t see the github install complete/i)).toBeInTheDocument(),
    );
  });

  it('test_visibilitychange_debounced_500ms', async () => {
    renderConnect('demo');
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: /install github app/i }),
      ).toBeInTheDocument(),
    );
    vi.useFakeTimers({ shouldAdvanceTime: true });
    fireEvent.click(screen.getByRole('button', { name: /install github app/i }));

    // Immediate refetch fired by click handler.
    await waitFor(() => expect(getCount).toBeGreaterThan(0));
    const baseline = getCount;

    // Fire 5 visibility events within 100ms -- only the first should fetch.
    Object.defineProperty(document, 'visibilityState', { value: 'visible', configurable: true });
    for (let i = 0; i < 5; i++) {
      await act(async () => {
        document.dispatchEvent(new Event('visibilitychange'));
        await vi.advanceTimersByTimeAsync(20);
      });
    }
    expect(getCount - baseline).toBe(1);
  });

  it('test_updated_query_param_shows_updated_banner', async () => {
    renderConnect('demo', '?updated=1');
    await waitFor(() =>
      expect(
        screen.getByText(/repository access updated/i),
      ).toBeInTheDocument(),
    );
  });
});

// Mirror of POLL_TIMEOUT_MS in project-connect.tsx. Advance by 6
// extra seconds so at least three interval ticks land past the cap
// (the cap check is exclusive ">" and the interval fires every 2s).
const POLL_TIMEOUT_PLUS_TICK = 5 * 60 * 1_000 + 6_000;

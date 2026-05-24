import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen, waitFor } from '@/test/render';

vi.mock('@/lib/config', () => ({
  API_URL: 'http://kratos.local',
}));

vi.mock('@/lib/auth', () => ({
  getSession: vi.fn().mockResolvedValue(null),
  startLogin: vi.fn(),
}));

vi.mock('@/lib/api', () => ({
  clearToken: vi.fn(),
}));

// Stub fetch BEFORE module import — login useEffect fires fetch on mount.
const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

import { LoginPage } from './login';

interface FlowMessage {
  text: string;
  type: string;
}
interface FlowFixture {
  id: string;
  ui: {
    action: string;
    method: string;
    nodes: Array<{
      group: string;
      attributes: { name?: string; type?: string; value?: string };
    }>;
    messages: FlowMessage[];
  };
}

function flowFixture(): FlowFixture {
  return {
    id: 'flow-1',
    ui: {
      action: 'http://kratos.local/self-service/login?flow=flow-1',
      method: 'POST',
      nodes: [
        { group: 'default', attributes: { name: 'csrf_token', type: 'hidden', value: 'csrf-abc' } },
        { group: 'password', attributes: { name: 'identifier', type: 'email' } },
        { group: 'password', attributes: { name: 'password', type: 'password' } },
      ],
      messages: [],
    },
  };
}

describe('LoginPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFetch.mockReset();
  });

  it('test_login_loads_kratos_flow', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => flowFixture(),
    });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /continue with github/i })).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /continue with google/i })).toBeInTheDocument();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
  });

  it('test_login_submit_password_calls_kratos', async () => {
    // First fetch: load flow. Second fetch: submit password.
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ session: { id: 'sess-1' } }),
      });

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.type(screen.getByLabelText('Password'), 'secret123');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledTimes(2);
    });
    const [submitUrl, submitOpts] = mockFetch.mock.calls[1];
    expect(submitUrl).toBe('http://kratos.local/self-service/login?flow=flow-1');
    expect(submitOpts.method).toBe('POST');
    const body = JSON.parse(submitOpts.body);
    expect(body.method).toBe('password');
    expect(body.identifier).toBe('u@x.com');
    expect(body.password).toBe('secret123');
    expect(body.csrf_token).toBe('csrf-abc');
  });

  it('test_login_oidc_form_submit_preserves_vanilla_dom', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });

    // Spy on createElement BEFORE click so we capture the form creation.
    // EC-4 invariant: OIDC handler MUST use document.createElement('form') +
    // form.submit() — substituting fetch breaks the OIDC redirect chain.
    const createElementSpy = vi.spyOn(document, 'createElement');
    // form.submit is a no-op in jsdom (no navigation); stub to avoid crash.
    const submitStub = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => undefined);

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /continue with github/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /continue with github/i }));

    const formCalls = createElementSpy.mock.calls.filter((c) => c[0] === 'form');
    expect(formCalls.length).toBeGreaterThanOrEqual(1);
    expect(submitStub).toHaveBeenCalled();

    createElementSpy.mockRestore();
    submitStub.mockRestore();
  });

  it('test_login_session_expired_alert_when_searchParams_reason', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />, { initialEntries: ['/login?reason=expired'] });
    await waitFor(() => {
      expect(screen.getByText('Session expired')).toBeInTheDocument();
    });
  });

  it('test_login_displays_kratos_errors', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'Invalid credentials.', type: 'error' }];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByText('Invalid credentials.')).toBeInTheDocument();
    });
  });
});

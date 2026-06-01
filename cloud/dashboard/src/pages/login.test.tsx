import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen, waitFor } from '@/test/render';

vi.mock('@/lib/config', () => ({
  API_URL: 'http://kratos.local',
  AUTH_URL: '',
  ENGINE_URL: '',
  ACCOUNTS_URL: '',
}));

vi.mock('@/lib/auth', () => ({
  getSession: vi.fn().mockResolvedValue(null),
  startLogin: vi.fn(),
}));

vi.mock('@/lib/api', () => ({
  clearToken: vi.fn(),
}));

// Mock @/lib/rum to assert telemetry wire-points (T2.2).
const mockReportEvent = vi.fn();
vi.mock('@/lib/rum', async () => {
  const actual = await vi.importActual<typeof import('@/lib/rum')>('@/lib/rum');
  return {
    ...actual,
    reportEvent: (...args: unknown[]) => mockReportEvent(...args),
  };
});

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
    mockReportEvent.mockReset();
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

    // EC-4 invariant: OIDC handler MUST use document.createElement('form') +
    // form.submit() — substituting fetch breaks the OIDC redirect chain.
    const createElementSpy = vi.spyOn(document, 'createElement');
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

  // =========================================================================
  // T1.1 — Generic error message + safeErrorDetail allow-list (D2)
  // =========================================================================

  it('test_login_error_message_is_generic', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'User not found.', type: 'error' }];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    render(<LoginPage />);
    await waitFor(() => {
      // Generic title replaces raw Kratos text
      expect(screen.getByText("We couldn't sign you in.")).toBeInTheDocument();
    });
    // Raw text must NOT appear (OWASP A07)
    expect(screen.queryByText('User not found.')).not.toBeInTheDocument();
  });

  it('test_login_safe_detail_for_csrf_mismatch', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'The csrf_token is invalid.', type: 'error' }];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByText('Your session expired. Try again.')).toBeInTheDocument();
    });
  });

  it('test_login_safe_detail_priority_csrf_over_provider', async () => {
    // EC-2: errors with both provider AND csrf must surface csrf description
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [
      { text: 'provider error from github', type: 'error' },
      { text: 'csrf invalid', type: 'error' },
    ];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByText('Your session expired. Try again.')).toBeInTheDocument();
    });
    expect(screen.queryByText('Try a different sign-in method.')).not.toBeInTheDocument();
  });

  it('test_login_safe_detail_handles_empty_text', async () => {
    // EC-3: null/empty text must not throw TypeError
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [
      { text: '', type: 'error' },
      { text: null as unknown as string, type: 'error' },
    ];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    expect(() => render(<LoginPage />)).not.toThrow();
    await waitFor(() => {
      expect(screen.getByText("We couldn't sign you in.")).toBeInTheDocument();
    });
  });

  // =========================================================================
  // T1.2 — autoFocus on email input (G2)
  // =========================================================================

  it('test_login_email_input_has_autofocus', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    expect(document.activeElement).toBe(screen.getByLabelText('Email'));
  });

  // =========================================================================
  // T1.3 — Visible network error feedback + HTTP 5xx gate (EC-1 MUST FIX)
  // =========================================================================

  it('test_login_network_error_shows_alert', async () => {
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockRejectedValueOnce(new Error('Network down'));

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.type(screen.getByLabelText('Password'), 'secret123');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      expect(
        screen.getByText('Network error. Check your connection and try again.'),
      ).toBeInTheDocument();
    });
  });

  it('test_login_kratos_5xx_shows_network_error', async () => {
    // EC-1 MUST FIX: HTTP 5xx from Kratos must not silently swallow
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => ({}),
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
      expect(
        screen.getByText('Network error. Check your connection and try again.'),
      ).toBeInTheDocument();
    });
  });

  it('test_login_kratos_503_html_body_no_crash', async () => {
    // EC-1 secondary: 503 with HTML body (json throws) must not crash
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({
        ok: false,
        status: 503,
        json: async () => {
          throw new Error('Unexpected token < in JSON');
        },
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
      expect(
        screen.getByText('Network error. Check your connection and try again.'),
      ).toBeInTheDocument();
    });
  });

  // =========================================================================
  // T1.4 — Microcopy title + subtitle (G5)
  // =========================================================================

  it('test_login_renders_new_title_and_subtitle', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(
        screen.getByRole('heading', { level: 1, name: 'Sign in to LoopZero' }),
      ).toBeInTheDocument();
    });
    expect(
      screen.getByText('Deploy, monitor, and operate your workloads.'),
    ).toBeInTheDocument();
  });

  // =========================================================================
  // T2.1 — FormField + visible Label (G6, EC-4, EC-5)
  // =========================================================================

  it('test_login_labels_are_visible_text', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    // Visible <label> text in DOM (not just aria-label)
    const emailLabel = document.querySelector('label[for]');
    expect(emailLabel).toBeTruthy();
    expect(screen.getAllByText('Email').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Password').length).toBeGreaterThan(0);
  });

  it('test_login_label_htmlfor_matches_input_id', async () => {
    // EC-4: detect upstream @loopzero/ui regression
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    const emailInput = screen.getByLabelText('Email') as HTMLInputElement;
    const passwordInput = screen.getByLabelText('Password') as HTMLInputElement;
    expect(emailInput.id).toBeTruthy();
    expect(passwordInput.id).toBeTruthy();

    const labels = document.querySelectorAll('label[for]');
    const labelIds = Array.from(labels).map((l) => (l as HTMLLabelElement).htmlFor);
    expect(labelIds).toContain(emailInput.id);
    expect(labelIds).toContain(passwordInput.id);
  });

  it('test_login_input_has_accessible_name', async () => {
    // EC-5: validate FormField.Label provides accessible name
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    expect(screen.getByRole('textbox', { name: 'Email' })).toBeInTheDocument();
  });

  // =========================================================================
  // T2.2 — FAANG-grade auth telemetry (G7, D5)
  // =========================================================================

  it('test_login_emits_attempt_on_github_click', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(() => undefined);

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /continue with github/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /continue with github/i }));

    const calls = mockReportEvent.mock.calls.filter((c) => c[0] === 'auth.method.attempt');
    expect(calls.length).toBe(1);
    const payload = calls[0][1] as Record<string, unknown>;
    expect(payload.v).toBe(1);
    expect(payload.method).toBe('github');
    expect(typeof payload.correlation_id).toBe('string');
    expect(payload.correlation_id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
    );
    expect(['mobile', 'tablet', 'desktop']).toContain(payload.viewport_bucket);
    expect(typeof payload.timestamp).toBe('number');
  });

  it('test_login_emits_attempt_on_google_click', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    vi.spyOn(HTMLFormElement.prototype, 'submit').mockImplementation(() => undefined);

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /continue with google/i })).toBeInTheDocument();
    });

    await user.click(screen.getByRole('button', { name: /continue with google/i }));

    const calls = mockReportEvent.mock.calls.filter((c) => c[0] === 'auth.method.attempt');
    expect(calls.length).toBe(1);
    expect((calls[0][1] as Record<string, unknown>).method).toBe('google');
  });

  it('test_login_emits_success_on_password_session', async () => {
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
      const successCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.success',
      );
      expect(successCalls.length).toBe(1);
    });

    const successCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.success');
    const payload = successCall![1] as Record<string, unknown>;
    expect(payload.v).toBe(1);
    expect(payload.method).toBe('password');
    expect(typeof payload.latency_ms).toBe('number');
    expect(payload.latency_ms as number).toBeGreaterThanOrEqual(0);
  });

  it('test_login_emits_failure_kratos_5xx', async () => {
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({
        ok: false,
        status: 500,
        json: async () => ({}),
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
      const failureCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.failure',
      );
      expect(failureCalls.length).toBe(1);
    });

    const failureCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.failure');
    const payload = failureCall![1] as Record<string, unknown>;
    expect(payload.reason).toBe('kratos_5xx');
    expect(typeof payload.latency_ms).toBe('number');
  });

  it('test_login_emits_failure_kratos_validation', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'Invalid credentials.', type: 'error' }];
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({ ok: true, json: async () => flowWithError });

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.type(screen.getByLabelText('Password'), 'wrong');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      const failureCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.failure',
      );
      expect(failureCalls.length).toBe(1);
    });

    const failureCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.failure');
    expect((failureCall![1] as Record<string, unknown>).reason).toBe('kratos_validation');
  });

  it('test_login_emits_failure_csrf_expired', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'The csrf_token is invalid.', type: 'error' }];
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({ ok: true, json: async () => flowWithError });

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.type(screen.getByLabelText('Password'), 'secret');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      const failureCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.failure',
      );
      expect(failureCalls.length).toBe(1);
    });

    const failureCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.failure');
    expect((failureCall![1] as Record<string, unknown>).reason).toBe('csrf_expired');
  });

  it('test_login_emits_failure_network', async () => {
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockRejectedValueOnce(new Error('Network down'));

    const user = userEvent.setup();
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.type(screen.getByLabelText('Password'), 'secret');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      const failureCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.failure',
      );
      expect(failureCalls.length).toBe(1);
    });

    const failureCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.failure');
    expect((failureCall![1] as Record<string, unknown>).reason).toBe('network');
  });

  it('test_login_attempt_and_outcome_share_correlation_id', async () => {
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
    await user.type(screen.getByLabelText('Password'), 'secret');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      const successCalls = mockReportEvent.mock.calls.filter(
        (c) => c[0] === 'auth.method.success',
      );
      expect(successCalls.length).toBe(1);
    });

    const attemptCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.attempt');
    const outcomeCall = mockReportEvent.mock.calls.find((c) => c[0] === 'auth.method.success');
    const attemptId = (attemptCall![1] as Record<string, unknown>).correlation_id;
    const outcomeId = (outcomeCall![1] as Record<string, unknown>).correlation_id;
    expect(attemptId).toBe(outcomeId);
  });

  it('test_login_payload_pii_free', async () => {
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

    await user.type(screen.getByLabelText('Email'), 'sensitive@example.com');
    await user.type(screen.getByLabelText('Password'), 'never-leak-me');
    await user.click(screen.getByRole('button', { name: /^sign in$/i }));

    await waitFor(() => {
      expect(mockReportEvent.mock.calls.length).toBeGreaterThanOrEqual(2);
    });

    const forbiddenKeys = [
      'email',
      'identifier',
      'password',
      'session',
      'user_id',
      'tenant_id',
      'referrer',
    ];
    for (const call of mockReportEvent.mock.calls) {
      const payload = call[1] as Record<string, unknown>;
      for (const key of forbiddenKeys) {
        expect(payload).not.toHaveProperty(key);
      }
      // Stringified payload must not contain the actual values
      const serialized = JSON.stringify(payload);
      expect(serialized).not.toContain('sensitive@example.com');
      expect(serialized).not.toContain('never-leak-me');
    }
  });

  // =========================================================================
  // T3.1 — Passkey autofill (G8)
  // =========================================================================

  it('test_login_email_autocomplete_includes_webauthn', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<LoginPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    const emailInput = screen.getByLabelText('Email') as HTMLInputElement;
    expect(emailInput.getAttribute('autocomplete')).toBe('email webauthn');
  });
});

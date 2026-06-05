import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen, waitFor } from '@/test/render';

vi.mock('@/lib/config', () => ({
  API_URL: 'http://kratos.local',
  AUTH_URL: '',
  ENGINE_URL: '',
  ACCOUNTS_URL: '',
}));

const mockGetFlow = vi.fn();
const mockSubmitFlow = vi.fn();
const mockInitFlow = vi.fn();
// Use REAL enrichKratosError so EC-2 D8 mapping (identifier_similarity) applies.
vi.mock('@/lib/kratos', async () => {
  const actual = await vi.importActual<typeof import('@/lib/kratos')>('@/lib/kratos');
  return {
    ...actual,
    getFlow: (...args: unknown[]) => mockGetFlow(...args),
    submitFlow: (...args: unknown[]) => mockSubmitFlow(...args),
    initFlow: (...args: unknown[]) => mockInitFlow(...args),
    // enrichKratosError, getCsrfToken, getFlowMessages, getFieldErrors come from actual
  };
});

import { RecoveryPage } from './recovery';

function emailPhaseFlow() {
  return {
    id: 'rec-flow-1',
    ui: {
      action: 'http://kratos.local/self-service/recovery?flow=rec-flow-1',
      method: 'POST',
      nodes: [
        { attributes: { name: 'csrf_token', value: 'csrf-rec' } },
        { attributes: { name: 'email', type: 'email' } },
      ],
      messages: [],
    },
  };
}

function codePhaseFlow() {
  return {
    id: 'rec-flow-1',
    ui: {
      action: 'http://kratos.local/self-service/recovery?flow=rec-flow-1',
      method: 'POST',
      nodes: [
        { attributes: { name: 'csrf_token', value: 'csrf-rec' } },
        { attributes: { name: 'code', type: 'text' } },
      ],
      messages: [],
    },
  };
}

// J-04 fixtures: password phase (after passed_challenge → initFlow('settings'))
function passwordPhaseFlow() {
  return {
    id: 'set-flow-1',
    ui: {
      action: 'http://kratos.local/self-service/settings?flow=set-flow-1',
      method: 'POST',
      nodes: [
        { attributes: { name: 'csrf_token', value: 'csrf-set' } },
        { attributes: { name: 'password', type: 'password' } },
      ],
      messages: [],
    },
  };
}

function passwordPhaseFlowWithError(errorText: string) {
  return {
    id: 'set-flow-1',
    ui: {
      action: 'http://kratos.local/self-service/settings?flow=set-flow-1',
      method: 'POST',
      nodes: [
        { attributes: { name: 'csrf_token', value: 'csrf-set' } },
        {
          attributes: { name: 'password', type: 'password' },
          messages: [{ text: errorText, type: 'error' }],
        },
      ],
      messages: [],
    },
  };
}

describe('RecoveryPage', () => {
  beforeEach(() => {
    // resetAllMocks clears BOTH call history AND queued mockResolvedValueOnce
    // implementations — prevents test pollution from leaked queued mocks.
    vi.resetAllMocks();
  });

  it('test_recovery_renders_email_input', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /send recovery code/i })).toBeInTheDocument();
    expect(screen.getByText('Reset your password')).toBeInTheDocument();
  });

  it('test_recovery_submit_triggers_send', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce(codePhaseFlow());

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Email'), 'user@example.com');
    await user.click(screen.getByRole('button', { name: /send recovery code/i }));

    await waitFor(() => {
      expect(mockSubmitFlow).toHaveBeenCalledTimes(1);
    });
    const [action, method, body] = mockSubmitFlow.mock.calls[0];
    expect(action).toBe('http://kratos.local/self-service/recovery?flow=rec-flow-1');
    expect(method).toBe('POST');
    expect(body.email).toBe('user@example.com');
    expect(body.method).toBe('code');
    expect(body.csrf_token).toBe('csrf-rec');
  });

  it('test_recovery_success_state_after_send', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    // After passed_challenge, initFlow('settings') is called for password phase
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => {
      expect(screen.getByLabelText('Recovery code')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));

    // Phase transitions to 'password' (not 'success') after code verified
    await waitFor(() => {
      expect(screen.getByLabelText(/new password/i)).toBeInTheDocument();
    });
  });

  // =========================================================================
  // T1.1 — Password phase tests (6 RED — Phase 2 will make GREEN)
  // =========================================================================

  it('test_recovery_password_phase_renders_password_input', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));

    await waitFor(() => {
      expect(screen.getByLabelText(/new password/i)).toBeInTheDocument();
    });
    expect(screen.getByLabelText(/confirm password/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /update password/i })).toBeInTheDocument();
  });

  it('test_recovery_password_min_length_validation', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    // Kratos rejects with min_length error on submit
    mockSubmitFlow.mockResolvedValueOnce(
      passwordPhaseFlowWithError('Password must be at least 10 characters.'),
    );

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());

    await user.type(screen.getByLabelText(/new password/i), 'short1');
    await user.type(screen.getByLabelText(/confirm password/i), 'short1');
    await user.click(screen.getByRole('button', { name: /update password/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/password must be at least 10 characters/i),
      ).toBeInTheDocument();
    });
  });

  it('test_recovery_password_submit_success', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'success' });

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());

    await user.type(screen.getByLabelText(/new password/i), 'StrongPass!12');
    await user.type(screen.getByLabelText(/confirm password/i), 'StrongPass!12');
    await user.click(screen.getByRole('button', { name: /update password/i }));

    await waitFor(() => {
      expect(screen.getByText(/password reset successful/i)).toBeInTheDocument();
    });
    expect(screen.getByRole('link', { name: /^sign in$/i })).toBeInTheDocument();
  });

  it('test_recovery_password_submit_kratos_error', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce(
      passwordPhaseFlowWithError('Password too weak.'),
    );

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());

    await user.type(screen.getByLabelText(/new password/i), 'StrongPass!12');
    await user.type(screen.getByLabelText(/confirm password/i), 'StrongPass!12');
    await user.click(screen.getByRole('button', { name: /update password/i }));

    await waitFor(() => {
      expect(screen.getByText(/password too weak/i)).toBeInTheDocument();
    });
  });

  // BUG fix 2026-06-05 (Ralph J-04 loop): Kratos returns HTTP 422 with
  // `redirect_browser_to: /settings?flow=<new>` when code is verified, not
  // `state: passed_challenge`. The original logic interpreted this as error.
  it('test_recovery_code_verified_via_browser_location_change_422', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    // Kratos 422 response shape — no `state`, has `redirect_browser_to`
    mockSubmitFlow.mockResolvedValueOnce({
      error: {
        id: 'browser_location_change_required',
        message: 'browser location change required',
        reason:
          'In order to complete this flow please redirect the browser to: https://app-dev.loopzero.example/settings?flow=set-flow-via-redirect',
      },
      redirect_browser_to:
        'https://app-dev.loopzero.example/settings?flow=set-flow-via-redirect',
    });
    // getFlow for the settings flow ID extracted from redirect_browser_to
    mockGetFlow.mockResolvedValueOnce(passwordPhaseFlow());

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '714962');
    await user.click(screen.getByRole('button', { name: /verify code/i }));

    // Should transition to password phase (not stay in code with error)
    await waitFor(() => {
      expect(screen.getByLabelText(/new password/i)).toBeInTheDocument();
    });
    // getFlow called with extracted flow ID
    expect(mockGetFlow).toHaveBeenCalledWith('settings', 'set-flow-via-redirect');
  });

  // EC-2: identifier similarity check mapping
  it('test_recovery_password_similarity_check_shows_product_language', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce(
      passwordPhaseFlowWithError('the password is too similar to the user identifier'),
    );

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());

    await user.type(screen.getByLabelText(/new password/i), 'StrongPass!12');
    await user.type(screen.getByLabelText(/confirm password/i), 'StrongPass!12');
    await user.click(screen.getByRole('button', { name: /update password/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/doesn't contain your email or username/i),
      ).toBeInTheDocument();
    });
    // EC-2 Rule 3: NO mention of internal "identifier similarity" / Kratos in UI
    expect(screen.queryByText(/identifier similarity/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/kratos/i)).not.toBeInTheDocument();
  });

  // EC-5: double-click race condition
  it('test_recovery_password_submit_disabled_during_request', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    // Slow submit so the disabled state is observable between clicks
    let resolveSubmit: (v: { state: string }) => void = () => {};
    mockSubmitFlow.mockImplementationOnce(
      () => new Promise((res) => { resolveSubmit = res; }),
    );

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());

    await user.type(screen.getByLabelText(/new password/i), 'StrongPass!12');
    await user.type(screen.getByLabelText(/confirm password/i), 'StrongPass!12');
    const submitBtn = screen.getByRole('button', { name: /update password/i });
    await user.click(submitBtn);
    // Second click while first is in-flight
    await user.click(submitBtn);

    // Resolve the original
    resolveSubmit({ state: 'success' });
    await waitFor(() => {
      expect(screen.getByText(/password reset successful/i)).toBeInTheDocument();
    });

    // submitFlow called only 2 times total: 1 for code phase, 1 for password phase
    // (the second click was guarded by disabled={loading})
    expect(mockSubmitFlow).toHaveBeenCalledTimes(2);
  });

  // =========================================================================
  // T1.2 — OIDC educational link tests (2 RED — Phase 3 will make GREEN)
  // =========================================================================

  it('test_recovery_email_phase_shows_oidc_educational_link', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());

    const githubLink = screen.getByRole('link', { name: /sign in with github/i });
    expect(githubLink).toHaveAttribute('href', '/login?provider=github');
    const googleLink = screen.getByRole('link', { name: /sign in with google/i });
    expect(googleLink).toHaveAttribute('href', '/login?provider=google');
  });

  it('test_recovery_success_phase_shows_oidc_educational_link', async () => {
    mockGetFlow.mockResolvedValueOnce(codePhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'passed_challenge' });
    mockInitFlow.mockResolvedValueOnce(passwordPhaseFlow());
    mockSubmitFlow.mockResolvedValueOnce({ state: 'success' });

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));
    await waitFor(() => expect(screen.getByLabelText(/new password/i)).toBeInTheDocument());
    await user.type(screen.getByLabelText(/new password/i), 'StrongPass!12');
    await user.type(screen.getByLabelText(/confirm password/i), 'StrongPass!12');
    await user.click(screen.getByRole('button', { name: /update password/i }));

    await waitFor(() => expect(screen.getByText(/password reset successful/i)).toBeInTheDocument());

    const githubLink = screen.getByRole('link', { name: /sign in with github/i });
    expect(githubLink).toHaveAttribute('href', '/login?provider=github');
    const googleLink = screen.getByRole('link', { name: /sign in with google/i });
    expect(googleLink).toHaveAttribute('href', '/login?provider=google');
  });

  // =========================================================================
  // T1.3 — Resend cooldown tests (2 RED — Phase 3 will make GREEN)
  // =========================================================================

  describe('cooldown', () => {
    beforeEach(() => {
      vi.useFakeTimers({ shouldAdvanceTime: true });
    });
    afterEach(() => {
      vi.useRealTimers();
    });

    it('test_recovery_resend_cooldown_60s', async () => {
      mockGetFlow.mockResolvedValueOnce(codePhaseFlow());

      render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
      await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());

      const resendBtn = screen.getByRole('button', { name: /resend/i });
      // Click — should start cooldown
      resendBtn.click();

      await waitFor(() => {
        expect(screen.getByText(/resend in 60s/i)).toBeInTheDocument();
      });
      expect(resendBtn).toBeDisabled();

      // Advance 30s
      vi.advanceTimersByTime(30_000);
      await waitFor(() => {
        expect(screen.getByText(/resend in 3\ds/i)).toBeInTheDocument();
      });

      // Advance another 30s — cooldown ends
      vi.advanceTimersByTime(31_000);
      await waitFor(() => {
        expect(resendBtn).not.toBeDisabled();
      });
    });

    it('test_recovery_cooldown_uses_wall_clock_not_tick_count', async () => {
      mockGetFlow.mockResolvedValueOnce(codePhaseFlow());

      // Spy on Date.now to simulate browser tab throttle: setInterval fires only
      // 2 times but wall-clock advances 35s
      const realNow = Date.now;
      let mockedNow = realNow();
      vi.spyOn(Date, 'now').mockImplementation(() => mockedNow);

      render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
      await waitFor(() => expect(screen.getByLabelText('Recovery code')).toBeInTheDocument());

      const resendBtn = screen.getByRole('button', { name: /resend/i });
      resendBtn.click();

      // Advance wall-clock 35s but tick only 2x (1s each)
      mockedNow += 35_000;
      vi.advanceTimersByTime(2_000);

      await waitFor(() => {
        // EC-6: shown value MUST be based on wall-clock (25s remaining) NOT tick count (58s would indicate broken impl)
        expect(screen.getByText(/resend in 25s/i)).toBeInTheDocument();
      });

      vi.spyOn(Date, 'now').mockRestore();
    });
  });

  // =========================================================================
  // T1.4 — Edge case tests (6 RED — Phase 4 will make GREEN)
  // =========================================================================

  // EC-7 + D5: stale flow → silent reinit (410)
  it('test_recovery_expired_flow_410_silent_reinit', async () => {
    mockGetFlow.mockRejectedValueOnce(new Error('Gone: 410'));
    mockInitFlow.mockResolvedValueOnce(emailPhaseFlow());

    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=stale-id'] });
    await waitFor(() => expect(mockInitFlow).toHaveBeenCalledWith('recovery'));
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());
  });

  // EC-7: stale flow 404 → silent reinit (same as 410)
  it('test_recovery_stale_flow_404_silent_reinit', async () => {
    mockGetFlow.mockRejectedValueOnce(new Error('Not Found: 404'));
    mockInitFlow.mockResolvedValueOnce(emailPhaseFlow());

    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=stale-uuid'] });
    await waitFor(() => expect(mockInitFlow).toHaveBeenCalledWith('recovery'));
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());
  });

  // EC-3 + D5b: cascading failure (Kratos completely down)
  it('test_recovery_cascading_failure_kratos_completely_down', async () => {
    mockGetFlow.mockRejectedValueOnce(new Error('Gone: 410'));
    mockInitFlow.mockRejectedValueOnce(new Error('Service Unavailable: 503'));

    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=stale-id'] });
    await waitFor(() => {
      expect(
        screen.getByText(/recovery service temporarily unavailable/i),
      ).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /reload page/i })).toBeInTheDocument();
  });

  // D6: Kratos 5xx → product-language
  it('test_recovery_kratos_5xx_shows_product_language', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    mockSubmitFlow.mockRejectedValueOnce(new Error('Internal Server Error: 500'));

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.click(screen.getByRole('button', { name: /send recovery code/i }));

    await waitFor(() => {
      expect(
        screen.getByText(/recovery service temporarily unavailable/i),
      ).toBeInTheDocument();
    });
    // Rule 3: no Kratos/flow/credential leakage
    expect(screen.queryByText(/kratos/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/credential method/i)).not.toBeInTheDocument();
  });

  // D6 + D7: network failure → Retry button
  it('test_recovery_network_failure_shows_retry_action', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    mockSubmitFlow.mockRejectedValueOnce(new TypeError('Failed to fetch'));

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());
    await user.type(screen.getByLabelText('Email'), 'u@x.com');
    await user.click(screen.getByRole('button', { name: /send recovery code/i }));

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
    });
  });

  // EC-4 + D7: lastSubmit uses CURRENT input on retry, not stale closure
  it('test_recovery_retry_uses_current_input_not_stale_closure', async () => {
    mockGetFlow.mockResolvedValueOnce(emailPhaseFlow());
    mockSubmitFlow.mockRejectedValueOnce(new TypeError('Failed to fetch'));
    mockSubmitFlow.mockResolvedValueOnce(codePhaseFlow());

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => expect(screen.getByLabelText('Email')).toBeInTheDocument());

    // First submit fails with "a@x.com"
    await user.type(screen.getByLabelText('Email'), 'a@x.com');
    await user.click(screen.getByRole('button', { name: /send recovery code/i }));
    await waitFor(() => expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument());

    // User corrects to "b@x.com" and clicks Retry
    await user.clear(screen.getByLabelText('Email'));
    await user.type(screen.getByLabelText('Email'), 'b@x.com');
    await user.click(screen.getByRole('button', { name: /retry/i }));

    await waitFor(() => expect(mockSubmitFlow).toHaveBeenCalledTimes(2));
    // Second call should use CURRENT input "b@x.com" not snapshot "a@x.com"
    const [, , body] = mockSubmitFlow.mock.calls[1];
    expect(body.email).toBe('b@x.com');
  });
});

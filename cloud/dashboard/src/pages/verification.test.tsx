import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@/test/render';

vi.mock('@/lib/config', () => ({
  API_URL: 'http://kratos.local',
}));

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

import { VerificationPage } from './verification';

describe('VerificationPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFetch.mockReset();
  });

  it('test_verification_renders_email_input', async () => {
    // Mount with ?flow=abc (no code) → enters "input" state showing the form.
    render(<VerificationPage />, { initialEntries: ['/verification?flow=abc'] });
    await waitFor(() => {
      expect(screen.getByLabelText('Verification code')).toBeInTheDocument();
    });
    expect(screen.getByRole('button', { name: /^verify$/i })).toBeInTheDocument();
  });

  it('test_verification_submit_triggers_send', async () => {
    // Mount with ?flow=abc&code=123456 → useEffect fires submitCode → 2 fetches
    // (flow lookup + submit). First fetch loads flow JSON; second posts code.
    mockFetch
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ui: {
            action: 'http://kratos.local/self-service/verification?flow=abc',
            nodes: [{ attributes: { name: 'csrf_token', value: 'csrf-xyz' } }],
          },
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ state: 'passed_challenge' }),
      });

    render(<VerificationPage />, {
      initialEntries: ['/verification?flow=abc&code=123456'],
    });

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledTimes(2);
    });
    const [submitUrl, submitOpts] = mockFetch.mock.calls[1];
    expect(submitUrl).toBe('http://kratos.local/self-service/verification?flow=abc');
    const body = JSON.parse(submitOpts.body);
    expect(body.method).toBe('code');
    expect(body.code).toBe('123456');
    expect(body.csrf_token).toBe('csrf-xyz');
  });

  it('test_verification_success_state_after_send', async () => {
    mockFetch
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({
          ui: {
            action: 'http://kratos.local/self-service/verification?flow=abc',
            nodes: [{ attributes: { name: 'csrf_token', value: 'csrf-xyz' } }],
          },
        }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ state: 'passed_challenge' }),
      });

    render(<VerificationPage />, {
      initialEntries: ['/verification?flow=abc&code=999000'],
    });

    await waitFor(() => {
      expect(screen.getByText('Email verified')).toBeInTheDocument();
    });
    expect(screen.getByText(/Your account is now fully active/i)).toBeInTheDocument();
  });
});

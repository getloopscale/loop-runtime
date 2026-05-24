import { describe, it, expect, vi, beforeEach } from 'vitest';
import { userEvent } from '@testing-library/user-event';
import { render, screen, waitFor } from '@/test/render';

vi.mock('@/lib/config', () => ({
  API_URL: 'http://kratos.local',
}));

const mockGetFlow = vi.fn();
const mockSubmitFlow = vi.fn();
vi.mock('@/lib/kratos', () => ({
  getFlow: (...args: unknown[]) => mockGetFlow(...args),
  submitFlow: (...args: unknown[]) => mockSubmitFlow(...args),
  getCsrfToken: (flow: { ui: { nodes: Array<{ attributes: { name?: string; value?: string } }> } }) =>
    flow.ui.nodes.find((n) => n.attributes.name === 'csrf_token')?.attributes.value ?? '',
  getFlowMessages: (flow: { ui: { messages?: Array<{ text: string }> } }) =>
    (flow.ui.messages ?? []).map((m) => m.text),
}));

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

describe('RecoveryPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
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

    const user = userEvent.setup();
    render(<RecoveryPage />, { initialEntries: ['/recovery?flow=rec-flow-1'] });
    await waitFor(() => {
      expect(screen.getByLabelText('Recovery code')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Recovery code'), '123456');
    await user.click(screen.getByRole('button', { name: /verify code/i }));

    await waitFor(() => {
      expect(screen.getByText('Password reset')).toBeInTheDocument();
    });
    expect(screen.getByRole('link', { name: /^sign in$/i })).toBeInTheDocument();
  });
});

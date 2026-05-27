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

const mockFetch = vi.fn();
vi.stubGlobal('fetch', mockFetch);

import { RegisterPage } from './register';

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
    id: 'reg-flow-1',
    ui: {
      action: 'http://kratos.local/self-service/registration?flow=reg-flow-1',
      method: 'POST',
      nodes: [
        { group: 'default', attributes: { name: 'csrf_token', type: 'hidden', value: 'csrf-reg' } },
        { group: 'password', attributes: { name: 'traits.email', type: 'email' } },
        { group: 'password', attributes: { name: 'password', type: 'password' } },
      ],
      messages: [],
    },
  };
}

describe('RegisterPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockFetch.mockReset();
  });

  it('test_register_renders_form_with_kratos_flow', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    render(<RegisterPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByLabelText('Name')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /create account/i })).toBeInTheDocument();
  });

  it('test_register_oidc_form_submit_preserves_vanilla_dom', async () => {
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowFixture() });
    const createElementSpy = vi.spyOn(document, 'createElement');
    const submitStub = vi
      .spyOn(HTMLFormElement.prototype, 'submit')
      .mockImplementation(() => undefined);

    const user = userEvent.setup();
    render(<RegisterPage />);
    await waitFor(() => {
      expect(screen.getByRole('button', { name: /sign up with github/i })).toBeInTheDocument();
    });
    await user.click(screen.getByRole('button', { name: /sign up with github/i }));

    const formCalls = createElementSpy.mock.calls.filter((c) => c[0] === 'form');
    expect(formCalls.length).toBeGreaterThanOrEqual(1);
    expect(submitStub).toHaveBeenCalled();

    createElementSpy.mockRestore();
    submitStub.mockRestore();
  });

  it('test_register_submit_password_posts_traits', async () => {
    mockFetch
      .mockResolvedValueOnce({ ok: true, json: async () => flowFixture() })
      .mockResolvedValueOnce({
        ok: true,
        json: async () => ({ session: { id: 'sess-1' } }),
      });

    const user = userEvent.setup();
    render(<RegisterPage />);
    await waitFor(() => {
      expect(screen.getByLabelText('Email')).toBeInTheDocument();
    });

    await user.type(screen.getByLabelText('Name'), 'Alice');
    await user.type(screen.getByLabelText('Email'), 'alice@x.com');
    await user.type(screen.getByLabelText('Password'), 'longpassword123');
    await user.click(screen.getByRole('button', { name: /create account/i }));

    await waitFor(() => {
      expect(mockFetch).toHaveBeenCalledTimes(2);
    });
    const [submitUrl, submitOpts] = mockFetch.mock.calls[1];
    expect(submitUrl).toBe('http://kratos.local/self-service/registration?flow=reg-flow-1');
    const body = JSON.parse(submitOpts.body);
    expect(body.method).toBe('password');
    expect(body['traits.email']).toBe('alice@x.com');
    expect(body['traits.name']).toBe('Alice');
    expect(body.password).toBe('longpassword123');
    expect(body.csrf_token).toBe('csrf-reg');
  });

  it('test_register_displays_kratos_errors', async () => {
    const flowWithError = flowFixture();
    flowWithError.ui.messages = [{ text: 'Email already exists.', type: 'error' }];
    mockFetch.mockResolvedValueOnce({ ok: true, json: async () => flowWithError });
    render(<RegisterPage />);
    await waitFor(() => {
      expect(screen.getByText('Email already exists.')).toBeInTheDocument();
    });
  });
});

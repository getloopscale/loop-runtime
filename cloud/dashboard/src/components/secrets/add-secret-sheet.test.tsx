import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { AddSecretSheet } from './add-secret-sheet';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
  mockedEngineFetch.mockResolvedValue({});
});

describe('AddSecretSheet — J-22 T3.2', () => {
  it('test_default_envs_preselected_d4', () => {
    render(
      <Wrapper>
        <AddSecretSheet open onOpenChange={vi.fn()} defaultEnvs={['production', 'staging']} />
      </Wrapper>,
    );
    const prod = screen.getByLabelText('Scope to production') as HTMLInputElement;
    const stg = screen.getByLabelText('Scope to staging') as HTMLInputElement;
    expect(prod.checked).toBe(true);
    expect(stg.checked).toBe(true);
  });

  it('test_invalid_key_shows_error_no_post', async () => {
    render(
      <Wrapper>
        <AddSecretSheet open onOpenChange={vi.fn()} />
      </Wrapper>,
    );
    // Single-char name fails the pattern (needs 2-64 chars). Auto-uppercase
    // means we can't test lowercase; use char count instead.
    fireEvent.change(screen.getByLabelText(/^Key$/i), { target: { value: 'A' } });
    fireEvent.change(screen.getByLabelText(/^Value$/i), { target: { value: 'x' } });
    fireEvent.click(screen.getByRole('button', { name: /add secret/i }));
    await screen.findAllByText(/SCREAMING_SNAKE_CASE/i);
    expect(mockedEngineFetch).not.toHaveBeenCalled();
  });

  it('test_valid_submit_posts_canonical_payload_with_selected_envs', async () => {
    const onOpenChange = vi.fn();
    const onCreated = vi.fn();
    render(
      <Wrapper>
        <AddSecretSheet
          open
          onOpenChange={onOpenChange}
          defaultEnvs={['production']}
          availableEnvs={['production', 'staging']}
          project="teste"
          onCreated={onCreated}
        />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/^Key$/i), { target: { value: 'API_KEY' } });
    fireEvent.change(screen.getByLabelText(/^Value$/i), { target: { value: 'sk_live_x' } });
    fireEvent.click(screen.getByLabelText('Scope to staging')); // add staging too

    fireEvent.click(screen.getByRole('button', { name: /add secret/i }));

    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    // Routes through /bulk to avoid the singular-`environment` schema
    // mismatch on /api/v1/secrets POST (LIVE catch 2026-06-10).
    expect(url).toBe('/api/v1/secrets/bulk');
    expect(init?.method).toBe('POST');
    const body = JSON.parse(init?.body as string);
    expect(body.project).toBe('teste');
    expect(body.secrets).toHaveLength(1);
    expect(body.secrets[0].name).toBe('API_KEY');
    expect(body.secrets[0].value).toBe('sk_live_x');
    expect(body.secrets[0].envs).toEqual(['production', 'staging']);

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onCreated).toHaveBeenCalled();
  });
});

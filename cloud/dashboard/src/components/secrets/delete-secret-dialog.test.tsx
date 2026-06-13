import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { DeleteSecretDialog } from './delete-secret-dialog';
import type { Secret } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const SECRET: Secret = {
  name: 'DATABASE_URL',
  environments: ['production'],
} as Secret;

beforeEach(() => {
  vi.clearAllMocks();
  mockedEngineFetch.mockResolvedValue({});
});

describe('DeleteSecretDialog — J-22 T3.4', () => {
  it('test_delete_button_disabled_until_exact_match_d3', () => {
    render(
      <Wrapper>
        <DeleteSecretDialog open onOpenChange={vi.fn()} secret={SECRET} />
      </Wrapper>,
    );
    const del = screen.getByRole('button', { name: /^Delete$/i });
    expect(del).toBeDisabled();
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'DATABASE_URL' },
    });
    expect(del).not.toBeDisabled();
  });

  it('test_case_mismatch_keeps_button_disabled_ec19', () => {
    render(
      <Wrapper>
        <DeleteSecretDialog open onOpenChange={vi.fn()} secret={SECRET} />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'database_url' },
    });
    expect(screen.getByRole('button', { name: /^Delete$/i })).toBeDisabled();
  });

  it('test_confirmed_delete_invokes_engine_delete_with_env_and_project', async () => {
    const onOpenChange = vi.fn();
    const onDeleted = vi.fn();
    render(
      <Wrapper>
        <DeleteSecretDialog
          open
          onOpenChange={onOpenChange}
          secret={SECRET}
          environment="staging"
          project="teste"
          onDeleted={onDeleted}
        />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'DATABASE_URL' },
    });
    fireEvent.click(screen.getByRole('button', { name: /^Delete$/i }));

    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/DATABASE_URL?environment=staging&project=teste');
    expect(init?.method).toBe('DELETE');

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onDeleted).toHaveBeenCalled();
  });
});

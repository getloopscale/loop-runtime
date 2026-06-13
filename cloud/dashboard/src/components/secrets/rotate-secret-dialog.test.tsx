import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { RotateSecretDialog } from './rotate-secret-dialog';
import type { Secret } from '@/lib/api/types';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function Wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

const SECRET: Secret & { version: number } = {
  name: 'DATABASE_URL',
  environments: ['production'],
  version: 3,
} as unknown as Secret & { version: number };

beforeEach(() => {
  vi.clearAllMocks();
  mockedEngineFetch.mockResolvedValue({});
});

describe('RotateSecretDialog — J-22 T3.3', () => {
  it('test_rotate_button_disabled_until_exact_match_d3', () => {
    render(
      <Wrapper>
        <RotateSecretDialog open onOpenChange={vi.fn()} secret={SECRET} />
      </Wrapper>,
    );
    const rotate = screen.getByRole('button', { name: /^Rotate$/i });
    expect(rotate).toBeDisabled();
    // Partial match keeps disabled
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'DATABASE_URL' }, // exact name; missing newValue still keeps disabled
    });
    expect(rotate).toBeDisabled();
    // Add value
    fireEvent.change(screen.getByLabelText(/New value/), { target: { value: 'new' } });
    expect(rotate).not.toBeDisabled();
  });

  it('test_case_mismatch_keeps_button_disabled_ec19', () => {
    render(
      <Wrapper>
        <RotateSecretDialog open onOpenChange={vi.fn()} secret={SECRET} />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'database_url' }, // lowercase paste — EC-19 forbids match
    });
    fireEvent.change(screen.getByLabelText(/New value/), { target: { value: 'new' } });
    expect(screen.getByRole('button', { name: /^Rotate$/i })).toBeDisabled();
  });

  it('test_rotate_carries_expected_version_ec13', async () => {
    const onOpenChange = vi.fn();
    const onRotated = vi.fn();
    render(
      <Wrapper>
        <RotateSecretDialog
          open
          onOpenChange={onOpenChange}
          secret={SECRET}
          project="teste"
          onRotated={onRotated}
        />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/Type.*to confirm/), {
      target: { value: 'DATABASE_URL' },
    });
    fireEvent.change(screen.getByLabelText(/New value/), { target: { value: 'rotated-x' } });
    fireEvent.click(screen.getByRole('button', { name: /^Rotate$/i }));

    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/DATABASE_URL');
    expect(init?.method).toBe('PUT');
    const body = JSON.parse(init?.body as string);
    expect(body.value).toBe('rotated-x');
    expect(body.expected_version).toBe(3); // ← EC-13 invariant
    expect(body.environment).toBe('production');
    expect(body.project).toBe('teste');

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onRotated).toHaveBeenCalled();
  });
});

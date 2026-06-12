import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

vi.mock('@/lib/config', () => ({ API_URL: '' }));

import { BulkUploadSheet } from './bulk-upload-sheet';
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
  mockedEngineFetch.mockResolvedValue({
    results: [
      { name: 'KEY_A', status: 'created', sync_status: 'pending' },
      { name: 'KEY_B', status: 'created', sync_status: 'pending' },
    ],
  });
});

describe('BulkUploadSheet — J-22 T3.5', () => {
  it('test_preview_5_valid_1_malformed_plan_invariant', () => {
    render(
      <Wrapper>
        <BulkUploadSheet open onOpenChange={vi.fn()} project="teste" />
      </Wrapper>,
    );
    const src = [
      'KEY_A=a',
      'KEY_B=b',
      'this-is-malformed',
      'KEY_C=c',
      'KEY_D=d',
      'KEY_E=e',
    ].join('\n');
    fireEvent.change(screen.getByLabelText(/.env content/), { target: { value: src } });
    // Plan T3.5 TDD: "preview MUST show 5 entries + 1 warning"
    expect(screen.getByText(/Preview: 5 entries · 1 warnings/)).toBeInTheDocument();
  });

  it('test_value_column_masked_d1', () => {
    render(
      <Wrapper>
        <BulkUploadSheet open onOpenChange={vi.fn()} project="teste" />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/.env content/), {
      target: { value: 'DATABASE_URL=secret-postgres-url' },
    });
    // Preview row MUST mask the value (D1); the raw value lives in the
    // textarea legitimately (user typed it). We only check the preview.
    expect(screen.getByText(/DATABASE_URL=••••••••/)).toBeInTheDocument();
  });

  it('test_submit_posts_to_bulk_endpoint_only_valid_entries', async () => {
    const onOpenChange = vi.fn();
    const onUploaded = vi.fn();
    render(
      <Wrapper>
        <BulkUploadSheet
          open
          onOpenChange={onOpenChange}
          project="teste"
          defaultEnvs={['production']}
          availableEnvs={['production', 'staging']}
          onUploaded={onUploaded}
        />
      </Wrapper>,
    );
    fireEvent.change(screen.getByLabelText(/.env content/), {
      target: { value: ['KEY_A=a', 'malformed', 'KEY_B=b'].join('\n') },
    });
    fireEvent.click(screen.getByRole('button', { name: /Upload 2 secret/i }));

    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalled());
    const [url, init] = mockedEngineFetch.mock.calls[0];
    expect(url).toBe('/api/v1/secrets/bulk');
    expect(init?.method).toBe('POST');
    const body = JSON.parse(init?.body as string);
    expect(body.project).toBe('teste');
    expect(body.secrets).toEqual([
      { name: 'KEY_A', value: 'a', envs: ['production'] },
      { name: 'KEY_B', value: 'b', envs: ['production'] },
    ]);

    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false));
    expect(onUploaded).toHaveBeenCalledWith(2);
  });
});

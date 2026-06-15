import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/api', () => ({ engineFetch: vi.fn() }));

import { ReverifyButton, __test_REVERIFY_ELIGIBLE_STATUSES } from './reverify-button';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function wrap(node: ReactElement): ReactElement {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('ReverifyButton (J-11 v1.1 T2.3)', () => {
  it('test_reverify_button_hidden_for_active_status (ADR D3)', () => {
    const { container } = render(
      wrap(<ReverifyButton domainName="loopzero-test.example" status="active" />),
    );
    expect(container.querySelector('button')).toBeNull();
  });

  it('test_reverify_button_hidden_for_provisioning_status', () => {
    const { container } = render(
      wrap(<ReverifyButton domainName="loopzero-test.example" status="provisioning" />),
    );
    expect(container.querySelector('button')).toBeNull();
  });

  it('test_reverify_button_hidden_for_removing_status', () => {
    const { container } = render(
      wrap(<ReverifyButton domainName="loopzero-test.example" status="removing" />),
    );
    expect(container.querySelector('button')).toBeNull();
  });

  it('test_reverify_button_renders_for_failed_status', () => {
    render(wrap(<ReverifyButton domainName="loopzero-test.example" status="failed" />));
    expect(screen.getByRole('button', { name: /Re-verify loopzero-test\.example/i })).toBeInTheDocument();
  });

  it('test_reverify_button_double_click_fires_once (EC-4 absorption)', async () => {
    // Slow mutation — engineFetch never resolves so isPending stays true,
    // letting us assert the debounce.
    mockedEngineFetch.mockImplementation(() => new Promise(() => {}));
    render(wrap(<ReverifyButton domainName="loopzero-test.example" status="failed" />));
    const button = screen.getByRole('button', { name: /Re-verify loopzero-test\.example/i });
    // Rapid double-click within the debounce window.
    fireEvent.click(button);
    fireEvent.click(button);
    fireEvent.click(button);
    // engineFetch is called ONCE — subsequent clicks blocked by isPending.
    await waitFor(() => expect(mockedEngineFetch).toHaveBeenCalledTimes(1));
    expect(button).toBeDisabled();
  });

  it('test_eligible_status_set_locked_to_failed_and_verified (ADR D3 + Degraded approx)', () => {
    expect(__test_REVERIFY_ELIGIBLE_STATUSES.has('failed')).toBe(true);
    expect(__test_REVERIFY_ELIGIBLE_STATUSES.has('verified')).toBe(true);
    expect(__test_REVERIFY_ELIGIBLE_STATUSES.size).toBe(2);
  });
});

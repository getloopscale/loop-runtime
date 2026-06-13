import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { ReactNode } from 'react';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import { DeleteEnvironmentDialog } from './delete-environment-dialog';
import { engineFetch } from '@/lib/api';

const mockedEngineFetch = vi.mocked(engineFetch);

function withClient(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0, staleTime: 0 },
      mutations: { retry: false },
    },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe('DeleteEnvironmentDialog', () => {
  it('test_confirm_button_disabled_until_exact_name_match', async () => {
    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="qa"
        />,
      ),
    );

    const confirmButton = screen.getByRole('button', { name: /Delete qa/i });
    expect(confirmButton).toBeDisabled();

    const input = screen.getByLabelText(/Type/i);
    await userEvent.type(input, 'q');
    expect(confirmButton).toBeDisabled();

    await userEvent.type(input, 'a');
    expect(confirmButton).toBeEnabled();
  });

  it('test_confirm_disabled_for_case_mismatch', async () => {
    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="QA"
        />,
      ),
    );

    const confirmButton = screen.getByRole('button', { name: /Delete QA/i });
    const input = screen.getByLabelText(/Type/i);
    await userEvent.type(input, 'qa');
    expect(confirmButton).toBeDisabled();
  });

  it('test_cascade_warning_shows_workloads_secrets_domains', () => {
    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="qa"
          cascade={{ workloads: 3, secrets: 5, domains: 1 }}
        />,
      ),
    );

    expect(screen.getByText(/3 workloads/i)).toBeInTheDocument();
    expect(screen.getByText(/5 secrets/i)).toBeInTheDocument();
    expect(screen.getByText(/1 domain/i)).toBeInTheDocument();
  });

  it('test_aria_describedby_links_input_to_instruction', () => {
    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="qa"
        />,
      ),
    );

    const input = screen.getByLabelText(/Type/i);
    const helpId = input.getAttribute('aria-describedby');
    expect(helpId).toBeTruthy();
    const help = document.getElementById(helpId!);
    expect(help).toBeInTheDocument();
    expect(help?.textContent).toMatch(/case-sensitive/i);
  });

  it('test_calls_delete_engineFetch_with_url_encoded_name', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'qa' });
    const onDeleted = vi.fn();

    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="qa"
          onDeleted={onDeleted}
        />,
      ),
    );

    const input = screen.getByLabelText(/Type/i);
    await userEvent.type(input, 'qa');
    fireEvent.click(screen.getByRole('button', { name: /Delete qa/i }));

    await waitFor(() => {
      expect(mockedEngineFetch).toHaveBeenCalledWith(
        '/api/v1/environments/qa',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
    expect(onDeleted).toHaveBeenCalled();
  });

  // M2 v1.2 T5.3 (2026-06-07): when the page passes `project`, the
  // DELETE URL MUST include `?project=<slug>` so the backend filters
  // by the (name, project) tuple. Without this, the M2 cap of
  // 5 envs/project is mis-interpreted (the wrong tuple gets removed).
  it('test_M2_delete_url_includes_project_query', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'staging', project: 'web' });
    const onDeleted = vi.fn();

    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="staging"
          project="web"
          onDeleted={onDeleted}
        />,
      ),
    );

    const input = screen.getByLabelText(/Type/i);
    await userEvent.type(input, 'staging');
    fireEvent.click(screen.getByRole('button', { name: /Delete staging/i }));

    await waitFor(() => {
      expect(mockedEngineFetch).toHaveBeenCalledWith(
        '/api/v1/environments/staging?project=web',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
    expect(onDeleted).toHaveBeenCalled();
  });

  // M2 v1.2 T5.3: undefined/empty `project` preserves the legacy URL
  // (production legacy entry resolves via project-agnostic lookup).
  it('test_M2_delete_url_omits_query_when_project_empty', async () => {
    mockedEngineFetch.mockResolvedValue({ deleted: 'production' });

    render(
      withClient(
        <DeleteEnvironmentDialog
          open
          onOpenChange={() => undefined}
          envName="production"
        />,
      ),
    );

    const input = screen.getByLabelText(/Type/i);
    await userEvent.type(input, 'production');
    fireEvent.click(screen.getByRole('button', { name: /Delete production/i }));

    await waitFor(() => {
      expect(mockedEngineFetch).toHaveBeenCalledWith(
        '/api/v1/environments/production',
        expect.objectContaining({ method: 'DELETE' }),
      );
    });
    // Ensure no `?project=` leaked into the URL (legacy path).
    const [url] = mockedEngineFetch.mock.calls[0]!;
    expect(url).not.toMatch(/\?project=/);
  });
});

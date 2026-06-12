import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi, beforeEach } from 'vitest';
import type { ReactNode } from 'react';

vi.mock('@/lib/api', () => ({
  engineFetch: vi.fn(),
}));

import { PromoteDialog } from './promote-dialog';
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

const ENVS = [
  { name: 'production', status: 'active' },
  { name: 'staging', status: 'active' },
  { name: 'qa', status: 'active' },
];

beforeEach(() => {
  vi.clearAllMocks();
});

describe('PromoteDialog', () => {
  it('test_canonical_request_body_uses_from_and_to_not_from_environment', async () => {
    mockedEngineFetch.mockResolvedValue({ from: 'staging', to: 'production' });

    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={ENVS}
          initialFrom="staging"
        />,
      ),
    );

    // Wait for the dialog to render with initialFrom + auto-selected target
    const fromSelect = (await screen.findByLabelText(/From/i)) as HTMLSelectElement;
    expect(fromSelect.value).toBe('staging');

    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    // Auto-selects last in array as target (production = first non-source); since
    // ENVS minus 'staging' = [production, qa], heuristic picks last = 'qa'.
    expect(['production', 'qa']).toContain(toSelect.value);
    // Pick production deterministically
    fireEvent.change(toSelect, { target: { value: 'production' } });

    const submitButton = screen.getByRole('button', { name: /Promote staging → production/i });
    fireEvent.click(submitButton);

    await waitFor(() => {
      expect(mockedEngineFetch).toHaveBeenCalledWith(
        '/api/v1/projects/teste/promote',
        expect.objectContaining({ method: 'POST' }),
      );
    });
    const body = JSON.parse(
      (mockedEngineFetch.mock.calls[0]?.[1] as RequestInit).body as string,
    );
    expect(body).toEqual({ from: 'staging', to: 'production' });
  });

  it('test_single_env_disables_submit_no_promote_target', async () => {
    // When the only env in the list is the source, there's nothing to promote
    // to. Submit must stay disabled.
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={[{ name: 'production', status: 'active' }]}
          initialFrom="production"
        />,
      ),
    );

    const submitButton = screen.getByRole('button', { name: /Promote/i });
    expect(submitButton).toBeDisabled();
  });

  it('test_create_new_sentinel_calls_onCreateRequested', async () => {
    const onCreateRequested = vi.fn();
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={ENVS}
          initialFrom="staging"
          onCreateRequested={onCreateRequested}
        />,
      ),
    );

    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    fireEvent.change(toSelect, { target: { value: '__create_new__' } });
    expect(onCreateRequested).toHaveBeenCalled();
  });

  it('test_empty_envs_list_disables_submit', () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={[]}
        />,
      ),
    );

    const submitButton = screen.getByRole('button', { name: /Promote/i });
    expect(submitButton).toBeDisabled();
  });

  // EC-14 — LIVE catch 2026-06-06 from sponsor manual validation.
  // When the user changes the `From` dropdown to the value currently in
  // the auto-selected `To`, the submit button used to lock silently
  // (from===to → disabled), forcing the user to also know they had to
  // touch the target dropdown. Fix: when from collides with current to,
  // auto-re-pick another candidate as target so the dialog stays usable.
  it('test_changing_from_to_match_target_auto_repicks_target', async () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={ENVS}
          initialFrom="staging"
        />,
      ),
    );

    const fromSelect = (await screen.findByLabelText(/From/i)) as HTMLSelectElement;
    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    expect(fromSelect.value).toBe('staging');
    // Auto-select picked some target ('production' or 'qa'). Capture it.
    const initialTarget = toSelect.value;
    expect(initialTarget).not.toBe('');

    // Force the user-driven collision: change `From` to the same value
    // as the current `To`.
    fireEvent.change(fromSelect, { target: { value: initialTarget } });

    // After the change, both selects must NOT be equal AND the submit
    // button must stay enabled.
    expect(fromSelect.value).toBe(initialTarget);
    expect(toSelect.value).not.toBe(initialTarget);
    const submitButton = screen.getByRole('button', { name: /Promote/i });
    expect(submitButton).toBeEnabled();
  });
});

// ─────────────────────────────────────────────────────────────────────
// T4.4 finding 4.6 + EC-4 absorption: cross-project promote toggle.
// Plan 2026-06-08-dashboard-screens-review-fixes ADR D6 declares
// cross-project promote is allowed but explicit — default-restricted
// to same-project envs with an opt-in checkbox.
// ─────────────────────────────────────────────────────────────────────

describe('PromoteDialog — EC-4 cross-project toggle', () => {
  // Mixed env set: 2 from `teste`, 1 from `other-app`, 1 legacy (no
  // project = backward compat).
  const MIXED_ENVS = [
    { name: 'production', status: 'active', project: 'teste' },
    { name: 'staging', status: 'active', project: 'teste' },
    { name: 'qa', status: 'active', project: 'other-app' },
    { name: 'legacy', status: 'active' /* project undefined */ },
  ];

  it('defaults to same-project envs in the target dropdown', () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={MIXED_ENVS}
          initialFrom="staging"
        />,
      ),
    );

    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    const optionValues = Array.from(toSelect.options).map((o) => o.value);
    // Same-project + legacy (empty project) are in scope.
    expect(optionValues).toContain('production');
    expect(optionValues).toContain('legacy');
    // Other-project env hidden by default.
    expect(optionValues).not.toContain('qa');
  });

  it('toggling "Include other projects" reveals cross-project envs', () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={MIXED_ENVS}
          initialFrom="staging"
        />,
      ),
    );

    const checkbox = screen.getByTestId('promote-cross-project');
    fireEvent.click(checkbox);

    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    const optionValues = Array.from(toSelect.options).map((o) => o.value);
    expect(optionValues).toContain('qa'); // other-project env now visible
  });

  it('renders cross-project warning Alert when target is from another project', () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={MIXED_ENVS}
          initialFrom="staging"
        />,
      ),
    );

    // Toggle on to reveal cross-project envs.
    fireEvent.click(screen.getByTestId('promote-cross-project'));

    // Switch target to `qa` (other-app).
    const toSelect = screen.getByLabelText(/To/i) as HTMLSelectElement;
    fireEvent.change(toSelect, { target: { value: 'qa' } });

    // Warning Alert visible with cross-project copy.
    expect(
      screen.getByText(/promoting across projects rewrites/i),
    ).toBeInTheDocument();
  });

  it('does NOT render cross-project warning when target is same project', () => {
    render(
      withClient(
        <PromoteDialog
          open
          onOpenChange={() => undefined}
          projectName="teste"
          environments={MIXED_ENVS}
          initialFrom="staging"
        />,
      ),
    );

    // Default state, target auto-picked from same-project envs.
    expect(
      screen.queryByText(/promoting across projects rewrites/i),
    ).not.toBeInTheDocument();
  });
});

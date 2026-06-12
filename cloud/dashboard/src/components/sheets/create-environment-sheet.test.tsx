import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent } from '@testing-library/react';
import { render, screen } from '@/test/render';

// engineFetch + useEngineApi mocks. The Sheet hard-depends on both:
//   - useEngineApi('/api/v1/projects') to render the picker
//   - engineFetch POST to create the env
const mockEngineFetch = vi.fn();
vi.mock('@/lib/api', () => ({
  engineFetch: (...args: unknown[]) => mockEngineFetch(...args),
}));

const mockUseEngineApi = vi.fn();
vi.mock('@/hooks/use-api', () => ({
  useEngineApi: () => mockUseEngineApi(),
}));

import { CreateEnvironmentSheet } from './create-environment-sheet';

describe('CreateEnvironmentSheet — M2 v1.2 T5.1', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // EC-2: 0 projects → empty state, no form
  it('test_M2_renders_empty_state_when_no_projects', () => {
    mockUseEngineApi.mockReturnValue({
      data: { projects: [] },
      loading: false,
      error: false,
    });
    render(<CreateEnvironmentSheet open={true} onOpenChange={() => {}} />);
    // Empty-state copy + CTA
    expect(screen.getByText(/No projects yet/i)).toBeInTheDocument();
    expect(screen.getByTestId('create-env-empty-state-cta')).toBeInTheDocument();
    expect(screen.getByText(/Create a project/i)).toBeInTheDocument();
    // The form Submit button is absent in empty state (the picker would
    // be unsatisfiable). Only the Close button remains.
    expect(screen.queryByRole('button', { name: /Create environment/i })).not.toBeInTheDocument();
  });

  // Auto-fill rule: exactly 1 project → the picker is auto-selected.
  it('test_M2_auto_fills_when_single_project', () => {
    mockUseEngineApi.mockReturnValue({
      data: { projects: [{ name: 'web' }] },
      loading: false,
      error: false,
    });
    render(<CreateEnvironmentSheet open={true} onOpenChange={() => {}} />);
    // The Select trigger renders the auto-selected value as the
    // visible text — assert the project name is visible AND the
    // "Only one project — auto-selected." hint is shown.
    expect(screen.getByText('Only one project — auto-selected.')).toBeInTheDocument();
  });

  // ≥2 projects → user picks (NO auto-fill, NO read-only).
  it('test_M2_renders_select_with_multiple_projects', () => {
    mockUseEngineApi.mockReturnValue({
      data: { projects: [{ name: 'web' }, { name: 'api' }] },
      loading: false,
      error: false,
    });
    render(<CreateEnvironmentSheet open={true} onOpenChange={() => {}} />);
    // Picker is enabled (no auto-select hint).
    expect(screen.queryByText('Only one project — auto-selected.')).not.toBeInTheDocument();
    expect(screen.getByTestId('env-project-trigger')).toBeInTheDocument();
  });

  // POST body includes the project field — defends T4.1 contract.
  it('test_M2_post_body_includes_project', async () => {
    mockUseEngineApi.mockReturnValue({
      data: { projects: [{ name: 'web' }] },
      loading: false,
      error: false,
    });
    mockEngineFetch.mockResolvedValue({ name: 'staging', status: 'active' });

    render(<CreateEnvironmentSheet open={true} onOpenChange={() => {}} />);
    // Fill the env name; project is auto-selected via singleProject path.
    const nameInput = screen.getByLabelText('Name');
    fireEvent.change(nameInput, { target: { value: 'staging' } });
    // Submit.
    const submit = screen.getByRole('button', { name: /Create environment/i });
    fireEvent.click(submit);
    // Yield to the event loop so engineFetch is invoked.
    await new Promise((r) => setTimeout(r, 0));
    expect(mockEngineFetch).toHaveBeenCalledWith(
      '/api/v1/environments',
      expect.objectContaining({ method: 'POST' }),
    );
    // Assert the body is the canonical M2 shape {name, project, protected}.
    const [, init] = mockEngineFetch.mock.calls[0]!;
    const body = JSON.parse((init as { body: string }).body);
    expect(body).toMatchObject({ name: 'staging', project: 'web' });
  });
});

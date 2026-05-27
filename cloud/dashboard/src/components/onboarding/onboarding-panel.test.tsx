import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@/test/render';
import { userEvent } from '@testing-library/user-event';

const mockUseOnboardingState = vi.fn();
vi.mock('@/hooks/use-onboarding-state', () => ({
  useOnboardingState: () => mockUseOnboardingState(),
}));
vi.mock('@/lib/rum', () => ({ reportEvent: vi.fn() }));

import { OnboardingPanel } from './onboarding-panel';

function makeHookResult(overrides: Record<string, unknown> = {}) {
  return {
    state: {
      dismissed: false,
      steps: {
        cli_installed: false,
        logged_in: false,
        project_initialized: false,
        deploy_succeeded: false,
        domain_added: false,
      },
    },
    completed: 0,
    total: 5,
    percent: 0,
    dismiss: vi.fn(),
    replay: vi.fn(),
    markStepDone: vi.fn(),
    isVisible: true,
    ...overrides,
  };
}

beforeEach(() => {
  mockUseOnboardingState.mockReset();
});

describe('OnboardingPanel (Sprint 3 T3.2 v2 — floating panel pattern)', () => {
  it('test_renders_collapsed_badge_by_default', () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ completed: 2, percent: 40 }));
    render(<OnboardingPanel />);
    expect(screen.getByLabelText(/2 of 5 done/i)).toBeInTheDocument();
    // Counter chip on badge
    expect(screen.getByText('2/5')).toBeInTheDocument();
  });

  it('test_does_not_render_when_dismissed', () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ isVisible: false }));
    render(<OnboardingPanel />);
    expect(screen.queryByLabelText(/get your app live/i)).not.toBeInTheDocument();
  });

  it('test_expands_to_panel_on_badge_click', async () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ completed: 1 }));
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/1 of 5 done/i));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(screen.getByText('Get your app live')).toBeInTheDocument();
  });

  it('test_collapses_back_to_badge_via_chevron', async () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ completed: 1 }));
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/1 of 5 done/i));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
    await userEvent.click(screen.getByLabelText('Collapse to badge'));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('test_dismiss_button_calls_dismiss_when_expanded', async () => {
    const dismiss = vi.fn();
    mockUseOnboardingState.mockReturnValue(makeHookResult({ completed: 1, dismiss }));
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/1 of 5 done/i));
    await userEvent.click(screen.getByLabelText('Dismiss onboarding'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('test_renders_all_5_steps_when_expanded', async () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult());
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/0 of 5 done/i));
    expect(screen.getByText('Install the LoopZero CLI')).toBeInTheDocument();
    expect(screen.getByText('Sign in')).toBeInTheDocument();
    expect(screen.getByText('Initialize your first project')).toBeInTheDocument();
    expect(screen.getByText('Deploy your first project')).toBeInTheDocument();
    expect(screen.getByText('Add a custom domain')).toBeInTheDocument();
  });

  it('test_cli_commands_visible_for_all_incomplete_steps', async () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult());
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/0 of 5 done/i));
    // All 5 CLI commands visible em panel expanded
    expect(screen.getByText('npm install -g @loopzero/cli')).toBeInTheDocument();
    expect(screen.getByText('loopzero login')).toBeInTheDocument();
    expect(screen.getByText('loopzero init')).toBeInTheDocument();
    expect(screen.getByText('loopzero deploy')).toBeInTheDocument();
    expect(screen.getByText('loopzero domain add app.example.com')).toBeInTheDocument();
  });

  it('test_optional_label_on_domain_step', async () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult());
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/0 of 5 done/i));
    expect(screen.getByText('optional')).toBeInTheDocument();
  });

  it('test_mark_cli_installed_via_override_button', async () => {
    const markStepDone = vi.fn();
    mockUseOnboardingState.mockReturnValue(makeHookResult({ markStepDone }));
    render(<OnboardingPanel />);
    await userEvent.click(screen.getByLabelText(/0 of 5 done/i));
    await userEvent.click(screen.getByRole('button', { name: 'I installed it' }));
    expect(markStepDone).toHaveBeenCalledWith('cli_installed');
  });
});

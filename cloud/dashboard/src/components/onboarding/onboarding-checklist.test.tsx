import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@/test/render';
import { userEvent } from '@testing-library/user-event';

const mockUseOnboardingState = vi.fn();
vi.mock('@/hooks/use-onboarding-state', () => ({
  useOnboardingState: () => mockUseOnboardingState(),
}));
vi.mock('@/lib/rum', () => ({ reportEvent: vi.fn() }));

import { OnboardingChecklist } from './onboarding-checklist';

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

describe('OnboardingChecklist (Sprint 3 T3.2)', () => {
  it('test_renders_5_steps_with_correct_completion_state', () => {
    mockUseOnboardingState.mockReturnValue(
      makeHookResult({
        state: {
          dismissed: false,
          steps: {
            cli_installed: true,
            logged_in: true,
            project_initialized: false,
            deploy_succeeded: false,
            domain_added: false,
          },
        },
        completed: 2,
        percent: 40,
      }),
    );
    render(<OnboardingChecklist />);
    expect(screen.getByText('Install the LoopZero CLI')).toBeInTheDocument();
    expect(screen.getByText('Sign in')).toBeInTheDocument();
    expect(screen.getByText('Initialize your first project')).toBeInTheDocument();
    expect(screen.getByText('2/5')).toBeInTheDocument();
  });

  it('test_dismiss_button_calls_dismiss', async () => {
    const dismiss = vi.fn();
    mockUseOnboardingState.mockReturnValue(makeHookResult({ dismiss }));
    render(<OnboardingChecklist />);
    await userEvent.click(screen.getByLabelText('Dismiss onboarding'));
    expect(dismiss).toHaveBeenCalledTimes(1);
  });

  it('test_does_not_render_when_dismissed', () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ isVisible: false }));
    render(<OnboardingChecklist />);
    expect(screen.queryByText('Get your app live')).not.toBeInTheDocument();
  });

  it('test_does_not_render_when_all_complete', () => {
    mockUseOnboardingState.mockReturnValue(makeHookResult({ isVisible: false, completed: 5 }));
    render(<OnboardingChecklist />);
    expect(screen.queryByText('Get your app live')).not.toBeInTheDocument();
  });

  it('test_active_step_shows_cli_command', () => {
    // First incomplete step is cli_installed → should show install command
    mockUseOnboardingState.mockReturnValue(makeHookResult());
    render(<OnboardingChecklist />);
    expect(screen.getByText('npm install -g @loopzero/cli')).toBeInTheDocument();
  });
});

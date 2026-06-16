import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@/test/render';
import { LoginErrorBanner } from './login-error-banner';
import type { KratosErrorMapped } from '@/lib/auth/error-mapper';

// T2.1 — RED specs for LoginErrorBanner. Plan v1.1 Phase 2 Task T2.1.
// Per ADR D1 the upstream error-mapper is a pure function; this banner
// just consumes its KratosErrorMapped tuple.

const buildError = (overrides: Partial<KratosErrorMapped> = {}): KratosErrorMapped => ({
  message: 'Something went wrong.',
  suggestion: 'Try again later.',
  retryable: true,
  ...overrides,
});

describe('LoginErrorBanner', () => {
  it('test_LoginErrorBanner_renders_message_and_suggestion', () => {
    const error = buildError({
      message: 'Your session expired.',
      suggestion: 'Sign in again to continue.',
    });
    render(<LoginErrorBanner error={error} />);
    expect(screen.getByText('Your session expired.')).toBeInTheDocument();
    expect(screen.getByText('Sign in again to continue.')).toBeInTheDocument();
  });

  it('test_LoginErrorBanner_renders_retry_button_when_retryable', () => {
    const error = buildError({ retryable: true });
    const onRetry = vi.fn();
    render(<LoginErrorBanner error={error} onRetry={onRetry} />);
    const button = screen.getByRole('button', { name: /try again/i });
    expect(button).toBeInTheDocument();
  });

  it('test_LoginErrorBanner_omits_retry_button_when_not_retryable', () => {
    const error = buildError({ retryable: false });
    render(<LoginErrorBanner error={error} onRetry={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /try again/i })).toBeNull();
  });

  it('test_LoginErrorBanner_uses_aria_alert_role', () => {
    const error = buildError();
    const { container } = render(<LoginErrorBanner error={error} />);
    // Destructive auth-error semantics: role="alert" per WCAG ARIA pattern +
    // .llms.txt Brief #3 reserves role="alert" for destructive intent.
    const alert = container.querySelector('[role="alert"]');
    expect(alert).not.toBeNull();
  });
});

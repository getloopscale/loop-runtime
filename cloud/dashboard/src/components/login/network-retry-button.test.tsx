import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@/test/render';
import { NetworkRetryButton } from './network-retry-button';

// T2.3 — NetworkRetryButton + EC-3 OWASP A01 security absorption.
// `onRetry` receives the SANITIZED return_to (via lib/auth/return-to.ts),
// never the raw query param.

describe('NetworkRetryButton', () => {
  beforeEach(() => {
    vi.stubGlobal('window', {
      location: { origin: 'https://app-dev.loopzero.example' },
    });
  });

  it('test_NetworkRetryButton_invokes_onRetry_on_click', () => {
    const onRetry = vi.fn();
    render(<NetworkRetryButton onRetry={onRetry} returnTo="/projects" />);
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalled();
  });

  it('test_NetworkRetryButton_disabled_during_retry', () => {
    render(<NetworkRetryButton onRetry={vi.fn()} returnTo="/projects" pending />);
    expect(screen.getByRole('button', { name: /try again/i })).toBeDisabled();
  });

  it('test_NetworkRetryButton_preserves_return_to_param', () => {
    const onRetry = vi.fn();
    render(<NetworkRetryButton onRetry={onRetry} returnTo="/projects/my-project" />);
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledWith('/projects/my-project');
  });

  it('test_NetworkRetryButton_sanitizes_javascript_return_to (EC-3 OWASP A01)', () => {
    const onRetry = vi.fn();
    render(<NetworkRetryButton onRetry={onRetry} returnTo="javascript:alert(1)" />);
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledWith('/projects');
    // Critically, NOT called with the dangerous payload.
    expect(onRetry).not.toHaveBeenCalledWith('javascript:alert(1)');
  });

  it('test_NetworkRetryButton_rejects_offorigin_return_to (EC-3 OWASP A01)', () => {
    const onRetry = vi.fn();
    render(<NetworkRetryButton onRetry={onRetry} returnTo="https://attacker.example/phish" />);
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledWith('/projects');
    expect(onRetry).not.toHaveBeenCalledWith('https://attacker.example/phish');
  });

  it('test_NetworkRetryButton_accepts_relative_path_return_to (EC-3 OWASP A01)', () => {
    const onRetry = vi.fn();
    render(<NetworkRetryButton onRetry={onRetry} returnTo="/projects/my-project" />);
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledWith('/projects/my-project');
  });

  it('test_NetworkRetryButton_accepts_sameorigin_absolute_return_to (EC-3 OWASP A01)', () => {
    const onRetry = vi.fn();
    render(
      <NetworkRetryButton
        onRetry={onRetry}
        returnTo="https://app-dev.loopzero.example/billing"
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /try again/i }));
    expect(onRetry).toHaveBeenCalledWith('https://app-dev.loopzero.example/billing');
  });
});

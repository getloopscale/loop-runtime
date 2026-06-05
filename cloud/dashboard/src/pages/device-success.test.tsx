import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen } from '@/test/render';
import { act } from 'react';
import { BEACON_PATH, DeviceSuccessPage } from './device-success';

describe('DeviceSuccessPage', () => {
  let sendBeaconSpy: ReturnType<typeof vi.fn>;
  let closeSpy: ReturnType<typeof vi.fn>;
  let originalSendBeacon: typeof navigator.sendBeacon | undefined;
  let originalMatchMedia: typeof window.matchMedia | undefined;
  let originalClose: typeof window.close;

  beforeEach(() => {
    vi.useFakeTimers();

    // EC-7: jsdom doesn't expose navigator.sendBeacon by default; polyfill spy.
    originalSendBeacon = navigator.sendBeacon;
    sendBeaconSpy = vi.fn(() => true);
    Object.defineProperty(navigator, 'sendBeacon', {
      value: sendBeaconSpy,
      configurable: true,
      writable: true,
    });

    // matchMedia: jsdom doesn't expose it; default mock returns false (no reduced motion).
    originalMatchMedia = window.matchMedia;
    Object.defineProperty(window, 'matchMedia', {
      value: vi.fn((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
      configurable: true,
      writable: true,
    });

    // window.close spy + window.closed flag. Default impl: NOOP (simulates browser block).
    originalClose = window.close;
    closeSpy = vi.fn();
    window.close = closeSpy as unknown as typeof window.close;
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.resetAllMocks();
    if (originalSendBeacon !== undefined) {
      Object.defineProperty(navigator, 'sendBeacon', {
        value: originalSendBeacon,
        configurable: true,
        writable: true,
      });
    }
    if (originalMatchMedia !== undefined) {
      Object.defineProperty(window, 'matchMedia', {
        value: originalMatchMedia,
        configurable: true,
        writable: true,
      });
    }
    window.close = originalClose;
  });

  it('test_device_success_renders_success_state', () => {
    render(<DeviceSuccessPage />);
    expect(screen.getByText('CLI authorized')).toBeInTheDocument();
    expect(
      screen.getByText(/You can close this tab and return to your terminal/i),
    ).toBeInTheDocument();
  });

  it('test_back_link_always_visible', () => {
    render(<DeviceSuccessPage />);
    const link = screen.getByRole('link', { name: /back to dashboard/i });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/');
  });

  it('test_auto_close_fires_after_5s', () => {
    render(<DeviceSuccessPage />);
    expect(closeSpy).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(5000);
    });
    expect(closeSpy).toHaveBeenCalledTimes(1);
  });

  it('test_fallback_visible_when_close_blocked', () => {
    // closeSpy is NOOP — window.closed stays false → after 100ms blockedCheck, fallback shows.
    render(<DeviceSuccessPage />);
    act(() => {
      vi.advanceTimersByTime(5100);
    });
    expect(screen.getByTestId('device-success-fallback')).toBeInTheDocument();
  });

  it('test_countdown_hidden_when_reduced_motion', () => {
    Object.defineProperty(window, 'matchMedia', {
      value: vi.fn((query: string) => ({
        matches: query.includes('reduce'),
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
      configurable: true,
      writable: true,
    });
    render(<DeviceSuccessPage />);
    expect(screen.queryByTestId('device-success-countdown')).not.toBeInTheDocument();
  });

  it('test_back_link_click_cancels_pending_close', () => {
    // EC-6/EC-1: clicking Back before 5s unmounts; timer cleanup must prevent window.close after.
    const { unmount } = render(<DeviceSuccessPage />);
    fireEvent.click(screen.getByRole('link', { name: /back to dashboard/i }));
    unmount();
    act(() => {
      vi.advanceTimersByTime(6000);
    });
    expect(closeSpy).not.toHaveBeenCalled();
  });

  it('test_beacon_sent_on_mount', () => {
    render(<DeviceSuccessPage />);
    expect(sendBeaconSpy).toHaveBeenCalledTimes(1);
    const [pathArg, bodyArg] = sendBeaconSpy.mock.calls[0];
    expect(pathArg).toBe(BEACON_PATH);
    const parsed = JSON.parse(bodyArg as string);
    expect(parsed.event).toBe('device_success_viewed');
    expect(typeof parsed.timestamp).toBe('number');
  });

  it('test_beacon_payload_no_pii', () => {
    render(<DeviceSuccessPage />);
    const [, bodyArg] = sendBeaconSpy.mock.calls[0];
    const raw = String(bodyArg);
    expect(raw).not.toMatch(/device_code/i);
    expect(raw).not.toMatch(/user_code/i);
    expect(raw).not.toMatch(/access_token/i);
    expect(raw).not.toMatch(/refresh_token/i);
  });

  it('test_beacon_absence_does_not_throw', () => {
    Object.defineProperty(navigator, 'sendBeacon', {
      value: undefined,
      configurable: true,
      writable: true,
    });
    expect(() => render(<DeviceSuccessPage />)).not.toThrow();
  });
});

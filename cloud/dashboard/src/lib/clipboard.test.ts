import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest';
import { copyToClipboard } from './clipboard';

describe('copyToClipboard (J-19 T2.2 — R6 SHOULD + EC-3 fallback)', () => {
  beforeEach(() => {
    // jsdom does not implement document.execCommand — install a no-op
    // before spies can attach.
    if (typeof document.execCommand !== 'function') {
      Object.defineProperty(document, 'execCommand', {
        value: () => false,
        configurable: true,
        writable: true,
      });
    }
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('test_copy_uses_navigator_clipboard_when_available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText },
      configurable: true,
    });
    const ok = await copyToClipboard('hello');
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(ok).toBe(true);
  });

  it('test_copy_falls_back_to_execCommand_when_clipboard_undefined_EC3', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: undefined,
      configurable: true,
    });
    const execCommandSpy = vi.spyOn(document, 'execCommand').mockReturnValue(true);
    const ok = await copyToClipboard('fallback text');
    expect(execCommandSpy).toHaveBeenCalledWith('copy');
    expect(ok).toBe(true);
  });

  it('test_copy_returns_false_when_execCommand_fails', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      value: undefined,
      configurable: true,
    });
    vi.spyOn(document, 'execCommand').mockReturnValue(false);
    const ok = await copyToClipboard('no luck');
    expect(ok).toBe(false);
  });
});

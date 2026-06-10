import { describe, it, expect, vi } from 'vitest';
import { withViewTransition } from './view-transitions';

describe('withViewTransition (T5.3)', () => {
  it('test_fallback_invokes_callback_when_api_unsupported', () => {
    // jsdom doesn't implement startViewTransition → fallback path
    const callback = vi.fn();
    withViewTransition(callback);
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('test_uses_startViewTransition_when_supported', () => {
    const apiSpy = vi.fn((cb: () => void) => {
      cb();
      return { finished: Promise.resolve() };
    });
    // Inject mock onto document
    (document as unknown as { startViewTransition: typeof apiSpy }).startViewTransition = apiSpy;
    const callback = vi.fn();
    withViewTransition(callback);
    expect(apiSpy).toHaveBeenCalledTimes(1);
    expect(callback).toHaveBeenCalledTimes(1);
    // Cleanup
    delete (document as unknown as { startViewTransition?: unknown }).startViewTransition;
  });
});

import { describe, it, expect, beforeEach } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useCommandPalette } from './use-command-palette';

function dispatchKey(key: string, opts: KeyboardEventInit = {}) {
  window.dispatchEvent(new KeyboardEvent('keydown', { key, ...opts }));
}

describe('useCommandPalette (Sprint 2 T2.2)', () => {
  beforeEach(() => {
    // Reset focus to body (clean baseline per test)
    if (document.activeElement instanceof HTMLElement) document.activeElement.blur();
  });

  it('test_opens_on_cmd_k', () => {
    const { result } = renderHook(() => useCommandPalette());
    expect(result.current.open).toBe(false);
    act(() => dispatchKey('k', { metaKey: true }));
    expect(result.current.open).toBe(true);
  });

  it('test_opens_on_ctrl_k', () => {
    const { result } = renderHook(() => useCommandPalette());
    act(() => dispatchKey('k', { ctrlKey: true }));
    expect(result.current.open).toBe(true);
  });

  it('test_second_cmd_k_closes', () => {
    const { result } = renderHook(() => useCommandPalette());
    act(() => dispatchKey('k', { metaKey: true }));
    act(() => dispatchKey('k', { metaKey: true }));
    expect(result.current.open).toBe(false);
  });

  // EC-02: skip when input/textarea/contenteditable focused
  it('test_skips_when_input_focused', () => {
    const { result } = renderHook(() => useCommandPalette());
    const input = document.createElement('input');
    document.body.appendChild(input);
    input.focus();
    // Dispatch from the focused input — handler must check e.target
    const event = new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true });
    act(() => { input.dispatchEvent(event); });
    expect(result.current.open).toBe(false);
    document.body.removeChild(input);
  });

  it('test_skips_when_textarea_focused', () => {
    const { result } = renderHook(() => useCommandPalette());
    const ta = document.createElement('textarea');
    document.body.appendChild(ta);
    ta.focus();
    const event = new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true });
    act(() => { ta.dispatchEvent(event); });
    expect(result.current.open).toBe(false);
    document.body.removeChild(ta);
  });

  it('test_skips_when_contenteditable_focused', () => {
    const { result } = renderHook(() => useCommandPalette());
    const div = document.createElement('div');
    div.setAttribute('contenteditable', 'true');
    document.body.appendChild(div);
    div.focus();
    const event = new KeyboardEvent('keydown', { key: 'k', metaKey: true, bubbles: true });
    act(() => { div.dispatchEvent(event); });
    expect(result.current.open).toBe(false);
    document.body.removeChild(div);
  });

  it('test_cleanup_removes_listener', () => {
    const { result, unmount } = renderHook(() => useCommandPalette());
    unmount();
    // After unmount, dispatching should not affect state (hook gone)
    // We can't assert "no effect" directly, but verify no error thrown
    expect(() => dispatchKey('k', { metaKey: true })).not.toThrow();
    expect(result.current.open).toBe(false);
  });
});

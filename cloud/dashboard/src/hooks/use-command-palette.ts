import { useEffect, useState } from 'react';

/**
 * useCommandPalette (Sprint 2 T2.2) — global ⌘K / Ctrl+K handler.
 *
 * EC-02 fix: skip when target is INPUT / TEXTAREA / contenteditable so
 * keystrokes inside forms aren't stolen. Without this guard, user typing
 * "look at this code" with Cmd held would accidentally open the palette
 * mid-sentence — very annoying bug.
 *
 * Cleanup ensures listener is removed on unmount (multiple mounts won't
 * register duplicate handlers).
 */
export function useCommandPalette(): {
  open: boolean;
  setOpen: (open: boolean | ((prev: boolean) => boolean)) => void;
} {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    function handler(e: KeyboardEvent) {
      // EC-02: ignore when user is typing in input/textarea/contenteditable.
      // Check both isContentEditable property AND attribute fallback (jsdom
      // doesn't always derive isContentEditable from runtime property setter).
      const t = e.target as HTMLElement | null;
      if (
        t &&
        (
          t.tagName === 'INPUT' ||
          t.tagName === 'TEXTAREA' ||
          t.isContentEditable ||
          t.getAttribute?.('contenteditable') === 'true'
        )
      ) {
        return;
      }
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault();
        setOpen((prev) => !prev);
      }
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  return { open, setOpen };
}

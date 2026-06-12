import '@testing-library/jest-dom/vitest';
import { afterEach } from 'vitest';
import { cleanup } from '@testing-library/react';

// Auto-cleanup DOM after each test to prevent state bleeding.
// Vitest 4.x with @testing-library/react v16+ does NOT auto-cleanup by default.
afterEach(() => {
  cleanup();
});

// Polyfill window.matchMedia for jsdom (used by Radix media queries internally).
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  });
}

// Polyfill ResizeObserver for jsdom (used by cmdk internally — Sprint 2 T2.1).
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
}

// Polyfill Element.scrollIntoView for jsdom (used by cmdk to follow active item).
if (typeof Element !== 'undefined' && !Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function () {};
}

// Polyfill PointerEvent methods for jsdom (Radix Select / DropdownMenu use them).
// J-19 T1.1: TimeRangePicker uses @loopzerokit/ui Select (radix-ui based) — its
// pointerdown handler invokes hasPointerCapture which jsdom does not implement.
if (typeof Element !== 'undefined') {
  if (!(Element.prototype as { hasPointerCapture?: unknown }).hasPointerCapture) {
    (Element.prototype as { hasPointerCapture: (pid: number) => boolean }).hasPointerCapture =
      function () {
        return false;
      };
  }
  if (!(Element.prototype as { releasePointerCapture?: unknown }).releasePointerCapture) {
    (Element.prototype as { releasePointerCapture: (pid: number) => void }).releasePointerCapture =
      function () {};
  }
  if (!(Element.prototype as { setPointerCapture?: unknown }).setPointerCapture) {
    (Element.prototype as { setPointerCapture: (pid: number) => void }).setPointerCapture =
      function () {};
  }
}

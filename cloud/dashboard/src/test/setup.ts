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

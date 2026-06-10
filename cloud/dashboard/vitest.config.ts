import { defineConfig } from 'vitest/config';
import path from 'path';

/**
 * Vitest config — separate from vite.config.ts so production build doesn't
 * load test setup.
 *
 * jsdom environment provides window, document, etc. — required because
 * src/lib/config.ts reads from window.__LOOPZERO_CONFIG__ and gets transitively
 * imported by other lib/ modules.
 */
export default defineConfig({
  test: {
    environment: 'jsdom',
    globals: false,
    include: ['src/**/*.test.{ts,tsx}'],
    setupFiles: ['./src/test/setup.ts'],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
});

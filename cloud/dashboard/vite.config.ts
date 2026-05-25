import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { visualizer } from 'rollup-plugin-visualizer';
import path from 'path';

/**
 * Vite config with manualChunks vendor split (T-H.3 final optimization).
 *
 * Splits @loopzero/ui + Radix + Lucide into dedicated vendor chunk so the
 * browser caches it independently of app code. Reduces re-download cost
 * on subsequent visits + parallelizes parsing.
 *
 * Initial paint cost stays the same (browser fetches both chunks) but
 * after first visit, design-system code is cached separately from app
 * code that changes more frequently.
 */
export default defineConfig({
  plugins: [
    react(),
    // T-DR-5.5 v1.2 (Phase 5) — Bundle analyzer per-route attribution.
    // Outputs dist/bundle-stats.html on every build (open via npm run bundle:analyze).
    // Resolves G5 FAANG gap (Vercel bundle analyzer parity).
    visualizer({
      filename: 'dist/bundle-stats.html',
      template: 'treemap',
      gzipSize: true,
      brotliSize: true,
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/@loopzero/ui')) return 'loopzero-ui';
          if (id.includes('node_modules/@radix-ui')) return 'radix';
          if (id.includes('node_modules/lucide-react')) return 'lucide';
          if (id.includes('node_modules/react/') || id.includes('node_modules/react-dom/')) return 'react-vendor';
          if (id.includes('node_modules/react-router')) return 'router';
          // All other vendor code → general vendor chunk
          if (id.includes('node_modules/')) return 'vendor';
        },
      },
    },
  },
});

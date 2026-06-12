/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { VitePWA } from 'vite-plugin-pwa';
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
    // T0.2: React Compiler (auto-memoization). React 19 GA, babel-plugin@1.0.
    // Auto-memoiza components + hooks sem useMemo/useCallback/memo() manuais.
    // Build-time only, zero runtime overhead. Existing 6 manual memos convivem.
    react({
      babel: {
        plugins: [
          ['babel-plugin-react-compiler', {}],
        ],
      },
    }),
    // T-DR-5.5 v1.2 (Phase 5) — Bundle analyzer per-route attribution.
    // Outputs dist/bundle-stats.html on every build (open via npm run bundle:analyze).
    // Resolves G5 FAANG gap (Vercel bundle analyzer parity).
    visualizer({
      filename: 'dist/bundle-stats.html',
      template: 'treemap',
      gzipSize: true,
      brotliSize: true,
    }),
    // T4.2: Service Worker via vite-plugin-pwa + Workbox.
    // EC-04 critical: auth endpoints MUST NEVER cache — stale token =
    // security hole (user sees logged-in UI but session is expired).
    // skipWaiting + clientsClaim = new build propaga em next page load
    // (without these, SW waits ALL tabs to close — SPA user keeps tab
    // open dias, never sees update).
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'LoopZeroCloud',
        short_name: 'LoopZero',
        description: 'CLI-first managed cloud for monorepo deployments.',
        theme_color: '#09090b',
        background_color: '#09090b',
        display: 'standalone',
        start_url: '/',
        // PWA manifest icons — use the real LoopZero mascot PNGs from public/.
        // Previously this was an inline SVG raio (#3ecf8e), shadowing the
        // brand assets. 192/512 sizes are the Android Chrome standard;
        // `maskable` on 512 lets Android's adaptive-icon system crop safely.
        icons: [
          {
            src: '/android-chrome-192x192.png',
            sizes: '192x192',
            type: 'image/png',
            purpose: 'any',
          },
          {
            src: '/android-chrome-512x512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'any maskable',
          },
          {
            src: '/apple-touch-icon.png',
            sizes: '180x180',
            type: 'image/png',
            purpose: 'any',
          },
        ],
      },
      workbox: {
        // EC-04: NEVER cache auth/session endpoints — would serve stale
        // tokens making user appear logged-in when session is gone.
        navigateFallbackDenylist: [
          /\/api\/v1\/auth\//,
          /\/sessions\//,
          /\/self-service\//,
          /\/oauth2\//,
        ],
        runtimeCaching: [
          // Auth/session — NetworkOnly (no fallback, no cache).
          {
            urlPattern: ({ url }) =>
              /\/api\/v1\/auth\//.test(url.pathname) ||
              /\/sessions\//.test(url.pathname) ||
              /\/self-service\//.test(url.pathname) ||
              /\/oauth2\//.test(url.pathname),
            handler: 'NetworkOnly',
          },
          // Generic API — NetworkFirst with 3s timeout, cache fallback.
          {
            urlPattern: /\/api\/v1\//,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'loopzero-api',
              networkTimeoutSeconds: 3,
              expiration: { maxEntries: 100, maxAgeSeconds: 60 * 60 },
            },
          },
          // Static assets — CacheFirst, 30 days.
          {
            urlPattern: /\.(js|css|woff2?|svg|png|webp)$/,
            handler: 'CacheFirst',
            options: {
              cacheName: 'loopzero-assets',
              expiration: { maxEntries: 200, maxAgeSeconds: 60 * 60 * 24 * 30 },
            },
          },
          // Runtime config — StaleWhileRevalidate (rarely changes but must
          // refresh eventually after rotation).
          {
            urlPattern: /\/__config\.js$/,
            handler: 'StaleWhileRevalidate',
            options: { cacheName: 'loopzero-config' },
          },
        ],
        // skipWaiting + clientsClaim: new SW activates immediately em next
        // page load. Without these, SW waits all tabs close → SPA user com
        // tab aberta nunca recebe update.
        skipWaiting: true,
        clientsClaim: true,
      },
      // Dev: SW disabled (Vite HMR conflicts with caching).
      devOptions: { enabled: false },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  // Dev-only: same-origin against the local loopzero-cloud + Kratos port-forwards
  // via vite proxy (avoids browser CORS). In prod, the dashboard is served
  // from cloud.{domain} (MEET-040). `allowedHosts` whitelists the public
  // Caddy-fronted hostname so vite's DNS-rebinding guard doesn't 403 us.
  server: {
    host: true,
    allowedHosts: ['localhost', 'app.loopzero.example', '.loopzero.example'],
    proxy: {
      '/v1':           { target: 'http://localhost:13002', changeOrigin: true },
      '/engine':       { target: 'http://localhost:13002', changeOrigin: true },
      '/auth':         { target: 'http://localhost:13002', changeOrigin: true },
      '/self-service': { target: 'http://localhost:14433', changeOrigin: true },
      '/sessions':     { target: 'http://localhost:14433', changeOrigin: true },
      '/oauth2':       { target: 'http://localhost:14444', changeOrigin: true },
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
  test: {
    // jsdom provides window/document so libs that touch the DOM at import time
    // (config.ts, BroadcastChannel) run under test (find_cicd_007).
    environment: 'jsdom',
  },
});

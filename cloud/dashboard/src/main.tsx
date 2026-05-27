import { StrictMode, lazy, Suspense } from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClientProvider } from '@tanstack/react-query';
import { LoopZeroUIProvider, violetForge } from '@loopzero/ui';
import App from './app';
import { RouteBoundary } from './components/error/route-boundary';
import { queryClient } from './lib/query-client';
import { initWebVitals } from './lib/rum';
import './index.css';

// DevTools only em dev — dynamic import so Vite tree-shakes em prod.
const ReactQueryDevtools = import.meta.env.DEV
  ? lazy(() =>
      import('@tanstack/react-query-devtools').then((m) => ({
        default: m.ReactQueryDevtools,
      })),
    )
  : null;

// Mock infrastructure — gated by VITE_USE_MOCKS=true via dynamic import so
// Vite tree-shakes the entire mock layer (≈27 KB) out of production builds.
// In dev with VITE_USE_MOCKS=true: full Kratos + /api/v1/* + SSE mocks load.
// In prod (env var absent): the dynamic import is never executed, and the
// statically-resolvable env check lets Vite DCE the if-body to zero bytes.
if (import.meta.env.VITE_USE_MOCKS === 'true') {
  Promise.all([
    import('./lib/dev-mocks'),
    import('./lib/mocks'),
  ]).then(([devMocks, mocks]) => {
    devMocks.installKratosMocks();
    mocks.installAllMocks();
  });
}

// T-DR-5.2 v1.2 (Phase 5) — Web Vitals RUM reporter per ADR D8.
// Dev: console.log LCP/INP/CLS with verdict. Prod: ready to POST to
// /v1/telemetry/web-vitals when backend ships (zero consumer impact today).
initWebVitals();

// T4.2: Service Worker registration via Workbox.
// Prod only — dev has SW disabled in vite.config.ts (HMR conflicts).
// Dynamic import so virtual:pwa-register chunk is tree-shaken when
// not invoked.
if (import.meta.env.PROD) {
  import('virtual:pwa-register').then(({ registerSW }) => {
    registerSW({ immediate: true });
  }).catch(() => {
    // Registration failure is non-fatal — app works without SW.
  });
}

/**
 * LoopZeroUIProvider — canonical entry point per @loopzero/ui API
 * (replaces hand-wired ThemeProvider + Toaster from earlier iterations).
 *
 * Composes <ThemeProvider> + <Toaster> with the right defaults so every
 * composite that depends on theme cascade + toast context works out of
 * the box. Single-theme override (`theme.themes={[violetForge]}`)
 * preserves the bundle-size win (drops 9 theme definitions, ~150KB).
 *
 * Trade-off: cannot switch themes at runtime. Acceptable per D1 (locked
 * single Violet Forge dark) — dashboard never offered theme picker.
 */
// Provider chain (order matters):
//   StrictMode
//     RouteBoundary (T0.1) — catches EXTREMAL crashes (LoopZero/Router init)
//       QueryClientProvider (T1.1) — server state cache, OUTSIDE LoopZeroUI so
//                                    Theme crash não derruba queries
//         LoopZeroUIProvider — design system + toaster
//           App
//             ReactQueryDevtools (dev only) — Cmd-Shift-Q inspect cache
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <RouteBoundary routePath="root">
      <QueryClientProvider client={queryClient}>
        <LoopZeroUIProvider
          theme={{
            themes: [violetForge],
            defaultTheme: 'violet-forge',
            defaultMode: 'dark',
          }}
        >
          <App />
          {ReactQueryDevtools ? (
            <Suspense fallback={null}>
              <ReactQueryDevtools initialIsOpen={false} buttonPosition="bottom-left" />
            </Suspense>
          ) : null}
        </LoopZeroUIProvider>
      </QueryClientProvider>
    </RouteBoundary>
  </StrictMode>,
);

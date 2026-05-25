import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, Toaster, violetForge } from '@loopzero/ui';
import App from './app';
import { installKratosMocks } from './lib/dev-mocks';
import { initWebVitals } from './lib/rum';
import './index.css';

// Install Kratos fetch interceptor for `/self-service/*` paths when
// VITE_USE_MOCKS=true. No-op otherwise. See dev-mocks.ts for guarantees:
// EC-1 (never mutates window.location), EC-7 (recovery phase transition).
installKratosMocks();

// T-DR-5.2 v1.2 (Phase 5) — Web Vitals RUM reporter per ADR D8.
// Dev: console.log LCP/INP/CLS with verdict. Prod: ready to POST to
// /v1/telemetry/web-vitals when backend ships (zero consumer impact today).
initWebVitals();

/**
 * Single-theme provider per D8 ADR (plan v1.2 T-H decision).
 *
 * Was: <LoopZeroUIProvider> (loads builtinThemes = 10 themes).
 * Now: <ThemeProvider themes={[violetForge]}> — single theme, drops 9
 * theme definitions from bundle (~150KB est. saving). Toaster wired
 * manually since we lose LoopZeroUIProvider's composition convenience.
 *
 * Trade-off: cannot switch themes at runtime. Acceptable per D1 (locked
 * single Violet Forge dark) — no UX impact since dashboard never offered
 * theme picker.
 *
 * If multi-theme is ever needed, revert to <LoopZeroUIProvider> (5-line edit).
 */
createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider
      themes={[violetForge]}
      defaultTheme="violet-forge"
      defaultMode="dark"
    >
      <Toaster>
        <App />
      </Toaster>
    </ThemeProvider>
  </StrictMode>,
);

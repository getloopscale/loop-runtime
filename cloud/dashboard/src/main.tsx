import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { ThemeProvider, Toaster, violetForge } from '@loopzero/ui';
import App from './app';
import './index.css';

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

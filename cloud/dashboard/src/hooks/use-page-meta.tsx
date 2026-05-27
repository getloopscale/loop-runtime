import { useEffect, type ReactNode } from 'react';

/**
 * T2.4 + ADR D9: retire Context-based page meta in favor of direct
 * document.title manipulation via useEffect.
 *
 * Why: browser tab title is the only real consumer of pageTitle state.
 * Context API for state propagation was pre-React-19 pattern. In 2026, a
 * single useEffect that sets document.title is simpler — zero state, zero
 * re-render, zero provider.
 *
 * Backwards-compat: 31 existing call sites (useSetPageTitle, usePageMeta,
 * setPageTitle) preserved. PageMetaProvider is a no-op shell so main.tsx
 * and test wrappers don't need to remove it (eventual cleanup safe).
 */

const TITLE_SUFFIX = ' · LoopZeroCloud';

function applyDocumentTitle(title: string): void {
  if (typeof document === 'undefined') return;
  document.title = title ? `${title}${TITLE_SUFFIX}` : 'LoopZeroCloud';
}

/** New canonical API per plan D9 — useEffect that owns document.title. */
export function useDocumentTitle(title: string): void {
  useEffect(() => {
    applyDocumentTitle(title);
  }, [title]);
}

/** Legacy alias preserved — set title on mount, sync to document.title. */
export function useSetPageTitle(title: string): void {
  useDocumentTitle(title);
}

/**
 * Legacy hook preserved as backwards-compat. PageShell calls onTitleChange
 * (fire-and-forget) when title prop renders; the returned setPageTitle just
 * pipes that into document.title.
 */
export function usePageMeta(): { pageTitle: string; setPageTitle: (t: string) => void } {
  return {
    pageTitle: '',
    setPageTitle: (title) => applyDocumentTitle(title),
  };
}

/** No-op shell — no longer wraps Context, but harmless to keep in trees. */
export function PageMetaProvider({ children }: { children: ReactNode }) {
  return <>{children}</>;
}

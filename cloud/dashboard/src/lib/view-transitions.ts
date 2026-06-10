// T5.3: View Transitions API helper (progressive enhancement).
//
// Browsers que suportam (Chrome 111+, Edge 111+, Safari 18+): smooth
// crossfade entre as duas árvores DOM ao navigate.
// Browsers que NÃO suportam (Firefox até 2026): fallback síncrono — chama
// callback imediatamente, zero diferença vs sem este helper.
//
// Zero runtime cost no fallback path. Zero impacto em testes (jsdom não
// implementa startViewTransition, então segue fallback path).

export function withViewTransition(callback: () => void): void {
  if (typeof document !== 'undefined' && typeof document.startViewTransition === 'function') {
    document.startViewTransition(callback);
    return;
  }
  // Fallback: invoke immediately, no transition.
  callback();
}

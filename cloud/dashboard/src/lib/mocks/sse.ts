// Mock EventSource for SSE endpoints — Plan v1.0 T0.3.
//
// Replaces window.EventSource when VITE_USE_MOCKS=true so log streams +
// build progress streams work offline. Per ADR D3, transparent shim — the
// LogViewer / DeploymentDetail consumers use native EventSource API and
// never know they're hitting a mock.
//
// Per-URL emitter pattern:
//   1. Domain handler file registers an emitter for a path pattern via
//      `registerSSE('/api/v1/builds/{id}/logs/stream', (es) => { ... })`.
//   2. When LogViewer does `new EventSource('/api/v1/builds/abc/logs/stream')`,
//      MockEventSource looks up the matching emitter, sets readyState OPEN,
//      and calls the emitter which uses setInterval to dispatch MessageEvents.
//   3. close() stops the interval and sets readyState CLOSED.

type EmitterCleanup = () => void;
export type SSEEmitter = (es: MockEventSource) => EmitterCleanup | void;

interface SSERegistration {
  pattern: RegExp;
  paramNames: string[];
  emitter: SSEEmitter;
}

const registrations: SSERegistration[] = [];

function compilePattern(pathPattern: string): {
  regex: RegExp;
  paramNames: string[];
} {
  const paramNames: string[] = [];
  const regexStr = pathPattern.replace(/\{(\w+)\}/g, (_, name) => {
    paramNames.push(name);
    return '([^/?]+)';
  });
  return { regex: new RegExp(`^${regexStr}$`), paramNames };
}

/** Register an emitter for a given SSE path pattern. */
export function registerSSE(pathPattern: string, emitter: SSEEmitter): void {
  const { regex, paramNames } = compilePattern(pathPattern);
  registrations.push({ pattern: regex, paramNames, emitter });
}

export function resetSSERegistrations(): void {
  registrations.length = 0;
}

export function getSSERegistrationCount(): number {
  return registrations.length;
}

/** Test-friendly EventSource implementation. Mirrors the relevant subset of
 * the WHATWG EventSource API used by the dashboard (readyState constants,
 * onopen/onmessage/onerror/onclose handlers, close() method, dispatchEvent
 * via MessageEvent). Not a full polyfill — fields like `withCredentials`
 * are not implemented because the dashboard doesn't use them. */
export class MockEventSource implements EventTarget {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;

  readonly CONNECTING = 0;
  readonly OPEN = 1;
  readonly CLOSED = 2;

  readyState: number = MockEventSource.CONNECTING;
  url: string;
  withCredentials = false;

  onopen: ((this: EventSource, ev: Event) => unknown) | null = null;
  onmessage: ((this: EventSource, ev: MessageEvent) => unknown) | null = null;
  onerror: ((this: EventSource, ev: Event) => unknown) | null = null;

  private listeners: Map<string, Set<EventListenerOrEventListenerObject>> = new Map();
  private cleanup: EmitterCleanup | null = null;

  constructor(url: string | URL, _init?: { withCredentials?: boolean }) {
    this.url = typeof url === 'string' ? url : url.href;
    const pathname = (() => {
      try {
        return new URL(this.url, 'http://localhost').pathname.replace(
          /^\/engine/,
          '',
        );
      } catch {
        return this.url;
      }
    })();

    const reg = registrations.find((r) => r.pattern.test(pathname));
    if (!reg) {
      // Dispatch error async so consumer has time to attach handlers
      setTimeout(() => {
        this.readyState = MockEventSource.CLOSED;
        this.onerror?.call(this as unknown as EventSource, new Event('error'));
        this.dispatchEvent(new Event('error'));
      }, 0);
      return;
    }

    // Open + invoke emitter on next tick so consumer can subscribe first
    setTimeout(() => {
      if (this.readyState === MockEventSource.CLOSED) return;
      this.readyState = MockEventSource.OPEN;
      this.onopen?.call(this as unknown as EventSource, new Event('open'));
      this.dispatchEvent(new Event('open'));
      const result = reg.emitter(this);
      if (typeof result === 'function') this.cleanup = result;
    }, 0);
  }

  close(): void {
    this.readyState = MockEventSource.CLOSED;
    if (this.cleanup) {
      this.cleanup();
      this.cleanup = null;
    }
  }

  addEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject,
  ): void {
    if (!this.listeners.has(type)) this.listeners.set(type, new Set());
    this.listeners.get(type)!.add(listener);
  }

  removeEventListener(
    type: string,
    listener: EventListenerOrEventListenerObject,
  ): void {
    this.listeners.get(type)?.delete(listener);
  }

  dispatchEvent(event: Event): boolean {
    // Route to type-specific handler
    if (event.type === 'message' && this.onmessage) {
      this.onmessage.call(this as unknown as EventSource, event as MessageEvent);
    }
    const set = this.listeners.get(event.type);
    if (set) {
      for (const listener of set) {
        if (typeof listener === 'function') {
          listener.call(this, event);
        } else {
          listener.handleEvent(event);
        }
      }
    }
    return true;
  }
}

let originalEventSource: typeof EventSource | null = null;

export function isSSEMockingEnabled(): boolean {
  if (typeof window === 'undefined') return false;
  if (typeof import.meta === 'undefined') return false;
  return import.meta.env?.VITE_USE_MOCKS === 'true';
}

export function installSSEMocks(): void {
  if (!isSSEMockingEnabled()) return;
  if (typeof window === 'undefined') return;
  if (originalEventSource !== null) return; // already installed
  originalEventSource = (window as unknown as { EventSource: typeof EventSource })
    .EventSource;
  (window as unknown as { EventSource: unknown }).EventSource = MockEventSource;
}

export function uninstallSSEMocks(): void {
  if (typeof window === 'undefined') return;
  if (originalEventSource === null) return;
  (window as unknown as { EventSource: typeof EventSource }).EventSource =
    originalEventSource;
  originalEventSource = null;
}

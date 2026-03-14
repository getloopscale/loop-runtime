import { ApiError } from './client';

export interface SSEEvent {
  id: string;
  event: string;
  data: Record<string, unknown>;
}

type FetchFn = (url: string, init?: RequestInit) => Promise<Response>;

export interface SSEClientOptions {
  url: string;
  headers?: Record<string, string>;
  onEvent: (event: SSEEvent) => void;
  onError?: (error: Error) => void;
  onClose?: () => void;
  reconnectDelay?: number;
  pollInterval?: number;
  pollUrl?: string;
  fetchFn?: FetchFn;
}

/**
 * SSE client with automatic reconnection, Last-Event-ID replay,
 * deduplication, and fallback to polling on JOURNAL_EXPIRED.
 */
export class SSEClient {
  private lastEventId: string | null = null;
  private closed = false;
  private switchedToPolling = false;
  private abortController: AbortController | null = null;
  private pollTimer: ReturnType<typeof setTimeout> | null = null;

  private readonly url: string;
  private readonly headers: Record<string, string>;
  private readonly onEvent: (event: SSEEvent) => void;
  private readonly onError: (error: Error) => void;
  private readonly onClose: () => void;
  private readonly reconnectDelay: number;
  private readonly pollInterval: number;
  private readonly pollUrl: string | null;
  private readonly fetchFn: FetchFn;

  constructor(options: SSEClientOptions) {
    this.url = options.url;
    this.headers = options.headers ?? {};
    this.onEvent = options.onEvent;
    this.onError = options.onError ?? (() => {});
    this.onClose = options.onClose ?? (() => {});
    this.reconnectDelay = options.reconnectDelay ?? 1000;
    this.pollInterval = options.pollInterval ?? 5000;
    this.pollUrl = options.pollUrl ?? null;
    this.fetchFn = options.fetchFn ?? globalThis.fetch;
  }

  getLastEventId(): string | null {
    return this.lastEventId;
  }

  async connect(): Promise<void> {
    if (this.closed) {
      return;
    }

    await this.startStreaming();
  }

  close(): void {
    this.closed = true;

    if (this.abortController) {
      this.abortController.abort();
      this.abortController = null;
    }

    if (this.pollTimer) {
      clearTimeout(this.pollTimer);
      this.pollTimer = null;
    }

    this.onClose();
  }

  private async startStreaming(): Promise<void> {
    while (!this.closed && !this.switchedToPolling) {
      try {
        await this.connectOnce();

        // If switched to polling or closed, stop the loop
        if (this.switchedToPolling || this.closed) {
          return;
        }

        // Stream ended (server closed connection) — reconnect per SSE spec
        await this.delay(this.reconnectDelay);
      } catch (err) {
        if (this.closed) {
          return;
        }

        this.onError(
          err instanceof Error ? err : new ApiError(String(err), 3, 0),
        );

        // Wait before reconnecting
        await this.delay(this.reconnectDelay);

        if (this.closed) {
          return;
        }
      }
    }
  }

  private async connectOnce(): Promise<void> {
    this.abortController = new AbortController();

    const headers: Record<string, string> = { ...this.headers };
    if (this.lastEventId) {
      headers['Last-Event-ID'] = this.lastEventId;
    }

    const response = await this.fetchFn(this.url, {
      headers,
      signal: this.abortController.signal,
    });

    if (!response.ok) {
      throw new ApiError(
        `SSE connection failed with status ${response.status}`,
        response.status >= 400 && response.status < 500 ? 1 : 3,
        response.status,
      );
    }

    const body = response.body;
    if (!body) {
      throw new ApiError('Response body is null', 3, 0);
    }

    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
      while (true) {
        if (this.closed) {
          return;
        }

        const { done, value } = await reader.read();
        if (done) {
          // Process any remaining buffer
          if (buffer.trim()) {
            this.processBuffer(buffer);
          }
          return;
        }

        buffer += decoder.decode(value, { stream: true });

        // Process complete events (delimited by \n\n)
        const parts = buffer.split('\n\n');
        // Keep the last incomplete part in the buffer
        buffer = parts.pop() ?? '';

        for (const part of parts) {
          if (this.closed || this.switchedToPolling) {
            return;
          }
          const trimmed = part.trim();
          if (trimmed) {
            this.processEvent(trimmed);
          }
        }
      }
    } finally {
      try {
        reader.cancel();
      } catch {
        // Ignore cancel errors on cleanup
      }
    }
  }

  private processBuffer(buffer: string): void {
    const trimmed = buffer.trim();
    if (trimmed) {
      this.processEvent(trimmed);
    }
  }

  private processEvent(block: string): void {
    if (this.closed) {
      return;
    }

    let id = '';
    let event = 'message';
    let dataStr = '';

    const lines = block.split('\n');
    for (const line of lines) {
      if (line.startsWith('id:')) {
        id = line.slice(3).trim();
      } else if (line.startsWith('event:')) {
        event = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        dataStr = line.slice(5).trim();
      }
    }

    // Dedup: skip events with ID <= last seen
    if (id && this.lastEventId && this.shouldSkip(id, this.lastEventId)) {
      return;
    }

    // Update last event ID
    if (id) {
      this.lastEventId = id;
    }

    // Parse data as JSON
    let data: Record<string, unknown>;
    try {
      data = dataStr ? JSON.parse(dataStr) : {};
    } catch {
      data = { raw: dataStr };
    }

    const sseEvent: SSEEvent = { id, event, data };

    // Check for JOURNAL_EXPIRED — switch to polling
    if (event === 'JOURNAL_EXPIRED') {
      this.switchedToPolling = true;
      this.startPolling();
      return;
    }

    this.onEvent(sseEvent);
  }

  private shouldSkip(currentId: string, lastId: string): boolean {
    const current = parseInt(currentId, 10);
    const last = parseInt(lastId, 10);

    if (!isNaN(current) && !isNaN(last)) {
      return current <= last;
    }

    // For non-numeric IDs, only skip exact duplicates
    return currentId === lastId;
  }

  private startPolling(): void {
    if (this.closed) {
      return;
    }

    // Cancel the SSE connection
    if (this.abortController) {
      this.abortController.abort();
      this.abortController = null;
    }

    const url = this.pollUrl ?? this.url;
    this.schedulePoll(url);
  }

  private schedulePoll(url: string): void {
    if (this.closed) {
      return;
    }

    this.pollTimer = setTimeout(async () => {
      if (this.closed) {
        return;
      }

      try {
        const response = await this.fetchFn(url, {
          headers: this.headers,
        });

        if (!response.ok) {
          this.onError(
            new ApiError(
              `Poll request failed with status ${response.status}`,
              response.status >= 400 && response.status < 500 ? 1 : 3,
              response.status,
            ),
          );
        } else {
          const data = await response.json();
          const pollEvent: SSEEvent = {
            id: '',
            event: 'poll.result',
            data: data as Record<string, unknown>,
          };
          this.onEvent(pollEvent);
        }
      } catch (err) {
        if (!this.closed) {
          this.onError(
            err instanceof Error ? err : new ApiError(String(err), 3, 0),
          );
        }
      }

      // Schedule next poll
      if (!this.closed) {
        this.schedulePoll(url);
      }
    }, this.pollInterval);
  }

  private delay(ms: number): Promise<void> {
    return new Promise((resolve) => {
      const timer = setTimeout(resolve, ms);
      if (this.closed) {
        clearTimeout(timer);
        resolve();
      }
    });
  }
}

import { SSEClient, SSEEvent } from '../../../src/api/sse-client';

/**
 * Helper: creates a ReadableStream that emits SSE-formatted text chunks.
 */
function createSSEStream(chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  let index = 0;

  return new ReadableStream<Uint8Array>({
    pull(controller) {
      if (index < chunks.length) {
        controller.enqueue(encoder.encode(chunks[index]));
        index++;
      } else {
        controller.close();
      }
    },
  });
}

function sseResponse(stream: ReadableStream<Uint8Array>): Response {
  return new Response(stream, {
    status: 200,
    headers: { 'Content-Type': 'text/event-stream' },
  });
}

/**
 * Creates an SSEClient that auto-closes after receiving `count` events.
 * Returns the events array and connect() promise.
 */
function createAutoClosingClient(
  opts: Omit<ConstructorParameters<typeof SSEClient>[0], 'onEvent'> & {
    expectedEvents: number;
  }
): { events: SSEEvent[]; connect: () => Promise<void> } {
  const events: SSEEvent[] = [];
  const { expectedEvents, ...rest } = opts;
  const client = new SSEClient({
    ...rest,
    onEvent: (event: SSEEvent) => {
      events.push(event);
      if (events.length >= expectedEvents) {
        client.close();
      }
    },
  });

  return { events, connect: () => client.connect() };
}

describe('SSEClient', () => {
  describe('SSE event parsing', () => {
    it('should correctly parse id/event/data from SSE text stream', async () => {
      const sseText =
        'id: 1\nevent: build.started\ndata: {"build_id":"build-abc123"}\n\n';

      const stream = createSSEStream([sseText]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));

      const { events, connect } = createAutoClosingClient({
        url: 'http://localhost:3001/builds/b1/stream',
        expectedEvents: 1,
        fetchFn: fetchMock,
      });

      await connect();

      expect(events).toHaveLength(1);
      expect(events[0]).toEqual({
        id: '1',
        event: 'build.started',
        data: { build_id: 'build-abc123' },
      });
    });

    it('should parse multiple events from a single chunk', async () => {
      const sseText =
        'id: 1\nevent: build.started\ndata: {"step":"clone"}\n\n' +
        'id: 2\nevent: build.progress\ndata: {"step":"compile"}\n\n';

      const stream = createSSEStream([sseText]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));

      const { events, connect } = createAutoClosingClient({
        url: 'http://localhost:3001/builds/b1/stream',
        expectedEvents: 2,
        fetchFn: fetchMock,
      });

      await connect();

      expect(events).toHaveLength(2);
      expect(events[0].event).toBe('build.started');
      expect(events[1].event).toBe('build.progress');
    });

    it('should parse events split across multiple chunks', async () => {
      const chunk1 = 'id: 1\nevent: build.sta';
      const chunk2 = 'rted\ndata: {"step":"clone"}\n\n';

      const stream = createSSEStream([chunk1, chunk2]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));

      const { events, connect } = createAutoClosingClient({
        url: 'http://localhost:3001/builds/b1/stream',
        expectedEvents: 1,
        fetchFn: fetchMock,
      });

      await connect();

      expect(events).toHaveLength(1);
      expect(events[0].event).toBe('build.started');
    });
  });

  describe('event callback', () => {
    it('should call onEvent for each parsed event', async () => {
      const sseText =
        'id: 10\nevent: build.started\ndata: {"msg":"hello"}\n\n' +
        'id: 11\nevent: build.log\ndata: {"msg":"world"}\n\n';

      const stream = createSSEStream([sseText]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));
      const onEvent = jest.fn();

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => {
          onEvent(event);
          if (onEvent.mock.calls.length === 2) {
            client.close();
          }
        },
        fetchFn: fetchMock,
      });

      await client.connect();

      expect(onEvent).toHaveBeenCalledTimes(2);
      expect(onEvent).toHaveBeenCalledWith(
        expect.objectContaining({ id: '10', event: 'build.started' })
      );
      expect(onEvent).toHaveBeenCalledWith(
        expect.objectContaining({ id: '11', event: 'build.log' })
      );
    });
  });

  describe('Last-Event-ID tracking', () => {
    it('should update lastEventId after receiving events', async () => {
      const sseText =
        'id: 5\nevent: build.started\ndata: {}\n\n' +
        'id: 10\nevent: build.progress\ndata: {}\n\n';

      const stream = createSSEStream([sseText]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => {
          if (event.id === '10') {
            client.close();
          }
        },
        fetchFn: fetchMock,
      });

      await client.connect();

      expect(client.getLastEventId()).toBe('10');
    });
  });

  describe('reconnect with Last-Event-ID', () => {
    it('should reconnect with Last-Event-ID header on connection drop', async () => {
      // First connection: delivers event 5, then stream closes (server drop).
      // Client reconnects with Last-Event-ID. Second connection: delivers event 6.
      const stream1 = createSSEStream([
        'id: 5\nevent: build.started\ndata: {"step":"clone"}\n\n',
      ]);
      const stream2 = createSSEStream([
        'id: 6\nevent: build.progress\ndata: {"step":"compile"}\n\n',
      ]);

      let call = 0;
      const fetchMock = jest.fn().mockImplementation(() => {
        call++;
        if (call === 1) {
          return Promise.resolve(sseResponse(stream1));
        }
        if (call === 2) {
          return Promise.resolve(sseResponse(stream2));
        }
        // Third call: block indefinitely (cancelled by close)
        return new Promise(() => {});
      });

      const events: SSEEvent[] = [];

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => {
          events.push(event);
          if (events.length === 2) {
            client.close();
          }
        },
        reconnectDelay: 10,
        fetchFn: fetchMock,
      });

      await client.connect();

      // Verify reconnect sent Last-Event-ID
      expect(fetchMock).toHaveBeenCalledTimes(2);
      const secondCallInit = fetchMock.mock.calls[1][1] as RequestInit;
      const headers = secondCallInit.headers as Record<string, string>;
      expect(headers['Last-Event-ID']).toBe('5');

      // Verify we got events from both connections
      expect(events).toHaveLength(2);
      expect(events[0].id).toBe('5');
      expect(events[1].id).toBe('6');
    });
  });

  describe('deduplication', () => {
    it('should ignore events with ID less than or equal to last seen', async () => {
      // First stream: events 1-3, then closes. Client reconnects.
      // Second stream: replays 2-3, then new event 4. Dedup skips 2 and 3.
      const stream1 = createSSEStream([
        'id: 1\nevent: a\ndata: {}\n\n' +
          'id: 2\nevent: b\ndata: {}\n\n' +
          'id: 3\nevent: c\ndata: {}\n\n',
      ]);
      const stream2 = createSSEStream([
        'id: 2\nevent: b\ndata: {}\n\n' +
          'id: 3\nevent: c\ndata: {}\n\n' +
          'id: 4\nevent: d\ndata: {}\n\n',
      ]);

      let call = 0;
      const fetchMock = jest.fn().mockImplementation(() => {
        call++;
        if (call === 1) {
          return Promise.resolve(sseResponse(stream1));
        }
        if (call === 2) {
          return Promise.resolve(sseResponse(stream2));
        }
        return new Promise(() => {});
      });

      const events: SSEEvent[] = [];

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => {
          events.push(event);
          if (event.id === '4') {
            client.close();
          }
        },
        reconnectDelay: 10,
        fetchFn: fetchMock,
      });

      await client.connect();

      const ids = events.map((e) => e.id);
      expect(ids).toEqual(['1', '2', '3', '4']);
    });
  });

  describe('JOURNAL_EXPIRED fallback to polling', () => {
    it('should switch to polling when JOURNAL_EXPIRED event is received', async () => {
      jest.useFakeTimers();

      const stream = createSSEStream([
        'id: 1\nevent: build.started\ndata: {"step":"clone"}\n\n' +
          'id: 2\nevent: JOURNAL_EXPIRED\ndata: {}\n\n',
      ]);

      let callCount = 0;
      const combinedFetch = jest.fn().mockImplementation(() => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(sseResponse(stream));
        }
        return Promise.resolve(
          new Response(
            JSON.stringify({
              status: callCount === 2 ? 'building' : 'success',
              build_id: 'b1',
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } }
          )
        );
      });

      const events: SSEEvent[] = [];

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        pollUrl: 'http://localhost:3001/builds/b1',
        pollInterval: 1000,
        onEvent: (event: SSEEvent) => {
          events.push(event);
          if (
            event.event === 'poll.result' &&
            (event.data as Record<string, unknown>).status === 'success'
          ) {
            client.close();
          }
        },
        fetchFn: combinedFetch,
      });

      const connectPromise = client.connect();

      // Allow SSE stream to process
      await flushMicrotasks();

      // First poll
      jest.advanceTimersByTime(1000);
      await flushMicrotasks();

      // Second poll
      jest.advanceTimersByTime(1000);
      await flushMicrotasks();

      await connectPromise.catch(() => {});

      expect(events.some((e) => e.event === 'build.started')).toBe(true);
      expect(events.some((e) => e.event === 'poll.result')).toBe(true);

      jest.useRealTimers();
    });
  });

  describe('close stops processing', () => {
    it('should not emit events after close()', async () => {
      const sseText = 'id: 1\nevent: first\ndata: {}\n\n';

      const stream = createSSEStream([sseText]);
      const fetchMock = jest.fn().mockResolvedValue(sseResponse(stream));
      const events: SSEEvent[] = [];

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => events.push(event),
        fetchFn: fetchMock,
      });

      // Close before connect
      client.close();
      await client.connect().catch(() => {});

      expect(events).toHaveLength(0);
    });

    it('should stop reconnect attempts after close()', async () => {
      const fetchMock = jest.fn().mockRejectedValue(new Error('Network error'));

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: () => {},
        reconnectDelay: 10,
        fetchFn: fetchMock,
      });

      const connectPromise = client.connect();

      // Let the first fetch reject and onError fire
      await flushMicrotasks();

      // Close during the reconnect delay
      client.close();

      // Let everything settle
      await new Promise((r) => setTimeout(r, 50));
      await connectPromise.catch(() => {});

      // Should only have tried once (no reconnect after close)
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });

  describe('connection error triggers reconnect', () => {
    it('should retry after network error with configured delay', async () => {
      const stream = createSSEStream([
        'id: 1\nevent: build.started\ndata: {}\n\n',
      ]);

      let call = 0;
      const customFetch = jest.fn().mockImplementation(() => {
        call++;
        if (call === 1) {
          return Promise.reject(new Error('Network error'));
        }
        if (call === 2) {
          return Promise.resolve(sseResponse(stream));
        }
        // Block further reconnects
        return new Promise(() => {});
      });

      const onError = jest.fn();
      const events: SSEEvent[] = [];

      const client = new SSEClient({
        url: 'http://localhost:3001/builds/b1/stream',
        onEvent: (event: SSEEvent) => {
          events.push(event);
          client.close();
        },
        onError,
        reconnectDelay: 10,
        fetchFn: customFetch,
      });

      await client.connect();

      expect(customFetch).toHaveBeenCalledTimes(2);
      expect(onError).toHaveBeenCalledWith(expect.any(Error));
      expect(events).toHaveLength(1);
    });
  });

  describe('no internal terms', () => {
    it('should not contain forbidden internal terms in source', () => {
      const forbidden = [
        'ArgoCD', 'argocd', 'GitOps', 'gitops-push', 'CRD',
        'tenant_id', 'advisory lock', 'pg_advisory', 'Gitea',
        'MinIO', 'kubebuilder', 'envtest', 'Kaniko',
      ];

      const fs = require('fs');
      const path = require('path');
      const moduleSource = fs.readFileSync(
        path.resolve(__dirname, '../../../src/api/sse-client.ts'),
        'utf-8'
      );
      for (const term of forbidden) {
        expect(moduleSource).not.toContain(term);
      }
    });
  });
});

/**
 * Helper to flush all pending microtasks (Promises).
 */
function flushMicrotasks(): Promise<void> {
  return new Promise((resolve) => {
    jest.requireActual<typeof import('timers')>('timers').setImmediate(resolve);
  });
}

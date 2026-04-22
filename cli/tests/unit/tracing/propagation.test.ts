// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { injectTraceContext } from '../../../src/tracing/propagation';
import { NodeTracerProvider } from '@opentelemetry/sdk-trace-node';
import { InMemorySpanExporter, SimpleSpanProcessor } from '@opentelemetry/sdk-trace-node';
import { context, trace, propagation } from '@opentelemetry/api';
import { W3CTraceContextPropagator } from '@opentelemetry/core';

describe('injectTraceContext', () => {
  it('returns headers unchanged when no active span', () => {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    const result = injectTraceContext(headers);
    expect(result).toBe(headers);
    expect(result['Content-Type']).toBe('application/json');
  });

  it('does not crash with empty headers', () => {
    const headers: Record<string, string> = {};
    expect(() => injectTraceContext(headers)).not.toThrow();
  });

  it('preserves existing headers', () => {
    const headers: Record<string, string> = {
      'Authorization': 'Bearer token123',
      'Correlation-ID': 'abc-def',
    };
    const result = injectTraceContext(headers);
    expect(result['Authorization']).toBe('Bearer token123');
    expect(result['Correlation-ID']).toBe('abc-def');
  });

  describe('with active TracerProvider', () => {
    let provider: NodeTracerProvider;
    let exporter: InMemorySpanExporter;

    beforeEach(() => {
      exporter = new InMemorySpanExporter();
      provider = new NodeTracerProvider();
      provider.addSpanProcessor(new SimpleSpanProcessor(exporter));
      provider.register();
      propagation.setGlobalPropagator(new W3CTraceContextPropagator());
    });

    afterEach(async () => {
      await provider.shutdown();
    });

    it('injects traceparent header when span is active', () => {
      const tracer = trace.getTracer('test');
      tracer.startActiveSpan('test-span', (span) => {
        const headers: Record<string, string> = {};
        injectTraceContext(headers);

        expect(headers['traceparent']).toBeDefined();
        // W3C format: 00-{32 hex traceId}-{16 hex spanId}-{2 hex flags}
        expect(headers['traceparent']).toMatch(
          /^00-[a-f0-9]{32}-[a-f0-9]{16}-[a-f0-9]{2}$/
        );

        span.end();
      });
    });

    it('traceparent contains the correct trace ID from active span', () => {
      const tracer = trace.getTracer('test');
      tracer.startActiveSpan('test-span', (span) => {
        const expectedTraceId = span.spanContext().traceId;
        const expectedSpanId = span.spanContext().spanId;

        const headers: Record<string, string> = {};
        injectTraceContext(headers);

        const traceparent = headers['traceparent'];
        expect(traceparent).toContain(expectedTraceId);
        expect(traceparent).toContain(expectedSpanId);

        span.end();
      });
    });

    it('preserves existing headers when injecting traceparent', () => {
      const tracer = trace.getTracer('test');
      tracer.startActiveSpan('test-span', (span) => {
        const headers: Record<string, string> = {
          'Authorization': 'Bearer token',
          'Content-Type': 'application/json',
        };
        injectTraceContext(headers);

        expect(headers['Authorization']).toBe('Bearer token');
        expect(headers['Content-Type']).toBe('application/json');
        expect(headers['traceparent']).toBeDefined();

        span.end();
      });
    });
  });
});

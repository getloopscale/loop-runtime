// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { initTracing, shutdownTracing, getTracer } from '../../../src/tracing';
import { trace } from '@opentelemetry/api';

describe('tracing', () => {
  afterEach(async () => {
    await shutdownTracing();
    delete process.env.LOOPZERO_TRACING_ENABLED;
    delete process.env.LOOPZERO_OTEL_ENDPOINT;
  });

  it('does not crash when tracing is disabled (default)', () => {
    expect(() => initTracing()).not.toThrow();
    expect(() => getTracer()).not.toThrow();
  });

  it('registers a real TracerProvider when enabled', () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'true';
    process.env.LOOPZERO_OTEL_ENDPOINT = 'http://localhost:4318/v1/traces';
    initTracing();

    // When enabled, the tracer should produce valid (non-zero) trace IDs
    const tracer = trace.getTracer('test-check');
    const span = tracer.startSpan('verify-provider');
    const traceId = span.spanContext().traceId;
    expect(traceId).toMatch(/^[a-f0-9]{32}$/);
    expect(traceId).not.toBe('00000000000000000000000000000000');
    span.end();
  });

  it('initializes when LOOPZERO_TRACING_ENABLED=1', () => {
    process.env.LOOPZERO_TRACING_ENABLED = '1';
    expect(() => initTracing()).not.toThrow();
  });

  it('does not initialize when LOOPZERO_TRACING_ENABLED=false', () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'false';
    expect(() => initTracing()).not.toThrow();
  });

  it('shuts down gracefully even when not initialized', async () => {
    await expect(shutdownTracing()).resolves.not.toThrow();
  });

  it('shuts down gracefully after initialization', async () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'true';
    initTracing();
    await expect(shutdownTracing()).resolves.not.toThrow();
  });

  it('returns a tracer that can create spans with valid IDs', () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'true';
    initTracing();

    const tracer = getTracer();
    expect(tracer).toBeDefined();
    expect(typeof tracer.startSpan).toBe('function');

    const span = tracer.startSpan('test-span');
    expect(span).toBeDefined();
    expect(span.spanContext().traceId).toMatch(/^[a-f0-9]{32}$/);
    expect(span.spanContext().spanId).toMatch(/^[a-f0-9]{16}$/);
    span.end();
  });

  it('getTracer works even without initialization', () => {
    const tracer = getTracer();
    expect(tracer).toBeDefined();
    expect(typeof tracer.startSpan).toBe('function');
  });
});

// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { initTracing, shutdownTracing, getTracer } from '../../../src/tracing';

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

  it('initializes when LOOPZERO_TRACING_ENABLED=true', () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'true';
    process.env.LOOPZERO_OTEL_ENDPOINT = 'http://localhost:4318/v1/traces';
    expect(() => initTracing()).not.toThrow();
  });

  it('initializes when LOOPZERO_TRACING_ENABLED=1', () => {
    process.env.LOOPZERO_TRACING_ENABLED = '1';
    expect(() => initTracing()).not.toThrow();
  });

  it('does not initialize when LOOPZERO_TRACING_ENABLED=false', () => {
    process.env.LOOPZERO_TRACING_ENABLED = 'false';
    // Should be a no-op (no provider created)
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

  it('returns a valid tracer from getTracer', () => {
    const tracer = getTracer();
    expect(tracer).toBeDefined();
    expect(typeof tracer.startSpan).toBe('function');
  });
});

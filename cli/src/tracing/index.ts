// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * OpenTelemetry tracing for LoopZero CLI.
 *
 * Disabled by default (opt-in via LOOPZERO_TRACING_ENABLED=true).
 * When enabled, exports spans to the OTel Collector via OTLP/HTTP.
 * Graceful degradation: if collector is unreachable, spans are silently dropped.
 */

import { NodeTracerProvider } from '@opentelemetry/sdk-trace-node';
import { BatchSpanProcessor } from '@opentelemetry/sdk-trace-node';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { Resource } from '@opentelemetry/resources';
import { ATTR_SERVICE_NAME, ATTR_SERVICE_VERSION } from '@opentelemetry/semantic-conventions';
import { trace, context, SpanStatusCode } from '@opentelemetry/api';

// eslint-disable-next-line @typescript-eslint/no-require-imports
const { version: CLI_VERSION } = require('../../package.json');

let provider: NodeTracerProvider | null = null;

/**
 * Returns true if tracing is enabled via LOOPZERO_TRACING_ENABLED=true.
 * Default: false (opt-in to avoid impacting CLI DX/startup time).
 */
function isEnabled(): boolean {
  const v = process.env.LOOPZERO_TRACING_ENABLED;
  return v === 'true' || v === '1';
}

/**
 * Initialize OpenTelemetry tracing. Call once at CLI startup.
 * No-op if LOOPZERO_TRACING_ENABLED is not set to true.
 */
export function initTracing(): void {
  if (!isEnabled()) return;

  const endpoint = process.env.LOOPZERO_OTEL_ENDPOINT || 'http://localhost:4318/v1/traces';

  const resource = new Resource({
    [ATTR_SERVICE_NAME]: 'loopzero-cli',
    [ATTR_SERVICE_VERSION]: CLI_VERSION,
    'deployment.environment': process.env.LOOPZERO_ENV || 'development',
  });

  const exporter = new OTLPTraceExporter({
    url: endpoint,
    timeoutMillis: 2000,
  });

  provider = new NodeTracerProvider({ resource });
  provider.addSpanProcessor(new BatchSpanProcessor(exporter, {
    maxQueueSize: 512,
    scheduledDelayMillis: 1000,
  }));
  provider.register();
}

/**
 * Flush and shut down the tracer provider. Call at CLI exit.
 * Safe to call even if tracing was not initialized.
 */
export async function shutdownTracing(): Promise<void> {
  if (provider) {
    await provider.shutdown().catch(() => { /* ignore shutdown errors */ });
    provider = null;
  }
}

/**
 * Get the CLI tracer instance.
 */
export function getTracer() {
  return trace.getTracer('loopzero-cli', CLI_VERSION);
}

export { trace, context, SpanStatusCode };

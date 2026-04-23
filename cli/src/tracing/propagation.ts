// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

/**
 * W3C Trace Context propagation for HTTP requests.
 * Injects traceparent/tracestate headers so CLI spans become parents of API spans.
 */

import { context, propagation } from '@opentelemetry/api';

/**
 * Injects W3C TraceContext headers into an existing headers object.
 * Modifies the headers object in-place and returns it.
 * Safe to call when tracing is disabled -- no-op when no active span.
 */
export function injectTraceContext(headers: Record<string, string>): Record<string, string> {
  propagation.inject(context.active(), headers);
  return headers;
}

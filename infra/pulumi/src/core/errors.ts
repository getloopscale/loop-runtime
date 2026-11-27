// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

/**
 * Typed infrastructure errors with structured context.
 * Every error from the infra layer MUST use these — never bare Error().
 *
 * Context fields enable structured logging and debugging in CI pipelines
 * without access to a debugger.
 */

export type InfraErrorCode =
  | "PROVIDER_NOT_IMPLEMENTED"
  | "INVALID_CONFIG"
  | "RESOURCE_CREATION_FAILED"
  | "NODE_CLASS_NOT_SUPPORTED"
  | "BOOTSTRAP_FAILED";

export interface InfraErrorContext {
  readonly provider?: string;
  readonly resource?: string;
  readonly operation?: string;
  readonly [key: string]: string | undefined;
}

export class InfraError extends Error {
  public readonly code: InfraErrorCode;
  public readonly context: InfraErrorContext;
  public readonly cause?: Error;

  constructor(args: {
    message: string;
    code: InfraErrorCode;
    context?: InfraErrorContext;
    cause?: Error;
  }) {
    const prefix = `[${args.code}]`;
    const ctx = args.context;
    const location = ctx
      ? ` (${[ctx.provider, ctx.resource, ctx.operation].filter(Boolean).join("/")})`
      : "";
    super(`${prefix}${location} ${args.message}`);

    this.name = "InfraError";
    this.code = args.code;
    this.context = args.context ?? {};
    this.cause = args.cause;
  }

  toStructuredLog(): Record<string, string | undefined> {
    return {
      error_code: this.code,
      error_message: this.message,
      ...this.context,
      cause: this.cause?.message,
    };
  }
}

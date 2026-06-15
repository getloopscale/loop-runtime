// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Plan: 2026-06-15-code-review-49-findings-remediation T2.1 (R3-F2 BLOCKER).
// Shared types for the specialized loopzero.yaml validators split out from the
// monolithic validateConfig (CC=95) per ADR D2 Strategy pattern.

import type { LoopZeroYaml } from '../loopzero-yaml';

export interface ValidationError {
  field: string;
  message: string;
}

export interface ValidatorResult {
  errors: ValidationError[];
  warnings: string[];
}

export const EMPTY_RESULT: ValidatorResult = Object.freeze({
  errors: [],
  warnings: [],
});

// Re-export the canonical shape so consumers don't need to import from
// the parent module.
export type { LoopZeroYaml };

// AppName + AppSpec narrowed from LoopZeroYaml.apps[name] for validator signatures.
export type AppName = string;
export type AppSpec = LoopZeroYaml['apps'][string];

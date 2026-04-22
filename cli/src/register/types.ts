// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { Command } from 'commander';
import { ConfigStore } from '../config/store';
import { ApiClient } from '../api/client';

/**
 * Shared dependencies passed to each register* function.
 * Keeps the orchestrator (index.ts) as the single place that
 * constructs these, while register files only wire commands.
 */
export interface CommandDeps {
  program: Command;
  configStore: ConfigStore;
  isCI: boolean;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;

  /** Factory: creates an ApiClient with the current token + onUnauthorized. */
  mkApiClient: (baseURL?: string) => ApiClient;

  /** Returns current token from store (may be null). */
  getToken: () => string | null;

  /** Reads global --json flag from program.opts(). */
  getGlobalOpts: () => { json?: boolean };

  /** onUnauthorized callback for ApiClient refresh. */
  handleUnauthorized: () => Promise<string | null>;

  /** Default API base URL. */
  apiUrl: string;

  /** Cloud service base URL. */
  cloudUrl: string;

  /** withTimeout wrapper. */
  withTimeout: <T>(promise: Promise<T>, ms: number, message: string) => Promise<T>;
}

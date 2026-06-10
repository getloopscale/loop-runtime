// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import { promises as fsPromises } from 'fs';
import * as path from 'path';
import { ApiError } from '../api/client';
import { UPLOAD_TIMEOUT_MS } from '../config/constants';

export interface UploadResult {
  commit_sha: string;
  apps: string[];
}

/**
 * Create a Blob from a file by reading it into memory.
 *
 * Plan v1.2 T5.2 (M5) — uses await fs.promises.readFile so the event loop
 * is not blocked while large bundles (up to MAX_BUNDLE_SIZE_BYTES, 500MB)
 * are loaded. openAsBlob (Node 20.8+ streaming) is intentionally still
 * avoided because its lazy file handle can cause intermittent multipart
 * upload failures (422) when the file handle becomes unavailable during
 * fetch. Plan v1.2 T5.3 audited this on Node 22 and the upstream issue is
 * still open as of 2026-06-06 — keep the buffered approach until the
 * Node bug is fixed.
 */
async function fileBlob(filePath: string, contentType: string): Promise<Blob> {
  const data = await fsPromises.readFile(filePath);
  return new Blob([data], { type: contentType });
}

/**
 * Plan B v3.1 T-B.3 (2026-06-05) — `loopzeroYamlPath` parameter renamed to
 * `servicesJsonPath`; multipart form field `loopzero_yaml` renamed to
 * `services_json` per ADR D4. Caller passes the path to the
 * `.loopzero/services.json` emitted by `loopzerokit build` (Plan A v2.2 thin
 * adapter). API-side payload inspection (per blueprint v2 D8) keeps
 * legacy `loopzero_yaml` field rejected with actionable E-009 error during
 * the transition window.
 */
export async function uploadSource(
  baseURL: string,
  token: string | null,
  bundlePath: string,
  servicesJsonPath: string,
): Promise<UploadResult> {
  // Plan v1.2 EC-5 (multipart field order): services_json MUST be appended
  // BEFORE bundle. Node's fetch + FormData preserves insertion order; the
  // API parses via ParseMultipartForm which is technically order-agnostic
  // but downstream debuggers + ADR D9 wire spec rely on this convention.
  const servicesJsonContent = await fsPromises.readFile(servicesJsonPath, 'utf-8');
  const jsonBlob = new Blob([servicesJsonContent], { type: 'application/json' });
  const bundleBlob = await fileBlob(bundlePath, 'application/octet-stream');

  const formData = new FormData();
  formData.append('services_json', jsonBlob, 'services.json');
  formData.append('bundle', bundleBlob, path.basename(bundlePath));

  const headers: Record<string, string> = {};
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  let resp: Response;
  try {
    resp = await fetch(`${baseURL}/api/v1/source/upload`, {
      method: 'POST',
      headers,
      body: formData,
      signal: AbortSignal.timeout(UPLOAD_TIMEOUT_MS),
    });
  } catch (err) {
    const isTimeout =
      (err instanceof DOMException && err.name === 'TimeoutError') ||
      (err instanceof Error && err.name === 'TimeoutError');

    if (isTimeout) {
      // Plan v1.2 T5.1 — preserve cause chain so LOOPZERO_DEBUG can surface it.
      throw new ApiError(
        'Upload timed out. Check your connection and try again.',
        3,
        0,
        'UPLOAD_TIMEOUT',
        err,
      );
    }

    const message = err instanceof Error ? err.message : String(err);
    const cause = err instanceof Error && 'cause' in err
      ? String((err as Error & { cause?: unknown }).cause)
      : '';

    if (
      message.includes('fetch failed') ||
      message.includes('ECONNREFUSED') ||
      cause.includes('ECONNREFUSED')
    ) {
      throw new ApiError(
        'Could not reach the server. Check your connection and try again.',
        3,
        0,
        'CONNECTION_REFUSED',
        err,
      );
    }

    throw new ApiError(
      'Upload failed. Check your connection and try again.',
      3,
      0,
      'UPLOAD_FAILED',
      err,
    );
  }

  if (!resp.ok) {
    let serverMessage: string | null = null;
    let serverCode: string | undefined;
    try {
      const body = (await resp.json()) as { code?: string; message?: string };
      if (body.message) {
        serverMessage = body.message;
      }
      if (body.code) {
        serverCode = body.code;
      }
    } catch {
      // Non-JSON response body — use fallback message
    }

    // Reuse the same exit code mapping as ApiClient.mapError to avoid
    // divergence (previously had a duplicate mapStatusToExitCode here).
    throw ApiError.fromHttpStatus(resp.status, serverMessage, serverCode);
  }

  return resp.json() as Promise<UploadResult>;
}

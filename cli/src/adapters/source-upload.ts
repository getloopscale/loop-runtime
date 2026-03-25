// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

import * as fs from 'fs';
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
 * readFileSync is used instead of openAsBlob (Node 20.8+ streaming)
 * because openAsBlob's lazy file handle can cause intermittent
 * multipart upload failures (422) when the file handle becomes
 * unavailable during fetch. Git bundles are bounded by MAX_BUNDLE_SIZE_BYTES
 * (500MB), which is within memory limits for modern systems.
 */
function fileBlob(filePath: string, contentType: string): Blob {
  const data = fs.readFileSync(filePath);
  return new Blob([data], { type: contentType });
}

export async function uploadSource(
  baseURL: string,
  token: string | null,
  bundlePath: string,
  loopzeroYamlPath: string,
): Promise<UploadResult> {
  const bundleBlob = fileBlob(bundlePath, 'application/octet-stream');

  // loopzero.yaml is always small — readFileSync is fine
  const loopzeroYamlContent = fs.readFileSync(loopzeroYamlPath, 'utf-8');
  const yamlBlob = new Blob([loopzeroYamlContent], { type: 'text/yaml' });

  const formData = new FormData();
  formData.append('bundle', bundleBlob, path.basename(bundlePath));
  formData.append('loopzero_yaml', yamlBlob, 'loopzero.yaml');

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
      throw new ApiError(
        'Upload timed out. Check your connection and try again.',
        3,
        0,
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
      );
    }

    throw new ApiError(
      'Upload failed. Check your connection and try again.',
      3,
      0,
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

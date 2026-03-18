import * as fs from 'fs';
import * as path from 'path';
import { ApiError } from '../api/client';

export interface UploadResult {
  commit_sha: string;
  apps: string[];
}

/**
 * Create a Blob from a file path using streaming when available.
 *
 * Prefers `fs.openAsBlob` (Node 20.8+) which streams file content
 * without loading the entire file into memory. Falls back to
 * `fs.readFileSync` for older Node versions or test environments.
 */
async function fileBlobStreaming(filePath: string, contentType: string): Promise<Blob> {
  try {
    // Node 20.8+ provides openAsBlob which streams file content
    // without loading it entirely into memory — critical for large bundles.
    const nodeFs = await import('node:fs');
    if (typeof nodeFs.openAsBlob === 'function') {
      return await nodeFs.openAsBlob(filePath, { type: contentType });
    }
  } catch {
    // openAsBlob unavailable or file access failed — fall through to readFileSync
  }

  // Fallback: load into memory (works on all Node 20+ versions and in tests)
  const data = fs.readFileSync(filePath);
  return new Blob([data], { type: contentType });
}

export async function uploadSource(
  baseURL: string,
  token: string | null,
  bundlePath: string,
  loopzeroYamlPath: string,
): Promise<UploadResult> {
  const bundleBlob = await fileBlobStreaming(bundlePath, 'application/octet-stream');

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
      signal: AbortSignal.timeout(30_000),
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
    try {
      const body = (await resp.json()) as { code?: string; message?: string };
      if (body.message) {
        serverMessage = body.message;
      }
    } catch {
      // Non-JSON response body — use fallback message
    }

    const exitCode = mapStatusToExitCode(resp.status);
    const fallback = defaultMessageForStatus(resp.status);
    throw new ApiError(serverMessage ?? fallback, exitCode, resp.status);
  }

  return resp.json() as Promise<UploadResult>;
}

function mapStatusToExitCode(status: number): number {
  if (status === 401 || status === 403) return 4;
  if (status >= 400 && status < 500) return 1;
  return 3;
}

function defaultMessageForStatus(status: number): string {
  switch (status) {
    case 401:
      return 'Not authenticated. Run: loopzero login';
    case 403:
      return 'Permission denied. Check your account access.';
    case 422:
      return 'Invalid request. Check your command arguments.';
    default:
      if (status >= 500) {
        return 'Service unavailable. Try again in a moment.';
      }
      return `Upload failed (HTTP ${status}). Check your command arguments.`;
  }
}

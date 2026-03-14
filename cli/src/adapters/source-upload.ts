import * as fs from 'fs';
import { ApiError } from '../api/client';

export interface UploadResult {
  commit_sha: string;
  apps: string[];
}

export async function uploadSource(
  baseURL: string,
  token: string | null,
  bundlePath: string,
  loopzeroYamlPath: string,
): Promise<UploadResult> {
  const bundleData = fs.readFileSync(bundlePath);
  const loopzeroYamlContent = fs.readFileSync(loopzeroYamlPath, 'utf-8');

  const formData = new FormData();
  formData.append('bundle', new Blob([bundleData]), 'bundle.bundle');
  formData.append('loopzero_yaml', loopzeroYamlContent);

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

export class LoopZeroError extends Error {
  constructor(
    public readonly code: string,
    message: string,
    public readonly httpStatus: number = 500,
  ) {
    super(message);
    this.name = 'LoopZeroError';
  }

  toJSON() {
    return { code: this.code, message: this.message };
  }
}

export const LOCK_TIMEOUT = (detail?: string) =>
  new LoopZeroError('LOCK_TIMEOUT', detail ?? 'Failed to acquire environment lock within timeout', 409);

export const INVALID_TRANSITION = (from: string, event: string) =>
  new LoopZeroError('INVALID_TRANSITION', `Invalid state transition: cannot apply '${event}' in state '${from}'`, 500);

export const TENANT_NOT_FOUND = (tenantId: string) =>
  new LoopZeroError('TENANT_NOT_FOUND', `Tenant '${tenantId}' not found`, 404);

export const BUILD_NOT_FOUND = (buildId: string) =>
  new LoopZeroError('BUILD_NOT_FOUND', `Build '${buildId}' not found`, 404);

export const RELEASE_NOT_FOUND = (releaseId: string) =>
  new LoopZeroError('RELEASE_NOT_FOUND', `Release '${releaseId}' not found`, 404);

export const IDEMPOTENCY_CONFLICT = (key: string) =>
  new LoopZeroError('IDEMPOTENCY_CONFLICT', `Idempotency key '${key}' was already used with a different request payload`, 422);

import {
  LoopZeroError,
  LOCK_TIMEOUT,
  INVALID_TRANSITION,
  TENANT_NOT_FOUND,
  BUILD_NOT_FOUND,
  RELEASE_NOT_FOUND,
  IDEMPOTENCY_CONFLICT,
} from '../../../src/errors/loopzero-error';

describe('LoopZeroError', () => {
  describe('constructor', () => {
    it('extends Error', () => {
      const error = new LoopZeroError('TEST_CODE', 'test message');
      expect(error).toBeInstanceOf(Error);
    });

    it('sets name to LoopZeroError', () => {
      const error = new LoopZeroError('TEST_CODE', 'test message');
      expect(error.name).toBe('LoopZeroError');
    });

    it('stores code, message, and httpStatus', () => {
      const error = new LoopZeroError('MY_CODE', 'something broke', 422);
      expect(error.code).toBe('MY_CODE');
      expect(error.message).toBe('something broke');
      expect(error.httpStatus).toBe(422);
    });

    it('defaults httpStatus to 500', () => {
      const error = new LoopZeroError('MY_CODE', 'server error');
      expect(error.httpStatus).toBe(500);
    });
  });

  describe('toJSON', () => {
    it('returns code and message', () => {
      const error = new LoopZeroError('MY_CODE', 'details here', 409);
      expect(error.toJSON()).toEqual({
        code: 'MY_CODE',
        message: 'details here',
      });
    });
  });

  describe('predefined error factories', () => {
    it('LOCK_TIMEOUT creates a 409 error', () => {
      const error = LOCK_TIMEOUT();
      expect(error.code).toBe('LOCK_TIMEOUT');
      expect(error.httpStatus).toBe(409);
      expect(error.message).toContain('lock');
    });

    it('LOCK_TIMEOUT accepts custom detail', () => {
      const error = LOCK_TIMEOUT('custom detail');
      expect(error.message).toBe('custom detail');
    });

    it('INVALID_TRANSITION creates a 500 error with from/event context', () => {
      const error = INVALID_TRANSITION('QUEUED', 'BuildSucceeded');
      expect(error.code).toBe('INVALID_TRANSITION');
      expect(error.httpStatus).toBe(500);
      expect(error.message).toContain('QUEUED');
      expect(error.message).toContain('BuildSucceeded');
    });

    it('TENANT_NOT_FOUND creates a 404 error', () => {
      const error = TENANT_NOT_FOUND('tenant-abc');
      expect(error.code).toBe('TENANT_NOT_FOUND');
      expect(error.httpStatus).toBe(404);
      expect(error.message).toContain('tenant-abc');
    });

    it('BUILD_NOT_FOUND creates a 404 error', () => {
      const error = BUILD_NOT_FOUND('build-deadbeef');
      expect(error.code).toBe('BUILD_NOT_FOUND');
      expect(error.httpStatus).toBe(404);
      expect(error.message).toContain('build-deadbeef');
    });

    it('RELEASE_NOT_FOUND creates a 404 error', () => {
      const error = RELEASE_NOT_FOUND('rel-12345678');
      expect(error.code).toBe('RELEASE_NOT_FOUND');
      expect(error.httpStatus).toBe(404);
      expect(error.message).toContain('rel-12345678');
    });

    it('IDEMPOTENCY_CONFLICT creates a 422 error', () => {
      const error = IDEMPOTENCY_CONFLICT('key-abc');
      expect(error.code).toBe('IDEMPOTENCY_CONFLICT');
      expect(error.httpStatus).toBe(422);
      expect(error.message).toContain('key-abc');
    });
  });
});

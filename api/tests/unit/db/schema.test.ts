import { builds } from '../../../src/db/schema/builds';
import { releases } from '../../../src/db/schema/releases';
import { environmentTargets } from '../../../src/db/schema/environment-targets';
import { auditLog } from '../../../src/db/schema/audit-log';
import { idempotencyKeys } from '../../../src/db/schema/idempotency-keys';
import { sseEvents } from '../../../src/db/schema/sse-events';
import * as schemaIndex from '../../../src/db/schema/index';
import { getTableName } from 'drizzle-orm';

describe('Database schema', () => {
  describe('all 6 tables are exported from index', () => {
    it('exports builds', () => {
      expect(schemaIndex.builds).toBeDefined();
    });

    it('exports releases', () => {
      expect(schemaIndex.releases).toBeDefined();
    });

    it('exports environmentTargets', () => {
      expect(schemaIndex.environmentTargets).toBeDefined();
    });

    it('exports auditLog', () => {
      expect(schemaIndex.auditLog).toBeDefined();
    });

    it('exports idempotencyKeys', () => {
      expect(schemaIndex.idempotencyKeys).toBeDefined();
    });

    it('exports sseEvents', () => {
      expect(schemaIndex.sseEvents).toBeDefined();
    });
  });

  describe('builds table', () => {
    it('has table name builds', () => {
      expect(getTableName(builds)).toBe('builds');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(builds);
      expect(columnNames).toEqual(expect.arrayContaining([
        'id', 'tenantId', 'projectId', 'environment', 'status',
        'correlationId', 'createdAt', 'createdBy', 'startedAt',
        'finishedAt', 'errorMessage', 'durationSeconds',
      ]));
    });
  });

  describe('releases table', () => {
    it('has table name releases', () => {
      expect(getTableName(releases)).toBe('releases');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(releases);
      expect(columnNames).toEqual(expect.arrayContaining([
        'id', 'buildId', 'tenantId', 'projectId', 'environment',
        'status', 'correlationId', 'createdAt', 'createdBy',
        'gitCommit', 'gitMirrorStatus', 'artifacts', 'sideEffects',
      ]));
    });
  });

  describe('environment_targets table', () => {
    it('has table name environment_targets', () => {
      expect(getTableName(environmentTargets)).toBe('environment_targets');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(environmentTargets);
      expect(columnNames).toEqual(expect.arrayContaining([
        'tenantId', 'projectId', 'environment',
        'desiredReleaseId', 'activeReleaseId',
        'updatedAt', 'updatedBy', 'correlationId',
      ]));
    });
  });

  describe('audit_log table', () => {
    it('has table name audit_log', () => {
      expect(getTableName(auditLog)).toBe('audit_log');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(auditLog);
      expect(columnNames).toEqual(expect.arrayContaining([
        'id', 'tenantId', 'action', 'actor', 'resourceType',
        'resourceId', 'correlationId', 'metadata', 'createdAt',
      ]));
    });
  });

  describe('idempotency_keys table', () => {
    it('has table name idempotency_keys', () => {
      expect(getTableName(idempotencyKeys)).toBe('idempotency_keys');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(idempotencyKeys);
      expect(columnNames).toEqual(expect.arrayContaining([
        'key', 'tenantId', 'endpoint', 'requestHash',
        'responseStatus', 'responseBody', 'createdAt', 'expiresAt',
      ]));
    });
  });

  describe('sse_events table', () => {
    it('has table name sse_events', () => {
      expect(getTableName(sseEvents)).toBe('sse_events');
    });

    it('has required columns', () => {
      const columnNames = Object.keys(sseEvents);
      expect(columnNames).toEqual(expect.arrayContaining([
        'id', 'tenantId', 'channel', 'eventType',
        'payload', 'correlationId', 'createdAt', 'sequence',
      ]));
    });
  });
});

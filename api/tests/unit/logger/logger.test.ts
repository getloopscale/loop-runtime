import pino from 'pino';
import { createLogger, createChildLogger } from '../../../src/logger';

describe('Logger', () => {
  describe('createLogger', () => {
    it('outputs valid JSON', () => {
      const chunks: string[] = [];
      const stream = new (require('stream').Writable)({
        write(chunk: Buffer, _encoding: string, callback: () => void) {
          chunks.push(chunk.toString());
          callback();
        },
      });

      const logger = createLogger({ stream });
      logger.info('test message');

      expect(chunks.length).toBe(1);
      const parsed = JSON.parse(chunks[0]);
      expect(parsed).toBeDefined();
    });

    it('includes required fields: timestamp, level, msg', () => {
      const chunks: string[] = [];
      const stream = new (require('stream').Writable)({
        write(chunk: Buffer, _encoding: string, callback: () => void) {
          chunks.push(chunk.toString());
          callback();
        },
      });

      const logger = createLogger({ stream });
      logger.info('hello structured logging');

      const parsed = JSON.parse(chunks[0]);
      expect(parsed).toHaveProperty('time');
      expect(parsed).toHaveProperty('level');
      expect(parsed).toHaveProperty('msg', 'hello structured logging');
    });

    it('respects LOG_LEVEL env var', () => {
      const originalLevel = process.env.LOG_LEVEL;

      try {
        process.env.LOG_LEVEL = 'warn';

        const chunks: string[] = [];
        const stream = new (require('stream').Writable)({
          write(chunk: Buffer, _encoding: string, callback: () => void) {
            chunks.push(chunk.toString());
            callback();
          },
        });

        const logger = createLogger({ stream });
        logger.info('this should be suppressed');
        logger.warn('this should appear');

        expect(chunks.length).toBe(1);
        const parsed = JSON.parse(chunks[0]);
        expect(parsed.msg).toBe('this should appear');
      } finally {
        if (originalLevel === undefined) {
          delete process.env.LOG_LEVEL;
        } else {
          process.env.LOG_LEVEL = originalLevel;
        }
      }
    });
  });

  describe('createChildLogger', () => {
    it('includes correlation_id and component fields', () => {
      const chunks: string[] = [];
      const stream = new (require('stream').Writable)({
        write(chunk: Buffer, _encoding: string, callback: () => void) {
          chunks.push(chunk.toString());
          callback();
        },
      });

      const parentLogger = createLogger({ stream });
      const childLogger = createChildLogger(parentLogger, {
        correlationId: 'abc-123',
        component: 'deploy-service',
      });

      childLogger.info('child log entry');

      const parsed = JSON.parse(chunks[0]);
      expect(parsed.correlation_id).toBe('abc-123');
      expect(parsed.component).toBe('deploy-service');
      expect(parsed.msg).toBe('child log entry');
    });
  });
});

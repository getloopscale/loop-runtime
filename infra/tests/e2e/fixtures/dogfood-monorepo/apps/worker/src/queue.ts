import { createLogger, generateId } from '@dogfood/shared-utils';

const logger = createLogger('worker:queue');

export interface Job {
  id: string;
  type: string;
  payload: Record<string, unknown>;
  createdAt: string;
  attempts: number;
}

type JobHandler = (job: Job) => Promise<void>;

export class JobQueue {
  private handlers = new Map<string, JobHandler>();
  private queue: Job[] = [];

  register(type: string, handler: JobHandler): void {
    this.handlers.set(type, handler);
    logger.info('Registered job handler', { type });
  }

  enqueue(type: string, payload: Record<string, unknown>): Job {
    const job: Job = {
      id: generateId('job'),
      type,
      payload,
      createdAt: new Date().toISOString(),
      attempts: 0,
    };
    this.queue.push(job);
    logger.info('Job enqueued', { jobId: job.id, type });
    return job;
  }

  async processNext(): Promise<boolean> {
    const job = this.queue.shift();
    if (!job) return false;

    const handler = this.handlers.get(job.type);
    if (!handler) {
      logger.error('No handler for job type', { type: job.type, jobId: job.id });
      return true;
    }

    job.attempts++;
    try {
      await handler(job);
      logger.info('Job completed', { jobId: job.id, type: job.type, attempts: job.attempts });
    } catch (err) {
      logger.error('Job failed', {
        jobId: job.id,
        type: job.type,
        attempts: job.attempts,
        error: err instanceof Error ? err.message : String(err),
      });

      if (job.attempts < 3) {
        this.queue.push(job);
        logger.info('Job requeued', { jobId: job.id, attempts: job.attempts });
      } else {
        logger.error('Job exhausted retries', { jobId: job.id, type: job.type });
      }
    }

    return true;
  }
}

import { createLogger, sleep } from '@dogfood/shared-utils';
import type { Job } from '../queue';

const logger = createLogger('worker:cleanup');

export async function processCleanup(job: Job): Promise<void> {
  const { resource, olderThanDays } = job.payload as {
    resource: string;
    olderThanDays: number;
  };

  logger.info('Running cleanup', { resource, olderThanDays });

  // Simulate cleanup work
  await sleep(200);

  const cutoff = new Date();
  cutoff.setDate(cutoff.getDate() - olderThanDays);

  logger.info('Cleanup completed', {
    jobId: job.id,
    resource,
    cutoffDate: cutoff.toISOString(),
    removedCount: Math.floor(Math.random() * 50),
  });
}

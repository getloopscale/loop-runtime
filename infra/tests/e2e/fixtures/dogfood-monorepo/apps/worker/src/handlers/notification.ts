import { createLogger, sleep } from '@dogfood/shared-utils';
import type { Job } from '../queue';

const logger = createLogger('worker:notification');

export async function processNotification(job: Job): Promise<void> {
  const { recipient, subject, body } = job.payload as {
    recipient: string;
    subject: string;
    body: string;
  };

  logger.info('Sending notification', { recipient, subject });

  // Simulate sending (email, push, webhook)
  await sleep(100);

  logger.info('Notification sent', {
    jobId: job.id,
    recipient,
    subject,
    bodyLength: typeof body === 'string' ? body.length : 0,
  });
}

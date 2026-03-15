import { createLogger, sleep } from '@dogfood/shared-utils';
import { loadConfig } from '@dogfood/shared-config';
import { JobQueue } from './queue';
import { processNotification } from './handlers/notification';
import { processCleanup } from './handlers/cleanup';

const config = loadConfig();
const logger = createLogger('worker');

const queue = new JobQueue();
queue.register('notification', processNotification);
queue.register('cleanup', processCleanup);

async function main() {
  logger.info('Worker started', { env: config.env });

  const shutdown = () => {
    logger.info('Worker shutting down');
    process.exit(0);
  };
  process.on('SIGTERM', shutdown);
  process.on('SIGINT', shutdown);

  while (true) {
    const processed = await queue.processNext();
    if (!processed) {
      await sleep(5000);
    }
  }
}

main().catch((err) => {
  logger.error('Worker fatal error', { error: err instanceof Error ? err.message : String(err) });
  process.exit(1);
});

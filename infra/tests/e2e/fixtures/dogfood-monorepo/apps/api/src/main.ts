import 'reflect-metadata';
import { NestFactory } from '@nestjs/core';
import { AppModule } from './app.module';
import { createLogger } from '@dogfood/shared-utils';
import { loadConfig } from '@dogfood/shared-config';

async function bootstrap() {
  const config = loadConfig({ port: parseInt(process.env.PORT || '3000', 10) });
  const logger = createLogger('api');

  const app = await NestFactory.create(AppModule, { logger: false });

  app.enableCors();
  app.enableShutdownHooks();

  await app.listen(config.port);
  logger.info('API server started', { port: config.port, env: config.env });
}

bootstrap().catch((err) => {
  console.error('Failed to start API:', err);
  process.exit(1);
});

import express from 'express';
import cors from 'cors';
import helmet from 'helmet';
import { createLogger } from '@dogfood/shared-utils';
import { loadConfig } from '@dogfood/shared-config';
import { healthRouter } from './routes/health';
import { proxyRouter } from './routes/proxy';
import { rateLimiter } from './middleware/rate-limiter';
import { requestLogger } from './middleware/request-logger';

const config = loadConfig({ port: parseInt(process.env.PORT || '4000', 10) });
const logger = createLogger('gateway');

const app = express();

app.use(helmet());
app.use(cors());
app.use(express.json());
app.use(requestLogger(logger));
app.use(rateLimiter());

app.use('/', healthRouter);
app.use('/api', proxyRouter);

app.listen(config.port, () => {
  logger.info('Gateway started', { port: config.port, env: config.env });
});

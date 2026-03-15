import { Router, Request, Response } from 'express';
import { createLogger } from '@dogfood/shared-utils';

const logger = createLogger('gateway:proxy');

export const proxyRouter = Router();

const API_URL = process.env.API_UPSTREAM_URL || 'http://localhost:3000';

proxyRouter.all('/*', async (req: Request, res: Response) => {
  const targetUrl = `${API_URL}${req.path}`;

  try {
    const response = await fetch(targetUrl, {
      method: req.method,
      headers: {
        'Content-Type': 'application/json',
        ...(req.headers['authorization']
          ? { Authorization: req.headers['authorization'] as string }
          : {}),
      },
      body: ['POST', 'PUT', 'PATCH'].includes(req.method)
        ? JSON.stringify(req.body)
        : undefined,
    });

    const data = await response.text();
    res.status(response.status);

    try {
      res.json(JSON.parse(data));
    } catch {
      res.send(data);
    }
  } catch (err) {
    logger.error('Upstream request failed', {
      target: targetUrl,
      error: err instanceof Error ? err.message : String(err),
    });
    res.status(502).json({
      error: 'Bad Gateway',
      message: 'Upstream service unavailable',
    });
  }
});

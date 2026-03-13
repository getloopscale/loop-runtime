import pino, { Logger, DestinationStream, LoggerOptions } from 'pino';

export interface CreateLoggerOptions {
  stream?: DestinationStream;
}

export function createLogger(options: CreateLoggerOptions = {}): Logger {
  const level = process.env.LOG_LEVEL?.toLowerCase() ?? 'info';

  const pinoOptions: LoggerOptions = {
    level,
    timestamp: pino.stdTimeFunctions.isoTime,
  };

  if (options.stream) {
    return pino(pinoOptions, options.stream);
  }

  return pino(pinoOptions);
}

export interface ChildLoggerOptions {
  correlationId: string;
  component: string;
}

export function createChildLogger(
  parent: Logger,
  options: ChildLoggerOptions,
): Logger {
  return parent.child({
    correlation_id: options.correlationId,
    component: options.component,
  });
}

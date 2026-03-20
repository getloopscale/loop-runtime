export interface AppConfig {
  port: number;
  env: string;
  logLevel: string;
  database?: DatabaseConfig;
  redis?: RedisConfig;
}

export interface DatabaseConfig {
  url: string;
  maxConnections: number;
  idleTimeoutMs: number;
}

export interface RedisConfig {
  url: string;
  maxRetries: number;
}

export function loadConfig(overrides: Partial<AppConfig> = {}): AppConfig {
  return {
    port: parseInt(process.env.PORT || '3000', 10),
    env: process.env.NODE_ENV || 'development',
    logLevel: process.env.LOG_LEVEL || 'info',
    database: process.env.DATABASE_URL
      ? {
          url: process.env.DATABASE_URL,
          maxConnections: parseInt(process.env.DB_MAX_CONN || '10', 10),
          idleTimeoutMs: parseInt(process.env.DB_IDLE_TIMEOUT || '30000', 10),
        }
      : undefined,
    redis: process.env.REDIS_URL
      ? {
          url: process.env.REDIS_URL,
          maxRetries: parseInt(process.env.REDIS_MAX_RETRIES || '3', 10),
        }
      : undefined,
    ...overrides,
  };
}

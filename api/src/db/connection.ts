/**
 * Database connection setup.
 * Actual connection will be configured in Sprint 1 when Fastify is added.
 * This module exists as a placeholder for the connection factory.
 */

export interface DatabaseConfig {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
}

export function getDatabaseConfig(): DatabaseConfig {
  return {
    host: process.env['DATABASE_HOST'] ?? 'localhost',
    port: parseInt(process.env['DATABASE_PORT'] ?? '5432', 10),
    database: process.env['DATABASE_NAME'] ?? 'loopzero',
    user: process.env['DATABASE_USER'] ?? 'loopzero',
    password: process.env['DATABASE_PASSWORD'] ?? '',
  };
}

import { defineConfig } from 'drizzle-kit';

export default defineConfig({
  schema: './src/db/schema/index.ts',
  out: './src/db/migrations',
  dialect: 'postgresql',
  dbCredentials: {
    host: process.env['DATABASE_HOST'] ?? 'localhost',
    port: parseInt(process.env['DATABASE_PORT'] ?? '30432', 10),
    database: process.env['DATABASE_NAME'] ?? 'loopzero',
    user: process.env['DATABASE_USER'] ?? 'loopzero',
    password: process.env['DATABASE_PASSWORD'] ?? 'loopzero-dev-password',
    ssl: false,
  },
});

import { pgTable, text, timestamp, integer } from 'drizzle-orm/pg-core';

export const builds = pgTable('builds', {
  id: text('id').primaryKey(),
  tenantId: text('tenant_id').notNull(),
  projectId: text('project_id').notNull(),
  environment: text('environment').notNull(),
  status: text('status').notNull(),
  correlationId: text('correlation_id').notNull(),
  createdAt: timestamp('created_at', { withTimezone: true }).defaultNow().notNull(),
  createdBy: text('created_by').notNull(),
  startedAt: timestamp('started_at', { withTimezone: true }),
  finishedAt: timestamp('finished_at', { withTimezone: true }),
  errorMessage: text('error_message'),
  durationSeconds: integer('duration_seconds'),
});

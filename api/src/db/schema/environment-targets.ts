import { pgTable, text, timestamp, primaryKey } from 'drizzle-orm/pg-core';
import { releases } from './releases';

export const environmentTargets = pgTable('environment_targets', {
  tenantId: text('tenant_id').notNull(),
  projectId: text('project_id').notNull(),
  environment: text('environment').notNull(),
  desiredReleaseId: text('desired_release_id').notNull().references(() => releases.id),
  activeReleaseId: text('active_release_id').references(() => releases.id),
  updatedAt: timestamp('updated_at', { withTimezone: true }).defaultNow().notNull(),
  updatedBy: text('updated_by').notNull(),
  correlationId: text('correlation_id').notNull(),
}, (table) => ({
  pk: primaryKey({ columns: [table.tenantId, table.projectId, table.environment] }),
}));

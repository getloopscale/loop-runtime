import { pgTable, text, timestamp, jsonb } from 'drizzle-orm/pg-core';
import { builds } from './builds';

export const releases = pgTable('releases', {
  id: text('id').primaryKey(),
  buildId: text('build_id').notNull().references(() => builds.id),
  tenantId: text('tenant_id').notNull(),
  projectId: text('project_id').notNull(),
  environment: text('environment').notNull(),
  status: text('status').notNull(),
  correlationId: text('correlation_id').notNull(),
  createdAt: timestamp('created_at', { withTimezone: true }).defaultNow().notNull(),
  createdBy: text('created_by').notNull(),
  gitCommit: text('git_commit'),
  gitMirrorStatus: text('git_mirror_status').default('pending').notNull(),
  artifacts: jsonb('artifacts'),
  sideEffects: jsonb('side_effects'),
});

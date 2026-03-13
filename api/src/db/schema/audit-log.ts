import { pgTable, text, timestamp, jsonb, serial } from 'drizzle-orm/pg-core';

export const auditLog = pgTable('audit_log', {
  id: serial('id').primaryKey(),
  tenantId: text('tenant_id').notNull(),
  action: text('action').notNull(),
  actor: text('actor').notNull(),
  resourceType: text('resource_type').notNull(),
  resourceId: text('resource_id').notNull(),
  correlationId: text('correlation_id').notNull(),
  metadata: jsonb('metadata'),
  createdAt: timestamp('created_at', { withTimezone: true }).defaultNow().notNull(),
});

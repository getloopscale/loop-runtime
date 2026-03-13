import { pgTable, text, timestamp, jsonb, serial } from 'drizzle-orm/pg-core';

export const sseEvents = pgTable('sse_events', {
  id: serial('id').primaryKey(),
  tenantId: text('tenant_id').notNull(),
  channel: text('channel').notNull(),
  eventType: text('event_type').notNull(),
  payload: jsonb('payload').notNull(),
  correlationId: text('correlation_id').notNull(),
  createdAt: timestamp('created_at', { withTimezone: true }).defaultNow().notNull(),
  sequence: serial('sequence').notNull(),
});

import { pgTable, text, timestamp, jsonb, integer, index } from 'drizzle-orm/pg-core';

export const idempotencyKeys = pgTable('idempotency_keys', {
  key: text('key').notNull(),
  tenantId: text('tenant_id').notNull(),
  endpoint: text('endpoint').notNull(),
  requestHash: text('request_hash').notNull(),
  responseStatus: integer('response_status').notNull(),
  responseBody: jsonb('response_body').notNull(),
  createdAt: timestamp('created_at', { withTimezone: true }).defaultNow().notNull(),
  expiresAt: timestamp('expires_at', { withTimezone: true }).notNull(),
}, (table) => ({
  keyTenantIdx: index('idempotency_keys_key_tenant_idx').on(table.key, table.tenantId),
}));

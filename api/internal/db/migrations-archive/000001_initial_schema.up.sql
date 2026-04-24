CREATE TABLE IF NOT EXISTS "builds" (
    "id" text PRIMARY KEY NOT NULL,
    "tenant_id" text NOT NULL,
    "project_id" text NOT NULL,
    "environment" text NOT NULL,
    "status" text NOT NULL,
    "correlation_id" text NOT NULL,
    "created_at" timestamp with time zone DEFAULT now() NOT NULL,
    "created_by" text NOT NULL,
    "started_at" timestamp with time zone,
    "finished_at" timestamp with time zone,
    "error_message" text,
    "duration_seconds" integer,
    "workflow_id" text
);

CREATE TABLE IF NOT EXISTS "releases" (
    "id" text PRIMARY KEY NOT NULL,
    "build_id" text NOT NULL,
    "tenant_id" text NOT NULL,
    "project_id" text NOT NULL,
    "environment" text NOT NULL,
    "status" text NOT NULL,
    "correlation_id" text NOT NULL,
    "created_at" timestamp with time zone DEFAULT now() NOT NULL,
    "created_by" text NOT NULL,
    "git_commit" text,
    "git_mirror_status" text DEFAULT 'pending' NOT NULL,
    "artifacts" jsonb,
    "side_effects" jsonb
);

CREATE TABLE IF NOT EXISTS "environment_targets" (
    "tenant_id" text NOT NULL,
    "project_id" text NOT NULL,
    "environment" text NOT NULL,
    "desired_release_id" text NOT NULL,
    "active_release_id" text,
    "updated_at" timestamp with time zone DEFAULT now() NOT NULL,
    "updated_by" text NOT NULL,
    "correlation_id" text NOT NULL,
    CONSTRAINT "environment_targets_pk" PRIMARY KEY ("tenant_id", "project_id", "environment")
);

CREATE TABLE IF NOT EXISTS "audit_log" (
    "id" serial PRIMARY KEY NOT NULL,
    "tenant_id" text NOT NULL,
    "action" text NOT NULL,
    "actor" text NOT NULL,
    "resource_type" text NOT NULL,
    "resource_id" text NOT NULL,
    "correlation_id" text NOT NULL,
    "metadata" jsonb,
    "created_at" timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE IF NOT EXISTS "idempotency_keys" (
    "key" text PRIMARY KEY NOT NULL,
    "tenant_id" text NOT NULL,
    "endpoint" text NOT NULL,
    "request_hash" text NOT NULL,
    "response_status" integer NOT NULL,
    "response_body" jsonb NOT NULL,
    "created_at" timestamp with time zone DEFAULT now() NOT NULL,
    "expires_at" timestamp with time zone NOT NULL
);

CREATE TABLE IF NOT EXISTS "sse_events" (
    "id" bigserial PRIMARY KEY NOT NULL,
    "build_id" text NOT NULL,
    "event_type" text NOT NULL,
    "data" jsonb NOT NULL,
    "created_at" timestamp with time zone DEFAULT now() NOT NULL
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'releases_build_id_builds_id_fk') THEN
        ALTER TABLE "releases" ADD CONSTRAINT "releases_build_id_builds_id_fk" FOREIGN KEY ("build_id") REFERENCES "builds"("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'environment_targets_desired_release_id_releases_id_fk') THEN
        ALTER TABLE "environment_targets" ADD CONSTRAINT "environment_targets_desired_release_id_releases_id_fk" FOREIGN KEY ("desired_release_id") REFERENCES "releases"("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
    END IF;
END $$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'environment_targets_active_release_id_releases_id_fk') THEN
        ALTER TABLE "environment_targets" ADD CONSTRAINT "environment_targets_active_release_id_releases_id_fk" FOREIGN KEY ("active_release_id") REFERENCES "releases"("id") ON DELETE NO ACTION ON UPDATE NO ACTION;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS "idempotency_keys_key_tenant_idx" ON "idempotency_keys" USING btree ("key", "tenant_id");

CREATE INDEX IF NOT EXISTS "idx_sse_events_build" ON "sse_events" USING btree ("build_id", "id");

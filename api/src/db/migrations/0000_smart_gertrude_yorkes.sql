CREATE TABLE "audit_log" (
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
--> statement-breakpoint
CREATE TABLE "builds" (
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
	"duration_seconds" integer
);
--> statement-breakpoint
CREATE TABLE "environment_targets" (
	"tenant_id" text NOT NULL,
	"project_id" text NOT NULL,
	"environment" text NOT NULL,
	"desired_release_id" text NOT NULL,
	"active_release_id" text,
	"updated_at" timestamp with time zone DEFAULT now() NOT NULL,
	"updated_by" text NOT NULL,
	"correlation_id" text NOT NULL,
	CONSTRAINT "environment_targets_tenant_id_project_id_environment_pk" PRIMARY KEY("tenant_id","project_id","environment")
);
--> statement-breakpoint
CREATE TABLE "idempotency_keys" (
	"key" text NOT NULL,
	"tenant_id" text NOT NULL,
	"endpoint" text NOT NULL,
	"request_hash" text NOT NULL,
	"response_status" integer NOT NULL,
	"response_body" jsonb NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"expires_at" timestamp with time zone NOT NULL
);
--> statement-breakpoint
CREATE TABLE "releases" (
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
--> statement-breakpoint
CREATE TABLE "sse_events" (
	"id" serial PRIMARY KEY NOT NULL,
	"tenant_id" text NOT NULL,
	"channel" text NOT NULL,
	"event_type" text NOT NULL,
	"payload" jsonb NOT NULL,
	"correlation_id" text NOT NULL,
	"created_at" timestamp with time zone DEFAULT now() NOT NULL,
	"sequence" serial NOT NULL
);
--> statement-breakpoint
ALTER TABLE "environment_targets" ADD CONSTRAINT "environment_targets_desired_release_id_releases_id_fk" FOREIGN KEY ("desired_release_id") REFERENCES "public"."releases"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "environment_targets" ADD CONSTRAINT "environment_targets_active_release_id_releases_id_fk" FOREIGN KEY ("active_release_id") REFERENCES "public"."releases"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
ALTER TABLE "releases" ADD CONSTRAINT "releases_build_id_builds_id_fk" FOREIGN KEY ("build_id") REFERENCES "public"."builds"("id") ON DELETE no action ON UPDATE no action;--> statement-breakpoint
CREATE INDEX "idempotency_keys_key_tenant_idx" ON "idempotency_keys" USING btree ("key","tenant_id");
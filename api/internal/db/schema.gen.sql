--
-- PostgreSQL database dump
--

-- Dumped from database version 17.5
-- Dumped by pg_dump version 17.5

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--

-- *not* creating schema, since initdb creates it


--
-- Name: audit_attestation_url_set(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.audit_attestation_url_set() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF NEW.image_attestation_rekor_url IS NOT NULL THEN
    INSERT INTO audit_log
      (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
    VALUES
      (NEW.tenant_id,
       'release.attestation_url_set',
       'system:release-attestation',
       'release',
       NEW.id,
       NEW.correlation_id,
       jsonb_build_object(
         'rekor_url',  NEW.image_attestation_rekor_url,
         'build_id',   NEW.build_id,
         'project_id', NEW.project_id,
         'environment', NEW.environment));
  END IF;
  RETURN NEW;
END;
$$;


--
-- Name: FUNCTION audit_attestation_url_set(); Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON FUNCTION public.audit_attestation_url_set() IS 'T2.1 / CR-4 (MEET-2026-05-28-001): emits audit_log row with action release.attestation_url_set whenever a release lands with a non-NULL image_attestation_rekor_url. Pairs with the write-once trigger above to give SecOps a tamper-evident trail of every Rekor URL persisted.';


--
-- Name: audit_log_prevent_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.audit_log_prevent_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  v_pruner_bypass TEXT;
BEGIN
  -- T7.3: allow controlled DELETE by the audit_log_pruner service
  -- when it sets `SET LOCAL loopzero.audit_pruner = 'true'` inside a
  -- transaction. Any other caller (psql, app code, runaway query)
  -- has NULL or empty here and falls through to the EXCEPTION.
  v_pruner_bypass := current_setting('loopzero.audit_pruner', true);
  IF TG_OP = 'DELETE' AND v_pruner_bypass = 'true' THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'audit_log is append-only: % not allowed', TG_OP;
  RETURN NULL;
END;
$$;


--
-- Name: FUNCTION audit_log_prevent_mutation(); Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON FUNCTION public.audit_log_prevent_mutation() IS 'deep-review-remediation T7.3 — allows pruner DELETE when session var loopzero.audit_pruner=true; all other mutations rejected.';


--
-- Name: audit_sensitive_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.audit_sensitive_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
  v_tenant_id TEXT;
  v_resource_id TEXT;
BEGIN
  IF TG_OP = 'DELETE' THEN
    v_tenant_id := OLD.tenant_id;
  ELSE
    v_tenant_id := NEW.tenant_id;
  END IF;

  IF TG_OP = 'DELETE' THEN
    IF TG_TABLE_NAME IN ('secrets', 'preview_environments') THEN
      v_resource_id := OLD.id;
    ELSE
      v_resource_id := OLD.tenant_id || ':' || OLD.user_id;
    END IF;
  ELSE
    IF TG_TABLE_NAME IN ('secrets', 'preview_environments') THEN
      v_resource_id := NEW.id;
    ELSE
      v_resource_id := NEW.tenant_id || ':' || NEW.user_id;
    END IF;
  END IF;

  INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
  VALUES (
    v_tenant_id, TG_OP,
    COALESCE(current_setting('app.current_user', true), 'system'),
    TG_TABLE_NAME, v_resource_id,
    current_setting('app.correlation_id', true),
    jsonb_build_object('trigger', true, 'table', TG_TABLE_NAME, 'op', TG_OP)
  );

  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;


--
-- Name: populate_cell_id_from_tenant(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: preview_environments_set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.preview_environments_set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


--
-- Name: reject_attestation_url_mutation(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.reject_attestation_url_mutation() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
  IF OLD.image_attestation_rekor_url IS NOT NULL
     AND NEW.image_attestation_rekor_url IS DISTINCT FROM OLD.image_attestation_rekor_url THEN
    RAISE EXCEPTION
      'image_attestation_rekor_url is write-once (release_id=%, old=%, new=%)',
      OLD.id, OLD.image_attestation_rekor_url, NEW.image_attestation_rekor_url
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END;
$$;


--
-- Name: FUNCTION reject_attestation_url_mutation(); Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON FUNCTION public.reject_attestation_url_mutation() IS 'T2.1 / CR-4 (MEET-2026-05-28-001): prevent mutation of releases.image_attestation_rekor_url once it is set. SLSA L3 chain integrity gate — silent rewrite would let an attacker point future verification at a controlled Rekor mirror.';


--
-- Name: release_app_attestations_set_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.release_app_attestations_set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


--
-- Name: set_cells_updated_at(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.set_cells_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: app_scaling_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_scaling_config (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    environment text NOT NULL,
    app_name text NOT NULL,
    min_replicas integer NOT NULL,
    max_replicas integer NOT NULL,
    target_cpu_pct integer,
    scale_to_zero boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL,
    config_hash text DEFAULT ''::text NOT NULL,
    CONSTRAINT app_scaling_config_check CHECK ((max_replicas >= min_replicas)),
    CONSTRAINT app_scaling_config_environment_syntax_check CHECK ((environment ~ '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$'::text)),
    CONSTRAINT app_scaling_config_min_replicas_check CHECK ((min_replicas >= 0)),
    CONSTRAINT app_scaling_config_target_cpu_pct_check CHECK (((target_cpu_pct IS NULL) OR ((target_cpu_pct >= 1) AND (target_cpu_pct <= 100)))),
    CONSTRAINT chk_scale_to_zero_requires_min_zero CHECK (((NOT scale_to_zero) OR (min_replicas = 0)))
);


--
-- Name: TABLE app_scaling_config; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.app_scaling_config IS 'CHG-2026-05-03-SCALE-REAL — scaling config per app. Replaces loopzero.yaml-only path. Plan T2.1 of pre-launch-gaps-plan.md.';


--
-- Name: COLUMN app_scaling_config.config_hash; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.app_scaling_config.config_hash IS 'sha256 of canonical (min,max,target_cpu_pct,scale_to_zero) for idempotency short-circuit.';


--
-- Name: audit_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_log (
    id integer NOT NULL,
    tenant_id text NOT NULL,
    action text NOT NULL,
    actor text NOT NULL,
    resource_type text NOT NULL,
    resource_id text NOT NULL,
    correlation_id text,
    metadata jsonb,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: audit_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.audit_log_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: audit_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.audit_log_id_seq OWNED BY public.audit_log.id;


--
-- Name: build_apps; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.build_apps (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    build_id text NOT NULL,
    app_name text NOT NULL,
    status text DEFAULT 'PENDING'::text NOT NULL,
    duration_ms integer,
    error text,
    image_digest text,
    url text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: builds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.builds (
    id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    environment text NOT NULL,
    status text NOT NULL,
    correlation_id text NOT NULL,
    created_by text NOT NULL,
    commit_sha text,
    apps_spec jsonb,
    workflow_id text,
    error_message text,
    duration_seconds integer,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    processing_by text,
    processing_since timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    bundle_digest text,
    CONSTRAINT builds_status_check CHECK ((status = ANY (ARRAY['QUEUED'::text, 'RUNNING'::text, 'RELEASING'::text, 'SUCCEEDED'::text, 'FAILED'::text, 'CANCELLING'::text, 'CANCELLED'::text])))
);


--
-- Name: COLUMN builds.bundle_digest; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.builds.bundle_digest IS 'deep-review-2026-05-10 T2.2 — SHA256 hex of the uploaded source bundle, computed at upload time + verified by the clone-runner step. Mismatch indicates S3 substitution.';


--
-- Name: cells; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cells (
    cell_id text NOT NULL,
    display_name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    region text DEFAULT 'unknown'::text NOT NULL,
    capacity_tenants_max integer,
    notes text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cells_capacity_tenants_max_check CHECK (((capacity_tenants_max IS NULL) OR (capacity_tenants_max >= 0))),
    CONSTRAINT cells_cell_id_check CHECK ((cell_id ~ '^cell-[a-z0-9-]+$'::text)),
    CONSTRAINT cells_status_check CHECK ((status = ANY (ARRAY['active'::text, 'draining'::text, 'maintenance'::text, 'retired'::text])))
);


--
-- Name: TABLE cells; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.cells IS 'Operational source of truth for cell metadata (capacity, status, audit). Connectivity SoT remains ClusterCredential CRD. Two SoTs answer different questions.';


--
-- Name: COLUMN cells.status; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.cells.status IS 'active=accept new pinning; draining=existing only; maintenance=reject all; retired=terminal.';


--
-- Name: COLUMN cells.capacity_tenants_max; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.cells.capacity_tenants_max IS 'NULL=unlimited; 0=blocked transiently; positive=hard cap enforced by placement layer.';


--
-- Name: circuit_breakers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.circuit_breakers (
    key text NOT NULL,
    state text DEFAULT 'closed'::text NOT NULL,
    failure_count integer DEFAULT 0 NOT NULL,
    success_count integer DEFAULT 0 NOT NULL,
    last_failure timestamp with time zone,
    last_success timestamp with time zone,
    opened_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT circuit_breakers_state_check CHECK ((state = ANY (ARRAY['closed'::text, 'open'::text])))
);


--
-- Name: cost_counters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cost_counters (
    tenant_id text NOT NULL,
    plan_tier text NOT NULL,
    metric text NOT NULL,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone NOT NULL,
    value bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cost_counters_metric_check CHECK ((metric = ANY (ARRAY['egress_bytes'::text, 'docr_pulls'::text, 'build_minutes'::text]))),
    CONSTRAINT cost_counters_plan_tier_check CHECK ((plan_tier = ANY (ARRAY['starter'::text, 'pro'::text, 'team'::text, 'enterprise'::text])))
);


--
-- Name: TABLE cost_counters; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.cost_counters IS 'Per-tenant cost cap enforcement state. T1.9 of MEET-2026-05-12 launch readiness. Separate from usage_records (billing history) per MEET-ASYNC-AMENDMENT-2026-05-12-001 A003.';


--
-- Name: custom_domains; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.custom_domains (
    id text NOT NULL,
    tenant_id text NOT NULL,
    project_name text NOT NULL,
    app_name text NOT NULL,
    environment text NOT NULL,
    domain text NOT NULL,
    status text DEFAULT 'pending_verification'::text NOT NULL,
    verification_token text NOT NULL,
    verified_at timestamp with time zone,
    tls_status text DEFAULT 'pending'::text,
    tls_expires_at timestamp with time zone,
    failure_reason text,
    last_cname_check timestamp with time zone,
    cname_consecutive_failures integer DEFAULT 0 NOT NULL,
    cf_zone_id text,
    cf_zone_status text DEFAULT 'none'::text,
    cf_nameservers text[],
    dns_records_imported boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT custom_domains_cf_zone_status_check CHECK ((cf_zone_status = ANY (ARRAY['none'::text, 'pending'::text, 'active'::text, 'failed'::text, 'removed'::text]))),
    CONSTRAINT custom_domains_status_check CHECK ((status = ANY (ARRAY['pending_verification'::text, 'verified'::text, 'pending_ns'::text, 'ns_active'::text, 'provisioning'::text, 'active'::text, 'degraded'::text, 'failed'::text, 'removing'::text]))),
    CONSTRAINT custom_domains_tls_status_check CHECK ((tls_status = ANY (ARRAY['pending'::text, 'issuing'::text, 'issued'::text, 'failed'::text])))
);


--
-- Name: environment_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.environment_targets (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    environment text NOT NULL,
    desired_release_id text NOT NULL,
    active_release_id text,
    updated_by text NOT NULL,
    correlation_id text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    kind text DEFAULT 'permanent'::text NOT NULL,
    CONSTRAINT environment_targets_kind_check CHECK ((kind = ANY (ARRAY['permanent'::text, 'preview'::text])))
);


--
-- Name: github_installations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.github_installations (
    id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: github_webhook_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.github_webhook_deliveries (
    delivery_id text NOT NULL,
    event_type text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.idempotency_keys (
    key text NOT NULL,
    tenant_id text NOT NULL,
    endpoint text NOT NULL,
    request_hash text NOT NULL,
    response_status integer NOT NULL,
    response_body jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone NOT NULL
);


--
-- Name: leader_generations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.leader_generations (
    name text NOT NULL,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    holder text NOT NULL,
    generation_id bigint DEFAULT 1 NOT NULL,
    acquired_at timestamp with time zone DEFAULT now() NOT NULL,
    heartbeat_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: memory_exports; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.memory_exports (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    display_name text,
    status text NOT NULL,
    file_path text,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT memory_exports_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'succeeded'::text, 'failed'::text])))
);


--
-- Name: memory_webhooks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.memory_webhooks (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    url text NOT NULL,
    label text NOT NULL,
    events jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT memory_webhooks_events_check CHECK ((jsonb_typeof(events) = 'array'::text)),
    CONSTRAINT memory_webhooks_label_check CHECK (((length(label) >= 1) AND (length(label) <= 64))),
    CONSTRAINT memory_webhooks_url_check CHECK ((url ~* '^https?://'::text))
);


--
-- Name: plan_limits; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plan_limits (
    plan text NOT NULL,
    metric text NOT NULL,
    hard_limit integer,
    soft_limit integer
);


--
-- Name: preview_environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.preview_environments (
    id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    pr_number integer NOT NULL,
    branch text NOT NULL,
    commit_sha text NOT NULL,
    status text DEFAULT 'creating'::text NOT NULL,
    url text,
    build_id text,
    release_id text,
    repo_full_name text,
    installation_id bigint,
    expires_at timestamp with time zone NOT NULL,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT preview_environments_id_format CHECK ((id ~~ 'prev-%'::text)),
    CONSTRAINT preview_environments_pr_number_check CHECK ((pr_number > 0)),
    CONSTRAINT preview_environments_status_check CHECK ((status = ANY (ARRAY['creating'::text, 'active'::text, 'expiring'::text, 'deleting'::text, 'deleted'::text, 'failed'::text, 'cleanup_failed'::text])))
);


--
-- Name: TABLE preview_environments; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.preview_environments IS 'Preview environment metadata for API queries (Sprint 5, ADR D7). Operational state lives in the PreviewEnvironment CR.';


--
-- Name: COLUMN preview_environments.status; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.preview_environments.status IS 'Lifecycle phase mirroring the state machine in pkg/statemachine/preview.go (stored lowercase here; Phase casing in the CR is "Creating"/"Active"/etc.).';


--
-- Name: preview_slot; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.preview_slot (
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    active_pr_number integer,
    active_branch text,
    active_release_id text,
    url text,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT preview_slot_all_or_nothing CHECK ((((active_pr_number IS NULL) AND (active_branch IS NULL) AND (url IS NULL)) OR ((active_pr_number IS NOT NULL) AND (active_branch IS NOT NULL))))
);


--
-- Name: project_repositories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_repositories (
    id text NOT NULL,
    tenant_id text NOT NULL,
    project_name text NOT NULL,
    provider text DEFAULT 'github'::text NOT NULL,
    repo_full_name text NOT NULL,
    repo_id bigint,
    installation_id bigint NOT NULL,
    default_branch text DEFAULT 'main'::text NOT NULL,
    auto_deploy boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projects (
    id text NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    created_by text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: release_app_attestations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.release_app_attestations (
    id text NOT NULL,
    release_id text NOT NULL,
    tenant_id text NOT NULL,
    app_name text NOT NULL,
    image_digest text NOT NULL,
    image_signature text,
    signed_at timestamp with time zone,
    sbom_s3_key text,
    sbom_format text,
    sbom_component_count integer,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT release_app_attestations_digest_format CHECK ((image_digest ~~ 'sha256:%'::text)),
    CONSTRAINT release_app_attestations_id_format CHECK ((id ~~ 'raa-%'::text)),
    CONSTRAINT release_app_attestations_sbom_count_nonneg CHECK (((sbom_component_count IS NULL) OR (sbom_component_count >= 0))),
    CONSTRAINT release_app_attestations_sbom_format_value CHECK (((sbom_format IS NULL) OR (sbom_format = ANY (ARRAY['cyclonedx-json'::text, 'spdx-json'::text]))))
);


--
-- Name: TABLE release_app_attestations; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON TABLE public.release_app_attestations IS 'Per-app supply-chain attestation (signature + SBOM metadata) sidecar for releases (Sprint 6, ADR-0014 fix for multi-app builds).';


--
-- Name: COLUMN release_app_attestations.image_digest; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.release_app_attestations.image_digest IS 'OCI image digest (sha256:...). Same digest as in the Application CR.';


--
-- Name: COLUMN release_app_attestations.image_signature; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.release_app_attestations.image_signature IS 'Pinned reference (registry/path:tag@sha256:...) used by cosign sign. NULL when the build pre-dates Sprint 6 cosign signing or signing failed.';


--
-- Name: COLUMN release_app_attestations.sbom_s3_key; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.release_app_attestations.sbom_s3_key IS 'S3 object key pointing to the CycloneDX/SPDX SBOM (Sprint 6 syft step). NULL when SBOM generation is disabled or pre-dates Sprint 6.';


--
-- Name: releases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.releases (
    id text NOT NULL,
    build_id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    environment text NOT NULL,
    status text NOT NULL,
    correlation_id text NOT NULL,
    created_by text NOT NULL,
    git_commit text,
    git_mirror_status text DEFAULT 'pending'::text NOT NULL,
    artifacts jsonb,
    side_effects jsonb,
    apps jsonb,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    processing_by text,
    processing_since timestamp with time zone,
    image_digest text,
    image_signature text,
    image_signed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    image_attestation_rekor_url text,
    CONSTRAINT releases_status_check CHECK ((status = ANY (ARRAY['READY'::text, 'PENDING'::text, 'ACTIVE'::text, 'GIT_MIRROR_FAILED'::text, 'SYNC_FAILED'::text, 'DRIFT_ALERT'::text, 'SUPERSEDED'::text, 'ROLLING_BACK'::text, 'ROLLED_BACK'::text, 'ROLLBACK_FAILED'::text, 'CANCELLED'::text, 'ARCHIVED'::text])))
);


--
-- Name: COLUMN releases.image_attestation_rekor_url; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.releases.image_attestation_rekor_url IS 'Rekor entry URL produced by cosign attest --predicate <sbom> step (SLSA L3 chain). Empty/NULL when CosignAttestEnabled=false or attest step never ran. Per ADR-2026-05-27-T2.2-OFFLINE-TUF + plan T2.1 of 2026-05-27-tenant-build-pipeline-gaps-closure.';


--
-- Name: CONSTRAINT releases_status_check ON releases; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON CONSTRAINT releases_status_check ON public.releases IS 'deep-review-remediation T0.1 — canonical state set matching pkg/statemachine/release.go + ARCHIVED. Drift is enforced by TestStateMachineCheckConstraintInvariant.';


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


--
-- Name: secrets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secrets (
    id text NOT NULL,
    tenant_id text NOT NULL,
    project_id text DEFAULT ''::text NOT NULL,
    environment text DEFAULT ''::text NOT NULL,
    app text DEFAULT ''::text NOT NULL,
    name text NOT NULL,
    encrypted_value bytea NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: sse_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sse_events (
    id bigint NOT NULL,
    build_id text NOT NULL,
    event_type text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: sse_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sse_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sse_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sse_events_id_seq OWNED BY public.sse_events.id;


--
-- Name: subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscriptions (
    id text NOT NULL,
    tenant_id text NOT NULL,
    stripe_customer_id text,
    stripe_subscription_id text,
    plan text DEFAULT 'starter'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    current_period_start timestamp with time zone,
    current_period_end timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    stripe_subscription_item_id text,
    CONSTRAINT subscriptions_plan_check CHECK ((plan = ANY (ARRAY['starter'::text, 'pro'::text, 'team'::text, 'enterprise'::text]))),
    CONSTRAINT subscriptions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'past_due'::text, 'canceled'::text, 'incomplete'::text, 'trialing'::text, 'unpaid'::text])))
);


--
-- Name: tenant_members; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenant_members (
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT tenant_members_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text])))
);


--
-- Name: tenant_tombstones; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenant_tombstones (
    tenant_id text NOT NULL,
    tenant_slug text NOT NULL,
    deleted_at timestamp with time zone DEFAULT now() NOT NULL,
    requested_by text NOT NULL,
    reason text NOT NULL,
    resources_cleaned jsonb DEFAULT '{}'::jsonb NOT NULL,
    resources_failed jsonb,
    anonymized_at timestamp with time zone
);


--
-- Name: tenants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants (
    id text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    slug text NOT NULL,
    owner_id text NOT NULL,
    cell_id text DEFAULT 'cell-default'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: tenants_pending_deletion; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants_pending_deletion (
    tenant_id text NOT NULL,
    requested_at timestamp with time zone DEFAULT now() NOT NULL,
    requested_by text NOT NULL,
    reason text NOT NULL,
    grace_until timestamp with time zone,
    force boolean DEFAULT false NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    last_error text,
    last_error_at timestamp with time zone
);


--
-- Name: loopzero_api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.loopzero_api_keys (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    key_name text NOT NULL,
    key_hash text NOT NULL,
    key_prefix text NOT NULL,
    scopes jsonb NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT loopzero_api_keys_key_name_check CHECK (((length(key_name) >= 1) AND (length(key_name) <= 64))),
    CONSTRAINT loopzero_api_keys_scopes_check CHECK ((jsonb_typeof(scopes) = 'array'::text))
);


--
-- Name: usage_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage_records (
    id text NOT NULL,
    tenant_id text NOT NULL,
    metric text NOT NULL,
    quantity integer DEFAULT 0 NOT NULL,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone NOT NULL,
    reported_to_stripe boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_identity_map; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_identity_map (
    kratos_identity_id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id text NOT NULL,
    email text NOT NULL,
    github_id bigint,
    display_name text DEFAULT ''::text NOT NULL,
    username text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    active_tenant_id text
);


--
-- Name: COLUMN users.active_tenant_id; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON COLUMN public.users.active_tenant_id IS 'Plan T2.5 D8: explicit active-team selection for multi-tenant users. NULL = legacy JWT middleware heuristic. Updated via POST /api/v1/team/switch. ON DELETE SET NULL prevents dangling reference on tenant deletion.';


--
-- Name: webhook_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_events (
    event_id text NOT NULL,
    event_type text NOT NULL,
    status text DEFAULT 'processed'::text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone DEFAULT now() NOT NULL,
    last_error text,
    attempt_count integer DEFAULT 1 NOT NULL,
    payload_hash text
);


--
-- Name: work_queue; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.work_queue (
    id bigint NOT NULL,
    queue text NOT NULL,
    payload jsonb NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 3 NOT NULL,
    claimed_by text,
    claimed_at timestamp with time zone,
    completed_at timestamp with time zone,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: work_queue_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.work_queue_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: work_queue_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.work_queue_id_seq OWNED BY public.work_queue.id;


--
-- Name: audit_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log ALTER COLUMN id SET DEFAULT nextval('public.audit_log_id_seq'::regclass);


--
-- Name: sse_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sse_events ALTER COLUMN id SET DEFAULT nextval('public.sse_events_id_seq'::regclass);


--
-- Name: work_queue id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.work_queue ALTER COLUMN id SET DEFAULT nextval('public.work_queue_id_seq'::regclass);


--
-- Name: app_scaling_config app_scaling_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_scaling_config
    ADD CONSTRAINT app_scaling_config_pkey PRIMARY KEY (id);


--
-- Name: app_scaling_config app_scaling_config_tenant_id_project_id_environment_app_nam_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_scaling_config
    ADD CONSTRAINT app_scaling_config_tenant_id_project_id_environment_app_nam_key UNIQUE (tenant_id, project_id, environment, app_name);


--
-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);


--
-- Name: build_apps build_apps_build_id_app_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_build_id_app_name_key UNIQUE (build_id, app_name);


--
-- Name: build_apps build_apps_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_pkey PRIMARY KEY (id);


--
-- Name: builds builds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.builds
    ADD CONSTRAINT builds_pkey PRIMARY KEY (id);


--
-- Name: cells cells_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cells
    ADD CONSTRAINT cells_pkey PRIMARY KEY (cell_id);


--
-- Name: circuit_breakers circuit_breakers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.circuit_breakers
    ADD CONSTRAINT circuit_breakers_pkey PRIMARY KEY (key);


--
-- Name: cost_counters cost_counters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_counters
    ADD CONSTRAINT cost_counters_pkey PRIMARY KEY (tenant_id, metric, period_start);


--
-- Name: custom_domains custom_domains_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.custom_domains
    ADD CONSTRAINT custom_domains_pkey PRIMARY KEY (id);


--
-- Name: environment_targets environment_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_pkey PRIMARY KEY (tenant_id, project_id, environment);


--
-- Name: github_installations github_installations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.github_installations
    ADD CONSTRAINT github_installations_pkey PRIMARY KEY (id);


--
-- Name: github_webhook_deliveries github_webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.github_webhook_deliveries
    ADD CONSTRAINT github_webhook_deliveries_pkey PRIMARY KEY (delivery_id);


--
-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (key);


--
-- Name: leader_generations leader_generations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.leader_generations
    ADD CONSTRAINT leader_generations_pkey PRIMARY KEY (name, cell_id);


--
-- Name: memory_exports memory_exports_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_exports
    ADD CONSTRAINT memory_exports_pkey PRIMARY KEY (id);


--
-- Name: memory_webhooks memory_webhooks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_webhooks
    ADD CONSTRAINT memory_webhooks_pkey PRIMARY KEY (id);


--
-- Name: plan_limits plan_limits_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plan_limits
    ADD CONSTRAINT plan_limits_pkey PRIMARY KEY (plan, metric);


--
-- Name: preview_environments preview_environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_pkey PRIMARY KEY (id);


--
-- Name: preview_slot preview_slot_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_slot
    ADD CONSTRAINT preview_slot_pkey PRIMARY KEY (tenant_id, project_id);


--
-- Name: project_repositories project_repositories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_pkey PRIMARY KEY (id);


--
-- Name: project_repositories project_repositories_repo_full_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_repo_full_name_key UNIQUE (repo_full_name);


--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: projects projects_tenant_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_tenant_id_name_key UNIQUE (tenant_id, name);


--
-- Name: release_app_attestations release_app_attestations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_pkey PRIMARY KEY (id);


--
-- Name: release_app_attestations release_app_attestations_unique_app; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_unique_app UNIQUE (release_id, app_name);


--
-- Name: releases releases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_pkey PRIMARY KEY (id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: secrets secrets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_pkey PRIMARY KEY (id);


--
-- Name: secrets secrets_tenant_id_project_id_environment_app_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_tenant_id_project_id_environment_app_name_key UNIQUE (tenant_id, project_id, environment, app, name);


--
-- Name: sse_events sse_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sse_events
    ADD CONSTRAINT sse_events_pkey PRIMARY KEY (id);


--
-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);


--
-- Name: subscriptions subscriptions_tenant_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_tenant_id_key UNIQUE (tenant_id);


--
-- Name: tenant_members tenant_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_pkey PRIMARY KEY (tenant_id, user_id);


--
-- Name: tenant_tombstones tenant_tombstones_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenant_tombstones
    ADD CONSTRAINT tenant_tombstones_pkey PRIMARY KEY (tenant_id);


--
-- Name: tenants_pending_deletion tenants_pending_deletion_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants_pending_deletion
    ADD CONSTRAINT tenants_pending_deletion_pkey PRIMARY KEY (tenant_id);


--
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);


--
-- Name: loopzero_api_keys loopzero_api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_pkey PRIMARY KEY (id);


--
-- Name: usage_records usage_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_pkey PRIMARY KEY (id);


--
-- Name: user_identity_map user_identity_map_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_identity_map
    ADD CONSTRAINT user_identity_map_pkey PRIMARY KEY (kratos_identity_id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: webhook_events webhook_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_events
    ADD CONSTRAINT webhook_events_pkey PRIMARY KEY (event_id);


--
-- Name: work_queue work_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.work_queue
    ADD CONSTRAINT work_queue_pkey PRIMARY KEY (id);


--
-- Name: idx_app_scaling_config_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_app_scaling_config_lookup ON public.app_scaling_config USING btree (tenant_id, project_id, environment);


--
-- Name: idx_audit_log_created_at_brin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_log_created_at_brin ON public.audit_log USING brin (created_at) WITH (pages_per_range='32');


--
-- Name: INDEX idx_audit_log_created_at_brin; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON INDEX public.idx_audit_log_created_at_brin IS 'deep-review-remediation T7.3 — enables time-range retention scans without seq scan.';


--
-- Name: idx_build_apps_build_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_build_apps_build_id ON public.build_apps USING btree (build_id);


--
-- Name: INDEX idx_build_apps_build_id; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON INDEX public.idx_build_apps_build_id IS 'deep-review-2026-05-10 T3.2 — required for FK cascade delete + GetBuildApps query.';


--
-- Name: idx_builds_cell_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_builds_cell_status_created ON public.builds USING btree (cell_id, status, created_at);


--
-- Name: idx_builds_processing_since; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_builds_processing_since ON public.builds USING btree (processing_since) WHERE (processing_by IS NOT NULL);


--
-- Name: idx_builds_tenant_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_builds_tenant_project ON public.builds USING btree (tenant_id, project_id, created_at DESC);


--
-- Name: idx_cells_status_capacity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_cells_status_capacity ON public.cells USING btree (status, capacity_tenants_max);


--
-- Name: idx_custom_domains_active_cname_check; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_custom_domains_active_cname_check ON public.custom_domains USING btree (last_cname_check) WHERE (status = 'active'::text);


--
-- Name: idx_custom_domains_domain; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_custom_domains_domain ON public.custom_domains USING btree (domain);


--
-- Name: idx_custom_domains_ns_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_custom_domains_ns_active ON public.custom_domains USING btree (status) WHERE (status = 'ns_active'::text);


--
-- Name: idx_custom_domains_pending_ns; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_custom_domains_pending_ns ON public.custom_domains USING btree (cf_zone_status) WHERE (status = 'pending_ns'::text);


--
-- Name: idx_custom_domains_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_custom_domains_status ON public.custom_domains USING btree (status) WHERE (status = ANY (ARRAY['verified'::text, 'provisioning'::text, 'removing'::text]));


--
-- Name: idx_custom_domains_tenant_project_app_env; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_custom_domains_tenant_project_app_env ON public.custom_domains USING btree (tenant_id, project_name, app_name, environment);


--
-- Name: idx_environment_targets_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environment_targets_kind ON public.environment_targets USING btree (tenant_id, kind);


--
-- Name: idx_idempotency_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_idempotency_expires ON public.idempotency_keys USING btree (expires_at);


--
-- Name: idx_leader_generations_heartbeat; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_leader_generations_heartbeat ON public.leader_generations USING btree (heartbeat_at);


--
-- Name: idx_memory_exports_owner_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memory_exports_owner_active ON public.memory_exports USING btree (tenant_id, subject, created_at DESC);


--
-- Name: idx_memory_webhooks_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_memory_webhooks_owner ON public.memory_webhooks USING btree (tenant_id, subject) WHERE (revoked_at IS NULL);


--
-- Name: idx_one_active_build_per_env; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_one_active_build_per_env ON public.builds USING btree (tenant_id, project_id, environment) WHERE (status = ANY (ARRAY['QUEUED'::text, 'RUNNING'::text]));


--
-- Name: idx_pending_deletion_grace; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pending_deletion_grace ON public.tenants_pending_deletion USING btree (grace_until) WHERE (grace_until IS NOT NULL);


--
-- Name: idx_preview_build; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preview_build ON public.preview_environments USING btree (build_id) WHERE (build_id IS NOT NULL);


--
-- Name: idx_preview_cell_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preview_cell_status_created ON public.preview_environments USING btree (cell_id, status, created_at);


--
-- Name: idx_preview_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preview_expires ON public.preview_environments USING btree (expires_at) WHERE ((deleted_at IS NULL) AND (status <> ALL (ARRAY['deleted'::text, 'deleting'::text])));


--
-- Name: idx_preview_slot_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preview_slot_project ON public.preview_slot USING btree (project_id);


--
-- Name: idx_preview_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_preview_tenant_status ON public.preview_environments USING btree (tenant_id, status) WHERE (deleted_at IS NULL);


--
-- Name: idx_project_repos_installation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_repos_installation ON public.project_repositories USING btree (installation_id);


--
-- Name: idx_project_repos_repo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_repos_repo ON public.project_repositories USING btree (repo_full_name);


--
-- Name: idx_project_repos_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_project_repos_tenant ON public.project_repositories USING btree (tenant_id, project_name);


--
-- Name: idx_projects_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_projects_tenant ON public.projects USING btree (tenant_id);


--
-- Name: idx_release_app_attestations_release; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_app_attestations_release ON public.release_app_attestations USING btree (release_id);


--
-- Name: idx_release_app_attestations_signed; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_app_attestations_signed ON public.release_app_attestations USING btree (tenant_id, signed_at) WHERE (signed_at IS NOT NULL);


--
-- Name: idx_release_app_attestations_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_release_app_attestations_tenant ON public.release_app_attestations USING btree (tenant_id, created_at DESC);


--
-- Name: idx_releases_active_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_releases_active_unique ON public.releases USING btree (tenant_id, project_id, environment) WHERE (status = 'ACTIVE'::text);


--
-- Name: idx_releases_build_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_build_id ON public.releases USING btree (build_id, created_at DESC);


--
-- Name: idx_releases_cell_status_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_cell_status_created ON public.releases USING btree (cell_id, status, created_at);


--
-- Name: idx_releases_env; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_env ON public.releases USING btree (tenant_id, project_id, environment, created_at DESC);


--
-- Name: idx_releases_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_pending ON public.releases USING btree (created_at) WHERE (status = 'PENDING'::text);


--
-- Name: idx_releases_processing_since; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_processing_since ON public.releases USING btree (processing_since) WHERE (processing_by IS NOT NULL);


--
-- Name: idx_releases_status_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_releases_status_active ON public.releases USING btree (status) WHERE (status <> 'ARCHIVED'::text);


--
-- Name: INDEX idx_releases_status_active; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON INDEX public.idx_releases_status_active IS 'fix-all-platform-issues-plan T2.3 — exclude ARCHIVED rows from active list queries.';


--
-- Name: idx_secrets_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secrets_lookup ON public.secrets USING btree (tenant_id, project_id, environment, app);


--
-- Name: idx_secrets_tenant_project_env; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secrets_tenant_project_env ON public.secrets USING btree (tenant_id, project_id, environment);


--
-- Name: idx_sse_events_build; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sse_events_build ON public.sse_events USING btree (build_id, id);


--
-- Name: idx_subscriptions_stripe_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_stripe_customer ON public.subscriptions USING btree (stripe_customer_id);


--
-- Name: idx_subscriptions_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant ON public.subscriptions USING btree (tenant_id);


--
-- Name: idx_tenant_members_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_members_user ON public.tenant_members USING btree (user_id);


--
-- Name: idx_tenants_cell_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenants_cell_id ON public.tenants USING btree (cell_id);


--
-- Name: idx_tenants_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenants_slug ON public.tenants USING btree (slug);


--
-- Name: idx_loopzero_api_keys_key_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_loopzero_api_keys_key_hash ON public.loopzero_api_keys USING btree (key_hash);


--
-- Name: idx_loopzero_api_keys_tenant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loopzero_api_keys_tenant ON public.loopzero_api_keys USING btree (tenant_id, subject) WHERE (revoked_at IS NULL);


--
-- Name: idx_tombstones_anonymization; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tombstones_anonymization ON public.tenant_tombstones USING btree (deleted_at) WHERE (anonymized_at IS NULL);


--
-- Name: idx_usage_tenant_metric_period; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_usage_tenant_metric_period ON public.usage_records USING btree (tenant_id, metric, period_start);


--
-- Name: idx_user_identity_map_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_identity_map_user_id ON public.user_identity_map USING btree (user_id);


--
-- Name: idx_users_active_tenant_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_active_tenant_id ON public.users USING btree (active_tenant_id) WHERE (active_tenant_id IS NOT NULL);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: idx_users_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_email_unique ON public.users USING btree (email);


--
-- Name: idx_users_github_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_github_id_unique ON public.users USING btree (github_id) WHERE (github_id IS NOT NULL);


--
-- Name: idx_users_username_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_username_unique ON public.users USING btree (username);


--
-- Name: idx_webhook_deliveries_received; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_received ON public.github_webhook_deliveries USING btree (received_at);


--
-- Name: idx_webhook_events_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_events_type ON public.webhook_events USING btree (event_type);


--
-- Name: idx_work_queue_dequeue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_work_queue_dequeue ON public.work_queue USING btree (queue, status, created_at) WHERE (status = 'pending'::text);


--
-- Name: idx_work_queue_stale; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_work_queue_stale ON public.work_queue USING btree (status, claimed_at) WHERE (status = 'processing'::text);


--
-- Name: uniq_preview_active_per_pr; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uniq_preview_active_per_pr ON public.preview_environments USING btree (tenant_id, project_id, pr_number) WHERE (deleted_at IS NULL);


--
-- Name: INDEX uniq_preview_active_per_pr; Type: COMMENT; Schema: public; Owner: -
--

COMMENT ON INDEX public.uniq_preview_active_per_pr IS 'Enforces one live preview per (tenant, project, PR). Soft-deleted rows (deleted_at IS NOT NULL) are excluded so a reopened PR can re-create a preview.';


--
-- Name: releases releases_attestation_url_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER releases_attestation_url_audit AFTER INSERT ON public.releases FOR EACH ROW EXECUTE FUNCTION public.audit_attestation_url_set();


--
-- Name: releases releases_attestation_url_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER releases_attestation_url_immutable BEFORE UPDATE ON public.releases FOR EACH ROW EXECUTE FUNCTION public.reject_attestation_url_mutation();


--
-- Name: audit_log trg_audit_log_immutable; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER trg_audit_log_immutable AFTER DELETE OR UPDATE ON public.audit_log DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_log_prevent_mutation();


--
-- Name: builds trg_builds_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_builds_populate_cell_id BEFORE INSERT ON public.builds FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();


--
-- Name: cells trg_cells_set_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_cells_set_updated_at BEFORE UPDATE ON public.cells FOR EACH ROW EXECUTE FUNCTION public.set_cells_updated_at();


--
-- Name: preview_environments trg_preview_environments_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER trg_preview_environments_audit AFTER INSERT OR DELETE OR UPDATE ON public.preview_environments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();


--
-- Name: preview_environments trg_preview_environments_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_preview_environments_updated_at BEFORE UPDATE ON public.preview_environments FOR EACH ROW EXECUTE FUNCTION public.preview_environments_set_updated_at();


--
-- Name: preview_environments trg_preview_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_preview_populate_cell_id BEFORE INSERT ON public.preview_environments FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();


--
-- Name: release_app_attestations trg_release_app_attestations_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_release_app_attestations_populate_cell_id BEFORE INSERT ON public.release_app_attestations FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();


--
-- Name: release_app_attestations trg_release_app_attestations_updated_at; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_release_app_attestations_updated_at BEFORE UPDATE ON public.release_app_attestations FOR EACH ROW EXECUTE FUNCTION public.release_app_attestations_set_updated_at();


--
-- Name: releases trg_releases_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_releases_populate_cell_id BEFORE INSERT ON public.releases FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();


--
-- Name: secrets trg_secrets_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER trg_secrets_audit AFTER INSERT OR DELETE OR UPDATE ON public.secrets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();


--
-- Name: tenant_members trg_tenant_members_audit; Type: TRIGGER; Schema: public; Owner: -
--

CREATE CONSTRAINT TRIGGER trg_tenant_members_audit AFTER INSERT OR DELETE OR UPDATE ON public.tenant_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();


--
-- Name: build_apps build_apps_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id) ON DELETE CASCADE;


--
-- Name: cost_counters cost_counters_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cost_counters
    ADD CONSTRAINT cost_counters_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: custom_domains custom_domains_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.custom_domains
    ADD CONSTRAINT custom_domains_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;


--
-- Name: environment_targets environment_targets_active_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_active_release_id_fkey FOREIGN KEY (active_release_id) REFERENCES public.releases(id) ON DELETE RESTRICT;


--
-- Name: environment_targets environment_targets_desired_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_desired_release_id_fkey FOREIGN KEY (desired_release_id) REFERENCES public.releases(id) ON DELETE RESTRICT;


--
-- Name: github_installations github_installations_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.github_installations
    ADD CONSTRAINT github_installations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE SET NULL;


--
-- Name: memory_exports memory_exports_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_exports
    ADD CONSTRAINT memory_exports_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: memory_webhooks memory_webhooks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.memory_webhooks
    ADD CONSTRAINT memory_webhooks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: preview_environments preview_environments_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id);


--
-- Name: preview_environments preview_environments_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(id);


--
-- Name: preview_environments preview_environments_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: preview_slot preview_slot_active_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.preview_slot
    ADD CONSTRAINT preview_slot_active_release_id_fkey FOREIGN KEY (active_release_id) REFERENCES public.releases(id) ON DELETE SET NULL;


--
-- Name: project_repositories project_repositories_installation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_installation_id_fkey FOREIGN KEY (installation_id) REFERENCES public.github_installations(id) ON DELETE RESTRICT;


--
-- Name: project_repositories project_repositories_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: projects projects_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: projects projects_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;


--
-- Name: release_app_attestations release_app_attestations_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(id) ON DELETE CASCADE;


--
-- Name: releases releases_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id) ON DELETE RESTRICT;


--
-- Name: subscriptions subscriptions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;


--
-- Name: tenant_members tenant_members_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: tenant_members tenant_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: tenants tenants_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE RESTRICT;


--
-- Name: tenants_pending_deletion tenants_pending_deletion_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants_pending_deletion
    ADD CONSTRAINT tenants_pending_deletion_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: loopzero_api_keys loopzero_api_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;


--
-- Name: usage_records usage_records_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;


--
-- Name: user_identity_map user_identity_map_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_identity_map
    ADD CONSTRAINT user_identity_map_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: users users_active_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_active_tenant_id_fkey FOREIGN KEY (active_tenant_id) REFERENCES public.tenants(id) ON DELETE SET NULL;


--
-- Name: audit_log; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.audit_log ENABLE ROW LEVEL SECURITY;

--
-- Name: audit_log audit_log_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY audit_log_tenant_isolation ON public.audit_log USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: builds; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.builds ENABLE ROW LEVEL SECURITY;

--
-- Name: builds builds_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY builds_tenant_isolation ON public.builds USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: cost_counters; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.cost_counters ENABLE ROW LEVEL SECURITY;

--
-- Name: cost_counters cost_counters_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY cost_counters_tenant_isolation ON public.cost_counters USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: environment_targets; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.environment_targets ENABLE ROW LEVEL SECURITY;

--
-- Name: environment_targets environment_targets_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY environment_targets_tenant_isolation ON public.environment_targets USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: idempotency_keys; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.idempotency_keys ENABLE ROW LEVEL SECURITY;

--
-- Name: idempotency_keys idempotency_keys_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY idempotency_keys_tenant_isolation ON public.idempotency_keys USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: preview_environments; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.preview_environments ENABLE ROW LEVEL SECURITY;

--
-- Name: preview_environments preview_environments_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY preview_environments_tenant_isolation ON public.preview_environments USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: preview_slot; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.preview_slot ENABLE ROW LEVEL SECURITY;

--
-- Name: preview_slot preview_slot_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY preview_slot_tenant_isolation ON public.preview_slot USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: projects; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.projects ENABLE ROW LEVEL SECURITY;

--
-- Name: projects projects_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY projects_tenant_isolation ON public.projects USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: release_app_attestations; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.release_app_attestations ENABLE ROW LEVEL SECURITY;

--
-- Name: release_app_attestations release_app_attestations_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY release_app_attestations_tenant_isolation ON public.release_app_attestations USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: releases; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.releases ENABLE ROW LEVEL SECURITY;

--
-- Name: releases releases_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY releases_tenant_isolation ON public.releases USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: secrets; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.secrets ENABLE ROW LEVEL SECURITY;

--
-- Name: secrets secrets_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY secrets_tenant_isolation ON public.secrets USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- Name: tenant_members; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.tenant_members ENABLE ROW LEVEL SECURITY;

--
-- Name: tenant_members tenant_members_tenant_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY tenant_members_tenant_isolation ON public.tenant_members USING (((tenant_id = current_setting('app.tenant_id'::text, true)) OR (current_setting('app.tenant_id'::text, true) IS NULL) OR (current_setting('app.tenant_id'::text, true) = ''::text)));


--
-- PostgreSQL database dump complete
--


-- PostgreSQL database dump

-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;

-- Name: audit_attestation_url_set(); Type: FUNCTION; Schema: public; Owner: -

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

-- Name: audit_log_prevent_mutation(); Type: FUNCTION; Schema: public; Owner: -

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

-- Name: audit_sensitive_mutation(); Type: FUNCTION; Schema: public; Owner: -

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

-- Name: metering_cost(bigint, interval); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_cost(req_millicore bigint, dur interval) RETURNS bigint
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT (req_millicore * EXTRACT(EPOCH FROM dur) / 1000)::bigint $$;

-- Name: metering_period_end(timestamp with time zone); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_period_end(ts timestamp with time zone) RETURNS timestamp with time zone
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT metering_period_start(ts) + interval '1 month' $$;

-- Name: metering_period_of(timestamp with time zone); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_period_of(ts timestamp with time zone) RETURNS timestamp with time zone
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT metering_period_start(ts) $$;

-- Name: metering_period_start(timestamp with time zone); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_period_start(ts timestamp with time zone) RETURNS timestamp with time zone
    LANGUAGE sql IMMUTABLE PARALLEL SAFE
    AS $$ SELECT date_trunc('month', ts AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' $$;

-- Name: metering_split_head(timestamp with time zone, interval); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_split_head(expected timestamp with time zone, max_delta interval) RETURNS interval
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT GREATEST(LEAST(now(), expected + max_delta) - metering_period_end(expected), interval '0') $$;

-- Name: metering_split_tail(timestamp with time zone, interval); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.metering_split_tail(expected timestamp with time zone, max_delta interval) RETURNS interval
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT GREATEST(LEAST(metering_period_end(expected), LEAST(now(), expected + max_delta)) - expected, interval '0') $$;

-- Name: populate_cell_id_from_tenant(); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            RAISE EXCEPTION
                'cell_id not resolved for tenant % in table %: the tenant does not exist or has no cell',
                NEW.tenant_id, TG_TABLE_NAME;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- Name: preview_environments_set_updated_at(); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.preview_environments_set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Name: reject_attestation_url_mutation(); Type: FUNCTION; Schema: public; Owner: -

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

-- Name: release_app_attestations_set_updated_at(); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.release_app_attestations_set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Name: set_cells_updated_at(); Type: FUNCTION; Schema: public; Owner: -

CREATE FUNCTION public.set_cells_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;

-- Name: accounts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.accounts (
    id text NOT NULL,
    display_name text NOT NULL,
    plan text DEFAULT 'starter'::text NOT NULL,
    owner_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Name: app_scaling_config; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.app_scaling_config FORCE ROW LEVEL SECURITY;

-- Name: audit_log; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.audit_log FORCE ROW LEVEL SECURITY;

-- Name: audit_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -

CREATE SEQUENCE public.audit_log_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Name: audit_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -

ALTER SEQUENCE public.audit_log_id_seq OWNED BY public.audit_log.id;

-- Name: build_apps; Type: TABLE; Schema: public; Owner: -

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

-- Name: builds; Type: TABLE; Schema: public; Owner: -

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
    cell_id text NOT NULL,
    processing_by text,
    processing_since timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    bundle_digest text,
    CONSTRAINT builds_status_check CHECK ((status = ANY (ARRAY['QUEUED'::text, 'RUNNING'::text, 'RELEASING'::text, 'SUCCEEDED'::text, 'FAILED'::text, 'CANCELLING'::text, 'CANCELLED'::text])))
);

ALTER TABLE ONLY public.builds FORCE ROW LEVEL SECURITY;

-- Name: cells; Type: TABLE; Schema: public; Owner: -

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

-- Name: circuit_breakers; Type: TABLE; Schema: public; Owner: -

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

-- Name: cost_counters; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.cost_counters (
    tenant_id text NOT NULL,
    plan_tier text NOT NULL,
    metric text NOT NULL,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone NOT NULL,
    value bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT cost_counters_metric_check CHECK ((metric = ANY (ARRAY['egress_bytes'::text, 'docr_pulls'::text, 'build_minutes'::text, 'cpu_core_seconds'::text, 'mem_gb_seconds'::text]))),
    CONSTRAINT cost_counters_plan_tier_check CHECK ((plan_tier = ANY (ARRAY['starter'::text, 'pro'::text, 'team'::text, 'enterprise'::text])))
);

ALTER TABLE ONLY public.cost_counters FORCE ROW LEVEL SECURITY;

-- Name: custom_domains; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.custom_domains FORCE ROW LEVEL SECURITY;

-- Name: deploy_events; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.deploy_events (
    id bigint NOT NULL,
    transition_id text NOT NULL,
    event_type text NOT NULL,
    tenant_id text NOT NULL,
    project_id text NOT NULL,
    deploy_id text NOT NULL,
    correlation_id text,
    payload jsonb NOT NULL,
    occurred_at timestamp with time zone DEFAULT now() NOT NULL,
    drained_at timestamp with time zone,
    CONSTRAINT deploy_events_event_type_check CHECK ((event_type = ANY (ARRAY['deploy.succeeded'::text, 'deploy.failed'::text]))),
    CONSTRAINT deploy_events_payload_check CHECK ((jsonb_typeof(payload) = 'object'::text))
);

ALTER TABLE ONLY public.deploy_events FORCE ROW LEVEL SECURITY;

-- Name: deploy_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -

CREATE SEQUENCE public.deploy_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Name: deploy_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -

ALTER SEQUENCE public.deploy_events_id_seq OWNED BY public.deploy_events.id;

-- Name: environment_targets; Type: TABLE; Schema: public; Owner: -

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
    last_replicas_ready_value integer,
    last_replicas_ready_at timestamp with time zone,
    CONSTRAINT environment_targets_kind_check CHECK ((kind = ANY (ARRAY['permanent'::text, 'preview'::text])))
);

ALTER TABLE ONLY public.environment_targets FORCE ROW LEVEL SECURITY;

-- Name: github_installations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.github_installations (
    id bigint NOT NULL,
    account_login text NOT NULL,
    account_type text DEFAULT 'User'::text NOT NULL,
    tenant_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    verified_at timestamp with time zone,
    verified_by text
);

ALTER TABLE ONLY public.github_installations FORCE ROW LEVEL SECURITY;

-- Name: github_webhook_deliveries; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.github_webhook_deliveries (
    delivery_id text NOT NULL,
    event_type text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Name: idempotency_keys; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.idempotency_keys FORCE ROW LEVEL SECURITY;

-- Name: leader_generations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.leader_generations (
    name text NOT NULL,
    cell_id text DEFAULT 'cell-0'::text NOT NULL,
    holder text NOT NULL,
    generation_id bigint DEFAULT 1 NOT NULL,
    acquired_at timestamp with time zone DEFAULT now() NOT NULL,
    heartbeat_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Name: lens_alert_rules; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.lens_alert_rules (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    metric text NOT NULL,
    comparator text NOT NULL,
    threshold double precision NOT NULL,
    window_seconds integer NOT NULL,
    webhook_id text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    status text DEFAULT 'OK'::text NOT NULL,
    last_evaluated_at timestamp with time zone,
    last_triggered_at timestamp with time zone,
    renotify_interval_seconds integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    monitor_id text,
    prompt_name text,
    CONSTRAINT lens_alert_rules_comparator_check CHECK ((comparator = ANY (ARRAY['gt'::text, 'gte'::text, 'lt'::text, 'lte'::text]))),
    CONSTRAINT lens_alert_rules_metric_check CHECK ((metric = ANY (ARRAY['error_rate'::text, 'cost'::text, 'p95_latency'::text, 'monitor_breach'::text, 'prompt_resolutions'::text]))),
    CONSTRAINT lens_alert_rules_monitor_pairing_check CHECK (((metric = 'monitor_breach'::text) = (monitor_id IS NOT NULL))),
    CONSTRAINT lens_alert_rules_name_check CHECK (((length(name) >= 1) AND (length(name) <= 64))),
    CONSTRAINT lens_alert_rules_prompt_pairing_check CHECK (((metric = 'prompt_resolutions'::text) = (prompt_name IS NOT NULL))),
    CONSTRAINT lens_alert_rules_renotify_interval_seconds_check CHECK (((renotify_interval_seconds IS NULL) OR (renotify_interval_seconds > 0))),
    CONSTRAINT lens_alert_rules_status_check CHECK ((status = ANY (ARRAY['OK'::text, 'TRIGGERED'::text]))),
    CONSTRAINT lens_alert_rules_window_seconds_check CHECK ((window_seconds > 0))
);

ALTER TABLE ONLY public.lens_alert_rules FORCE ROW LEVEL SECURITY;

-- Name: lens_dashboards; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.lens_dashboards (
    id text NOT NULL,
    tenant_id text NOT NULL,
    created_by text NOT NULL,
    name text NOT NULL,
    config jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT lens_dashboards_name_check CHECK (((length(name) >= 1) AND (length(name) <= 64)))
);

-- Name: lens_fired_alerts; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.lens_fired_alerts (
    id text NOT NULL,
    rule_id text NOT NULL,
    tenant_id text NOT NULL,
    metric text NOT NULL,
    comparator text NOT NULL,
    threshold double precision NOT NULL,
    value double precision NOT NULL,
    fired_at timestamp with time zone NOT NULL,
    transition_id text NOT NULL,
    delivery_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT lens_fired_alerts_comparator_check CHECK ((comparator = ANY (ARRAY['gt'::text, 'gte'::text, 'lt'::text, 'lte'::text]))),
    CONSTRAINT lens_fired_alerts_metric_check CHECK ((metric = ANY (ARRAY['error_rate'::text, 'cost'::text, 'p95_latency'::text, 'monitor_breach'::text, 'prompt_resolutions'::text])))
);

-- Name: lens_saved_views; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.lens_saved_views (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    query_string text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT lens_saved_views_name_check CHECK (((length(name) >= 1) AND (length(name) <= 64))),
    CONSTRAINT lens_saved_views_query_string_check CHECK (((length(query_string) >= 1) AND (length(query_string) <= 4096)))
);

ALTER TABLE ONLY public.lens_saved_views FORCE ROW LEVEL SECURITY;

-- Name: loopzero_api_keys; Type: TABLE; Schema: public; Owner: -

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
    bff_minted boolean DEFAULT false NOT NULL,
    rotated_from text,
    account_id text NOT NULL,
    CONSTRAINT loopzero_api_keys_key_name_check CHECK (((length(key_name) >= 1) AND (length(key_name) <= 64))),
    CONSTRAINT loopzero_api_keys_scopes_check CHECK ((jsonb_typeof(scopes) = 'array'::text))
);

ALTER TABLE ONLY public.loopzero_api_keys FORCE ROW LEVEL SECURITY;

-- Name: memory_exports; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.memory_exports FORCE ROW LEVEL SECURITY;

-- Name: memory_webhook_deliveries; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.memory_webhook_deliveries (
    id text NOT NULL,
    webhook_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone,
    last_response_status integer,
    last_response_body text,
    last_error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT memory_webhook_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'delivering'::text, 'delivered'::text, 'failed'::text, 'dead_letter'::text])))
);

-- Name: memory_webhooks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.memory_webhooks (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    url text NOT NULL,
    label text NOT NULL,
    events jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    secret text,
    CONSTRAINT memory_webhooks_events_check CHECK ((jsonb_typeof(events) = 'array'::text)),
    CONSTRAINT memory_webhooks_label_check CHECK (((length(label) >= 1) AND (length(label) <= 64))),
    CONSTRAINT memory_webhooks_url_check CHECK ((url ~* '^https?://'::text))
);

ALTER TABLE ONLY public.memory_webhooks FORCE ROW LEVEL SECURITY;

-- Name: metering_config; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.metering_config (
    id boolean DEFAULT true NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    CONSTRAINT metering_config_singleton CHECK ((id = true))
);

-- Name: metering_cursor; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.metering_cursor (
    tenant_id text NOT NULL,
    metric text NOT NULL,
    cell_id text NOT NULL,
    last_metered_at timestamp with time zone NOT NULL
);

ALTER TABLE ONLY public.metering_cursor FORCE ROW LEVEL SECURITY;

-- Name: plan_limits; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.plan_limits (
    plan text NOT NULL,
    metric text NOT NULL,
    hard_limit integer,
    soft_limit integer
);

-- Name: preview_environments; Type: TABLE; Schema: public; Owner: -

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
    cell_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT preview_environments_id_format CHECK ((id ~~ 'prev-%'::text)),
    CONSTRAINT preview_environments_pr_number_check CHECK ((pr_number > 0)),
    CONSTRAINT preview_environments_status_check CHECK ((status = ANY (ARRAY['creating'::text, 'active'::text, 'expiring'::text, 'deleting'::text, 'deleted'::text, 'failed'::text, 'cleanup_failed'::text])))
);

ALTER TABLE ONLY public.preview_environments FORCE ROW LEVEL SECURITY;

-- Name: preview_slot; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.preview_slot FORCE ROW LEVEL SECURITY;

-- Name: project_repositories; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.project_repositories FORCE ROW LEVEL SECURITY;

-- Name: projects; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.projects (
    id text NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    created_by text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.projects FORCE ROW LEVEL SECURITY;

-- Name: release_app_attestations; Type: TABLE; Schema: public; Owner: -

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
    cell_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT release_app_attestations_digest_format CHECK ((image_digest ~~ 'sha256:%'::text)),
    CONSTRAINT release_app_attestations_id_format CHECK ((id ~~ 'raa-%'::text)),
    CONSTRAINT release_app_attestations_sbom_count_nonneg CHECK (((sbom_component_count IS NULL) OR (sbom_component_count >= 0))),
    CONSTRAINT release_app_attestations_sbom_format_value CHECK (((sbom_format IS NULL) OR (sbom_format = ANY (ARRAY['cyclonedx-json'::text, 'spdx-json'::text]))))
);

ALTER TABLE ONLY public.release_app_attestations FORCE ROW LEVEL SECURITY;

-- Name: releases; Type: TABLE; Schema: public; Owner: -

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
    cell_id text NOT NULL,
    processing_by text,
    processing_since timestamp with time zone,
    image_digest text,
    image_signature text,
    image_signed_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    image_attestation_rekor_url text,
    CONSTRAINT releases_status_check CHECK ((status = ANY (ARRAY['READY'::text, 'PENDING'::text, 'ACTIVE'::text, 'GIT_MIRROR_FAILED'::text, 'SYNC_FAILED'::text, 'DRIFT_ALERT'::text, 'SUPERSEDED'::text, 'ROLLING_BACK'::text, 'ROLLED_BACK'::text, 'ROLLBACK_FAILED'::text, 'CANCELLED'::text, 'ARCHIVED'::text])))
);

ALTER TABLE ONLY public.releases FORCE ROW LEVEL SECURITY;

-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);

-- Name: secrets; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.secrets FORCE ROW LEVEL SECURITY;

-- Name: sse_events; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.sse_events (
    id bigint NOT NULL,
    build_id text NOT NULL,
    event_type text NOT NULL,
    data jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Name: sse_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -

CREATE SEQUENCE public.sse_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Name: sse_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -

ALTER SEQUENCE public.sse_events_id_seq OWNED BY public.sse_events.id;

-- Name: storage_objects; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.storage_objects (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id text NOT NULL,
    name text NOT NULL,
    size_bytes bigint NOT NULL,
    mime_type text DEFAULT 'application/octet-stream'::text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT storage_objects_size_bytes_check CHECK ((size_bytes >= 0)),
    CONSTRAINT storage_objects_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'committed'::text])))
);

-- Name: subscriptions; Type: TABLE; Schema: public; Owner: -

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
    account_id text NOT NULL,
    amount_minor bigint,
    currency text,
    CONSTRAINT subscriptions_plan_check CHECK ((plan = ANY (ARRAY['starter'::text, 'pro'::text, 'team'::text, 'enterprise'::text]))),
    CONSTRAINT subscriptions_status_check CHECK ((status = ANY (ARRAY['active'::text, 'past_due'::text, 'canceled'::text, 'incomplete'::text, 'trialing'::text, 'unpaid'::text])))
);

ALTER TABLE ONLY public.subscriptions FORCE ROW LEVEL SECURITY;

-- Name: tenant_members; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.tenant_members (
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT tenant_members_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text])))
);

ALTER TABLE ONLY public.tenant_members FORCE ROW LEVEL SECURITY;

-- Name: tenant_tombstones; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.tenant_tombstones FORCE ROW LEVEL SECURITY;

-- Name: tenants; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.tenants (
    id text NOT NULL,
    display_name text DEFAULT ''::text NOT NULL,
    slug text NOT NULL,
    owner_id text NOT NULL,
    cell_id text DEFAULT 'cell-0'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    account_id text NOT NULL
);

ALTER TABLE ONLY public.tenants FORCE ROW LEVEL SECURITY;

-- Name: tenants_pending_deletion; Type: TABLE; Schema: public; Owner: -

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

ALTER TABLE ONLY public.tenants_pending_deletion FORCE ROW LEVEL SECURITY;

-- Name: usage_records; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.usage_records (
    id text NOT NULL,
    tenant_id text NOT NULL,
    metric text NOT NULL,
    quantity bigint DEFAULT 0 NOT NULL,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone NOT NULL,
    reported_to_stripe boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

ALTER TABLE ONLY public.usage_records FORCE ROW LEVEL SECURITY;

-- Name: user_identity_map; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.user_identity_map (
    kratos_identity_id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

-- Name: users; Type: TABLE; Schema: public; Owner: -

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

-- Name: webhook_events; Type: TABLE; Schema: public; Owner: -

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

-- Name: work_queue; Type: TABLE; Schema: public; Owner: -

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

-- Name: work_queue_id_seq; Type: SEQUENCE; Schema: public; Owner: -

CREATE SEQUENCE public.work_queue_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- Name: work_queue_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -

ALTER SEQUENCE public.work_queue_id_seq OWNED BY public.work_queue.id;

-- Name: workspace_webhook_deliveries; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.workspace_webhook_deliveries (
    id text NOT NULL,
    webhook_id text NOT NULL,
    transition_id text NOT NULL,
    tenant_id text NOT NULL,
    event_type text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone,
    claimed_at timestamp with time zone,
    claimed_by text,
    last_response_status integer,
    last_response_body text,
    last_error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT workspace_webhook_deliveries_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'delivering'::text, 'delivered'::text, 'failed'::text, 'dead_letter'::text])))
);

ALTER TABLE ONLY public.workspace_webhook_deliveries FORCE ROW LEVEL SECURITY;

-- Name: workspace_webhooks; Type: TABLE; Schema: public; Owner: -

CREATE TABLE public.workspace_webhooks (
    id text NOT NULL,
    subject text NOT NULL,
    tenant_id text NOT NULL,
    url text NOT NULL,
    label text NOT NULL,
    events jsonb NOT NULL,
    secret text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    revoked_at timestamp with time zone,
    CONSTRAINT workspace_webhooks_events_check CHECK ((jsonb_typeof(events) = 'array'::text)),
    CONSTRAINT workspace_webhooks_label_check CHECK (((length(label) >= 1) AND (length(label) <= 64))),
    CONSTRAINT workspace_webhooks_url_check CHECK ((url ~* '^https?://'::text))
);

ALTER TABLE ONLY public.workspace_webhooks FORCE ROW LEVEL SECURITY;

-- Name: audit_log id; Type: DEFAULT; Schema: public; Owner: -

ALTER TABLE ONLY public.audit_log ALTER COLUMN id SET DEFAULT nextval('public.audit_log_id_seq'::regclass);

-- Name: deploy_events id; Type: DEFAULT; Schema: public; Owner: -

ALTER TABLE ONLY public.deploy_events ALTER COLUMN id SET DEFAULT nextval('public.deploy_events_id_seq'::regclass);

-- Name: sse_events id; Type: DEFAULT; Schema: public; Owner: -

ALTER TABLE ONLY public.sse_events ALTER COLUMN id SET DEFAULT nextval('public.sse_events_id_seq'::regclass);

-- Name: work_queue id; Type: DEFAULT; Schema: public; Owner: -

ALTER TABLE ONLY public.work_queue ALTER COLUMN id SET DEFAULT nextval('public.work_queue_id_seq'::regclass);

-- Name: accounts accounts_owner_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_owner_id_key UNIQUE (owner_id);

-- Name: accounts accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);

-- Name: app_scaling_config app_scaling_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.app_scaling_config
    ADD CONSTRAINT app_scaling_config_pkey PRIMARY KEY (id);

-- Name: app_scaling_config app_scaling_config_tenant_id_project_id_environment_app_nam_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.app_scaling_config
    ADD CONSTRAINT app_scaling_config_tenant_id_project_id_environment_app_nam_key UNIQUE (tenant_id, project_id, environment, app_name);

-- Name: audit_log audit_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.audit_log
    ADD CONSTRAINT audit_log_pkey PRIMARY KEY (id);

-- Name: build_apps build_apps_build_id_app_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_build_id_app_name_key UNIQUE (build_id, app_name);

-- Name: build_apps build_apps_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_pkey PRIMARY KEY (id);

-- Name: builds builds_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.builds
    ADD CONSTRAINT builds_pkey PRIMARY KEY (id);

-- Name: cells cells_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.cells
    ADD CONSTRAINT cells_pkey PRIMARY KEY (cell_id);

-- Name: circuit_breakers circuit_breakers_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.circuit_breakers
    ADD CONSTRAINT circuit_breakers_pkey PRIMARY KEY (key);

-- Name: cost_counters cost_counters_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.cost_counters
    ADD CONSTRAINT cost_counters_pkey PRIMARY KEY (tenant_id, metric, period_start);

-- Name: custom_domains custom_domains_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.custom_domains
    ADD CONSTRAINT custom_domains_pkey PRIMARY KEY (id);

-- Name: deploy_events deploy_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.deploy_events
    ADD CONSTRAINT deploy_events_pkey PRIMARY KEY (id);

-- Name: deploy_events deploy_events_transition_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.deploy_events
    ADD CONSTRAINT deploy_events_transition_id_key UNIQUE (transition_id);

-- Name: environment_targets environment_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_pkey PRIMARY KEY (tenant_id, project_id, environment);

-- Name: github_installations github_installations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.github_installations
    ADD CONSTRAINT github_installations_pkey PRIMARY KEY (id);

-- Name: github_webhook_deliveries github_webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.github_webhook_deliveries
    ADD CONSTRAINT github_webhook_deliveries_pkey PRIMARY KEY (delivery_id);

-- Name: idempotency_keys idempotency_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.idempotency_keys
    ADD CONSTRAINT idempotency_keys_pkey PRIMARY KEY (tenant_id, key);

-- Name: leader_generations leader_generations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.leader_generations
    ADD CONSTRAINT leader_generations_pkey PRIMARY KEY (name, cell_id);

-- Name: lens_alert_rules lens_alert_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_alert_rules
    ADD CONSTRAINT lens_alert_rules_pkey PRIMARY KEY (id);

-- Name: lens_dashboards lens_dashboards_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_dashboards
    ADD CONSTRAINT lens_dashboards_pkey PRIMARY KEY (id);

-- Name: lens_fired_alerts lens_fired_alerts_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_fired_alerts
    ADD CONSTRAINT lens_fired_alerts_pkey PRIMARY KEY (id);

-- Name: lens_saved_views lens_saved_views_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_saved_views
    ADD CONSTRAINT lens_saved_views_pkey PRIMARY KEY (id);

-- Name: loopzero_api_keys loopzero_api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_pkey PRIMARY KEY (id);

-- Name: memory_exports memory_exports_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_exports
    ADD CONSTRAINT memory_exports_pkey PRIMARY KEY (id);

-- Name: memory_webhook_deliveries memory_webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_webhook_deliveries
    ADD CONSTRAINT memory_webhook_deliveries_pkey PRIMARY KEY (id);

-- Name: memory_webhooks memory_webhooks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_webhooks
    ADD CONSTRAINT memory_webhooks_pkey PRIMARY KEY (id);

-- Name: metering_config metering_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.metering_config
    ADD CONSTRAINT metering_config_pkey PRIMARY KEY (id);

-- Name: metering_cursor metering_cursor_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.metering_cursor
    ADD CONSTRAINT metering_cursor_pkey PRIMARY KEY (tenant_id, metric, cell_id);

-- Name: plan_limits plan_limits_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.plan_limits
    ADD CONSTRAINT plan_limits_pkey PRIMARY KEY (plan, metric);

-- Name: preview_environments preview_environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_pkey PRIMARY KEY (id);

-- Name: preview_slot preview_slot_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_slot
    ADD CONSTRAINT preview_slot_pkey PRIMARY KEY (tenant_id, project_id);

-- Name: project_repositories project_repositories_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_pkey PRIMARY KEY (id);

-- Name: project_repositories project_repositories_repo_full_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_repo_full_name_key UNIQUE (repo_full_name);

-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);

-- Name: projects projects_tenant_id_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_tenant_id_name_key UNIQUE (tenant_id, name);

-- Name: release_app_attestations release_app_attestations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_pkey PRIMARY KEY (id);

-- Name: release_app_attestations release_app_attestations_unique_app; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_unique_app UNIQUE (release_id, app_name);

-- Name: releases releases_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_pkey PRIMARY KEY (id);

-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);

-- Name: secrets secrets_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_pkey PRIMARY KEY (id);

-- Name: secrets secrets_tenant_id_project_id_environment_app_name_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_tenant_id_project_id_environment_app_name_key UNIQUE (tenant_id, project_id, environment, app, name);

-- Name: sse_events sse_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.sse_events
    ADD CONSTRAINT sse_events_pkey PRIMARY KEY (id);

-- Name: storage_objects storage_objects_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.storage_objects
    ADD CONSTRAINT storage_objects_pkey PRIMARY KEY (id);

-- Name: storage_objects storage_objects_tenant_name_uniq; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.storage_objects
    ADD CONSTRAINT storage_objects_tenant_name_uniq UNIQUE (tenant_id, name);

-- Name: subscriptions subscriptions_account_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_account_id_key UNIQUE (account_id);

-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);

-- Name: tenant_members tenant_members_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_pkey PRIMARY KEY (tenant_id, user_id);

-- Name: tenant_tombstones tenant_tombstones_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenant_tombstones
    ADD CONSTRAINT tenant_tombstones_pkey PRIMARY KEY (tenant_id);

-- Name: tenants_pending_deletion tenants_pending_deletion_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenants_pending_deletion
    ADD CONSTRAINT tenants_pending_deletion_pkey PRIMARY KEY (tenant_id);

-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);

-- Name: usage_records usage_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_pkey PRIMARY KEY (id);

-- Name: user_identity_map user_identity_map_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_identity_map
    ADD CONSTRAINT user_identity_map_pkey PRIMARY KEY (kratos_identity_id);

-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

-- Name: webhook_events webhook_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.webhook_events
    ADD CONSTRAINT webhook_events_pkey PRIMARY KEY (event_id);

-- Name: work_queue work_queue_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.work_queue
    ADD CONSTRAINT work_queue_pkey PRIMARY KEY (id);

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workspace_webhook_deliveries
    ADD CONSTRAINT workspace_webhook_deliveries_pkey PRIMARY KEY (id);

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_webhook_id_transition_id_key; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workspace_webhook_deliveries
    ADD CONSTRAINT workspace_webhook_deliveries_webhook_id_transition_id_key UNIQUE (webhook_id, transition_id);

-- Name: workspace_webhooks workspace_webhooks_pkey; Type: CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workspace_webhooks
    ADD CONSTRAINT workspace_webhooks_pkey PRIMARY KEY (id);

-- Name: idx_accounts_owner_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_accounts_owner_id ON public.accounts USING btree (owner_id);

-- Name: idx_app_scaling_config_lookup; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_app_scaling_config_lookup ON public.app_scaling_config USING btree (tenant_id, project_id, environment);

-- Name: idx_audit_log_created_at_brin; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_audit_log_created_at_brin ON public.audit_log USING brin (created_at) WITH (pages_per_range='32');

-- Name: idx_build_apps_build_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_build_apps_build_id ON public.build_apps USING btree (build_id);

-- Name: idx_builds_cell_status_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_builds_cell_status_created ON public.builds USING btree (cell_id, status, created_at);

-- Name: idx_builds_processing_since; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_builds_processing_since ON public.builds USING btree (processing_since) WHERE (processing_by IS NOT NULL);

-- Name: idx_builds_tenant_project; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_builds_tenant_project ON public.builds USING btree (tenant_id, project_id, created_at DESC);

-- Name: idx_cells_status_capacity; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_cells_status_capacity ON public.cells USING btree (status, capacity_tenants_max);

-- Name: idx_custom_domains_active_cname_check; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_custom_domains_active_cname_check ON public.custom_domains USING btree (last_cname_check) WHERE (status = 'active'::text);

-- Name: idx_custom_domains_domain; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_custom_domains_domain ON public.custom_domains USING btree (domain);

-- Name: idx_custom_domains_ns_active; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_custom_domains_ns_active ON public.custom_domains USING btree (status) WHERE (status = 'ns_active'::text);

-- Name: idx_custom_domains_pending_ns; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_custom_domains_pending_ns ON public.custom_domains USING btree (cf_zone_status) WHERE (status = 'pending_ns'::text);

-- Name: idx_custom_domains_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_custom_domains_status ON public.custom_domains USING btree (status) WHERE (status = ANY (ARRAY['verified'::text, 'provisioning'::text, 'removing'::text]));

-- Name: idx_custom_domains_tenant_project_app_env; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_custom_domains_tenant_project_app_env ON public.custom_domains USING btree (tenant_id, project_name, app_name, environment);

-- Name: idx_deploy_events_undrained; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_deploy_events_undrained ON public.deploy_events USING btree (id) WHERE (drained_at IS NULL);

-- Name: idx_env_targets_last_replicas_ready_at; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_env_targets_last_replicas_ready_at ON public.environment_targets USING btree (last_replicas_ready_at DESC) WHERE (last_replicas_ready_at IS NOT NULL);

-- Name: idx_environment_targets_kind; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_environment_targets_kind ON public.environment_targets USING btree (tenant_id, kind);

-- Name: idx_idempotency_expires; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_idempotency_expires ON public.idempotency_keys USING btree (expires_at);

-- Name: idx_leader_generations_heartbeat; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_leader_generations_heartbeat ON public.leader_generations USING btree (heartbeat_at);

-- Name: idx_lens_alert_rules_enabled; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_alert_rules_enabled ON public.lens_alert_rules USING btree (tenant_id) WHERE enabled;

-- Name: idx_lens_alert_rules_monitor; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_alert_rules_monitor ON public.lens_alert_rules USING btree (monitor_id) WHERE (monitor_id IS NOT NULL);

-- Name: idx_lens_alert_rules_owner; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_alert_rules_owner ON public.lens_alert_rules USING btree (tenant_id, subject);

-- Name: idx_lens_alert_rules_prompt; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_alert_rules_prompt ON public.lens_alert_rules USING btree (prompt_name) WHERE (prompt_name IS NOT NULL);

-- Name: idx_lens_dashboards_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_dashboards_tenant ON public.lens_dashboards USING btree (tenant_id);

-- Name: idx_lens_fired_alerts_rule_time; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_fired_alerts_rule_time ON public.lens_fired_alerts USING btree (rule_id, tenant_id, fired_at DESC);

-- Name: idx_lens_saved_views_owner; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_lens_saved_views_owner ON public.lens_saved_views USING btree (tenant_id, subject);

-- Name: idx_loopzero_api_keys_account_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_loopzero_api_keys_account_id ON public.loopzero_api_keys USING btree (account_id);

-- Name: idx_loopzero_api_keys_bff_minted_expires; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_loopzero_api_keys_bff_minted_expires ON public.loopzero_api_keys USING btree (bff_minted, expires_at) WHERE (bff_minted = true);

-- Name: idx_loopzero_api_keys_key_hash; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_loopzero_api_keys_key_hash ON public.loopzero_api_keys USING btree (key_hash);

-- Name: idx_loopzero_api_keys_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_loopzero_api_keys_tenant ON public.loopzero_api_keys USING btree (tenant_id, subject) WHERE (revoked_at IS NULL);

-- Name: idx_memory_exports_owner_active; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_memory_exports_owner_active ON public.memory_exports USING btree (tenant_id, subject, created_at DESC);

-- Name: idx_memory_webhook_deliveries_due; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_memory_webhook_deliveries_due ON public.memory_webhook_deliveries USING btree (next_attempt_at) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));

-- Name: idx_memory_webhook_deliveries_webhook; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_memory_webhook_deliveries_webhook ON public.memory_webhook_deliveries USING btree (webhook_id, created_at DESC);

-- Name: idx_memory_webhooks_owner; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_memory_webhooks_owner ON public.memory_webhooks USING btree (tenant_id, subject) WHERE (revoked_at IS NULL);

-- Name: idx_one_active_build_per_env; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_one_active_build_per_env ON public.builds USING btree (tenant_id, project_id, environment) WHERE (status = ANY (ARRAY['QUEUED'::text, 'RUNNING'::text]));

-- Name: idx_pending_deletion_grace; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_pending_deletion_grace ON public.tenants_pending_deletion USING btree (grace_until) WHERE (grace_until IS NOT NULL);

-- Name: idx_preview_build; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_preview_build ON public.preview_environments USING btree (build_id) WHERE (build_id IS NOT NULL);

-- Name: idx_preview_cell_status_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_preview_cell_status_created ON public.preview_environments USING btree (cell_id, status, created_at);

-- Name: idx_preview_expires; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_preview_expires ON public.preview_environments USING btree (expires_at) WHERE ((deleted_at IS NULL) AND (status <> ALL (ARRAY['deleted'::text, 'deleting'::text])));

-- Name: idx_preview_slot_project; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_preview_slot_project ON public.preview_slot USING btree (project_id);

-- Name: idx_preview_tenant_status; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_preview_tenant_status ON public.preview_environments USING btree (tenant_id, status) WHERE (deleted_at IS NULL);

-- Name: idx_project_repos_installation; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_project_repos_installation ON public.project_repositories USING btree (installation_id);

-- Name: idx_project_repos_repo; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_project_repos_repo ON public.project_repositories USING btree (repo_full_name);

-- Name: idx_project_repos_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_project_repos_tenant ON public.project_repositories USING btree (tenant_id, project_name);

-- Name: idx_projects_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_projects_tenant ON public.projects USING btree (tenant_id);

-- Name: idx_release_app_attestations_release; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_app_attestations_release ON public.release_app_attestations USING btree (release_id);

-- Name: idx_release_app_attestations_signed; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_app_attestations_signed ON public.release_app_attestations USING btree (tenant_id, signed_at) WHERE (signed_at IS NOT NULL);

-- Name: idx_release_app_attestations_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_release_app_attestations_tenant ON public.release_app_attestations USING btree (tenant_id, created_at DESC);

-- Name: idx_releases_active_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_releases_active_unique ON public.releases USING btree (tenant_id, project_id, environment) WHERE (status = 'ACTIVE'::text);

-- Name: idx_releases_build_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_build_id ON public.releases USING btree (build_id, created_at DESC);

-- Name: idx_releases_cell_status_created; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_cell_status_created ON public.releases USING btree (cell_id, status, created_at);

-- Name: idx_releases_env; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_env ON public.releases USING btree (tenant_id, project_id, environment, created_at DESC);

-- Name: idx_releases_pending; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_pending ON public.releases USING btree (created_at) WHERE (status = 'PENDING'::text);

-- Name: idx_releases_processing_since; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_processing_since ON public.releases USING btree (processing_since) WHERE (processing_by IS NOT NULL);

-- Name: idx_releases_status_active; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_releases_status_active ON public.releases USING btree (status) WHERE (status <> 'ARCHIVED'::text);

-- Name: idx_secrets_lookup; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_secrets_lookup ON public.secrets USING btree (tenant_id, project_id, environment, app);

-- Name: idx_secrets_tenant_project_env; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_secrets_tenant_project_env ON public.secrets USING btree (tenant_id, project_id, environment);

-- Name: idx_sse_events_build; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_sse_events_build ON public.sse_events USING btree (build_id, id);

-- Name: idx_subscriptions_account_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_subscriptions_account_id ON public.subscriptions USING btree (account_id);

-- Name: idx_subscriptions_stripe_customer; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_subscriptions_stripe_customer ON public.subscriptions USING btree (stripe_customer_id);

-- Name: idx_subscriptions_tenant; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_subscriptions_tenant ON public.subscriptions USING btree (tenant_id);

-- Name: idx_tenant_members_user; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_tenant_members_user ON public.tenant_members USING btree (user_id);

-- Name: idx_tenants_account_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_tenants_account_id ON public.tenants USING btree (account_id);

-- Name: idx_tenants_cell_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_tenants_cell_id ON public.tenants USING btree (cell_id);

-- Name: idx_tenants_slug; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_tenants_slug ON public.tenants USING btree (slug);

-- Name: idx_tombstones_anonymization; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_tombstones_anonymization ON public.tenant_tombstones USING btree (deleted_at) WHERE (anonymized_at IS NULL);

-- Name: idx_usage_tenant_metric_period; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_usage_tenant_metric_period ON public.usage_records USING btree (tenant_id, metric, period_start);

-- Name: idx_user_identity_map_user_id; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_user_identity_map_user_id ON public.user_identity_map USING btree (user_id);

-- Name: idx_users_active_tenant_id; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_users_active_tenant_id ON public.users USING btree (active_tenant_id) WHERE (active_tenant_id IS NOT NULL);

-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_users_email ON public.users USING btree (email);

-- Name: idx_users_email_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_users_email_unique ON public.users USING btree (email);

-- Name: idx_users_github_id_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_users_github_id_unique ON public.users USING btree (github_id) WHERE (github_id IS NOT NULL);

-- Name: idx_users_username_unique; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX idx_users_username_unique ON public.users USING btree (username);

-- Name: idx_webhook_deliveries_received; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_webhook_deliveries_received ON public.github_webhook_deliveries USING btree (received_at);

-- Name: idx_webhook_events_type; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_webhook_events_type ON public.webhook_events USING btree (event_type);

-- Name: idx_work_queue_dequeue; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_work_queue_dequeue ON public.work_queue USING btree (queue, status, created_at) WHERE (status = 'pending'::text);

-- Name: idx_work_queue_stale; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_work_queue_stale ON public.work_queue USING btree (status, claimed_at) WHERE (status = 'processing'::text);

-- Name: idx_workspace_webhook_deliveries_due; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workspace_webhook_deliveries_due ON public.workspace_webhook_deliveries USING btree (next_attempt_at) WHERE (status = ANY (ARRAY['pending'::text, 'failed'::text]));

-- Name: idx_workspace_webhook_deliveries_lease; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workspace_webhook_deliveries_lease ON public.workspace_webhook_deliveries USING btree (claimed_at) WHERE (status = 'delivering'::text);

-- Name: idx_workspace_webhook_deliveries_webhook; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workspace_webhook_deliveries_webhook ON public.workspace_webhook_deliveries USING btree (webhook_id, created_at DESC);

-- Name: idx_workspace_webhooks_owner; Type: INDEX; Schema: public; Owner: -

CREATE INDEX idx_workspace_webhooks_owner ON public.workspace_webhooks USING btree (tenant_id, subject) WHERE (revoked_at IS NULL);

-- Name: storage_objects_tenant_prefix; Type: INDEX; Schema: public; Owner: -

CREATE INDEX storage_objects_tenant_prefix ON public.storage_objects USING btree (tenant_id, name text_pattern_ops);

-- Name: storage_objects_tenant_usage; Type: INDEX; Schema: public; Owner: -

CREATE INDEX storage_objects_tenant_usage ON public.storage_objects USING btree (tenant_id) INCLUDE (size_bytes);

-- Name: uniq_preview_active_per_pr; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX uniq_preview_active_per_pr ON public.preview_environments USING btree (tenant_id, project_id, pr_number) WHERE (deleted_at IS NULL);

-- Name: uq_lens_fired_alerts_transition; Type: INDEX; Schema: public; Owner: -

CREATE UNIQUE INDEX uq_lens_fired_alerts_transition ON public.lens_fired_alerts USING btree (rule_id, transition_id);

-- Name: releases releases_attestation_url_audit; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER releases_attestation_url_audit AFTER INSERT ON public.releases FOR EACH ROW EXECUTE FUNCTION public.audit_attestation_url_set();

-- Name: releases releases_attestation_url_immutable; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER releases_attestation_url_immutable BEFORE UPDATE ON public.releases FOR EACH ROW EXECUTE FUNCTION public.reject_attestation_url_mutation();

-- Name: audit_log trg_audit_log_immutable; Type: TRIGGER; Schema: public; Owner: -

CREATE CONSTRAINT TRIGGER trg_audit_log_immutable AFTER DELETE OR UPDATE ON public.audit_log DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_log_prevent_mutation();

-- Name: builds trg_builds_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_builds_populate_cell_id BEFORE INSERT ON public.builds FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();

-- Name: cells trg_cells_set_updated_at; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_cells_set_updated_at BEFORE UPDATE ON public.cells FOR EACH ROW EXECUTE FUNCTION public.set_cells_updated_at();

-- Name: preview_environments trg_preview_environments_audit; Type: TRIGGER; Schema: public; Owner: -

CREATE CONSTRAINT TRIGGER trg_preview_environments_audit AFTER INSERT OR DELETE OR UPDATE ON public.preview_environments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();

-- Name: preview_environments trg_preview_environments_updated_at; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_preview_environments_updated_at BEFORE UPDATE ON public.preview_environments FOR EACH ROW EXECUTE FUNCTION public.preview_environments_set_updated_at();

-- Name: preview_environments trg_preview_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_preview_populate_cell_id BEFORE INSERT ON public.preview_environments FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();

-- Name: release_app_attestations trg_release_app_attestations_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_release_app_attestations_populate_cell_id BEFORE INSERT ON public.release_app_attestations FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();

-- Name: release_app_attestations trg_release_app_attestations_updated_at; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_release_app_attestations_updated_at BEFORE UPDATE ON public.release_app_attestations FOR EACH ROW EXECUTE FUNCTION public.release_app_attestations_set_updated_at();

-- Name: releases trg_releases_populate_cell_id; Type: TRIGGER; Schema: public; Owner: -

CREATE TRIGGER trg_releases_populate_cell_id BEFORE INSERT ON public.releases FOR EACH ROW EXECUTE FUNCTION public.populate_cell_id_from_tenant();

-- Name: secrets trg_secrets_audit; Type: TRIGGER; Schema: public; Owner: -

CREATE CONSTRAINT TRIGGER trg_secrets_audit AFTER INSERT OR DELETE OR UPDATE ON public.secrets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();

-- Name: tenant_members trg_tenant_members_audit; Type: TRIGGER; Schema: public; Owner: -

CREATE CONSTRAINT TRIGGER trg_tenant_members_audit AFTER INSERT OR DELETE OR UPDATE ON public.tenant_members DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION public.audit_sensitive_mutation();

-- Name: accounts accounts_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE RESTRICT;

-- Name: build_apps build_apps_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.build_apps
    ADD CONSTRAINT build_apps_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id) ON DELETE CASCADE;

-- Name: cost_counters cost_counters_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.cost_counters
    ADD CONSTRAINT cost_counters_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: custom_domains custom_domains_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.custom_domains
    ADD CONSTRAINT custom_domains_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;

-- Name: deploy_events deploy_events_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.deploy_events
    ADD CONSTRAINT deploy_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: environment_targets environment_targets_active_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_active_release_id_fkey FOREIGN KEY (active_release_id) REFERENCES public.releases(id) ON DELETE RESTRICT;

-- Name: environment_targets environment_targets_desired_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.environment_targets
    ADD CONSTRAINT environment_targets_desired_release_id_fkey FOREIGN KEY (desired_release_id) REFERENCES public.releases(id) ON DELETE RESTRICT;

-- Name: github_installations github_installations_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.github_installations
    ADD CONSTRAINT github_installations_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE SET NULL;

-- Name: lens_alert_rules lens_alert_rules_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_alert_rules
    ADD CONSTRAINT lens_alert_rules_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: lens_alert_rules lens_alert_rules_webhook_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_alert_rules
    ADD CONSTRAINT lens_alert_rules_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES public.workspace_webhooks(id) ON DELETE CASCADE;

-- Name: lens_dashboards lens_dashboards_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_dashboards
    ADD CONSTRAINT lens_dashboards_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: lens_fired_alerts lens_fired_alerts_rule_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_fired_alerts
    ADD CONSTRAINT lens_fired_alerts_rule_id_fkey FOREIGN KEY (rule_id) REFERENCES public.lens_alert_rules(id) ON DELETE CASCADE;

-- Name: lens_fired_alerts lens_fired_alerts_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_fired_alerts
    ADD CONSTRAINT lens_fired_alerts_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: lens_saved_views lens_saved_views_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.lens_saved_views
    ADD CONSTRAINT lens_saved_views_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: loopzero_api_keys loopzero_api_keys_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

-- Name: loopzero_api_keys loopzero_api_keys_rotated_from_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_rotated_from_fkey FOREIGN KEY (rotated_from) REFERENCES public.loopzero_api_keys(id) ON DELETE SET NULL;

-- Name: loopzero_api_keys loopzero_api_keys_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.loopzero_api_keys
    ADD CONSTRAINT loopzero_api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: memory_exports memory_exports_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_exports
    ADD CONSTRAINT memory_exports_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: memory_webhook_deliveries memory_webhook_deliveries_webhook_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_webhook_deliveries
    ADD CONSTRAINT memory_webhook_deliveries_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES public.memory_webhooks(id) ON DELETE CASCADE;

-- Name: memory_webhooks memory_webhooks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.memory_webhooks
    ADD CONSTRAINT memory_webhooks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: preview_environments preview_environments_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id) ON DELETE SET NULL;

-- Name: preview_environments preview_environments_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(id) ON DELETE SET NULL;

-- Name: preview_environments preview_environments_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_environments
    ADD CONSTRAINT preview_environments_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: preview_slot preview_slot_active_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.preview_slot
    ADD CONSTRAINT preview_slot_active_release_id_fkey FOREIGN KEY (active_release_id) REFERENCES public.releases(id) ON DELETE SET NULL;

-- Name: project_repositories project_repositories_installation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_installation_id_fkey FOREIGN KEY (installation_id) REFERENCES public.github_installations(id) ON DELETE RESTRICT;

-- Name: project_repositories project_repositories_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.project_repositories
    ADD CONSTRAINT project_repositories_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: projects projects_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE RESTRICT;

-- Name: projects projects_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;

-- Name: release_app_attestations release_app_attestations_release_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.release_app_attestations
    ADD CONSTRAINT release_app_attestations_release_id_fkey FOREIGN KEY (release_id) REFERENCES public.releases(id) ON DELETE CASCADE;

-- Name: releases releases_build_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.releases
    ADD CONSTRAINT releases_build_id_fkey FOREIGN KEY (build_id) REFERENCES public.builds(id) ON DELETE RESTRICT;

-- Name: subscriptions subscriptions_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

-- Name: subscriptions subscriptions_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;

-- Name: tenant_members tenant_members_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: tenant_members tenant_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenant_members
    ADD CONSTRAINT tenant_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

-- Name: tenants tenants_account_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE RESTRICT;

-- Name: tenants tenants_owner_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_owner_id_fkey FOREIGN KEY (owner_id) REFERENCES public.users(id) ON DELETE RESTRICT;

-- Name: tenants_pending_deletion tenants_pending_deletion_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.tenants_pending_deletion
    ADD CONSTRAINT tenants_pending_deletion_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: usage_records usage_records_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE RESTRICT;

-- Name: user_identity_map user_identity_map_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.user_identity_map
    ADD CONSTRAINT user_identity_map_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

-- Name: users users_active_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_active_tenant_id_fkey FOREIGN KEY (active_tenant_id) REFERENCES public.tenants(id) ON DELETE SET NULL;

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_webhook_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workspace_webhook_deliveries
    ADD CONSTRAINT workspace_webhook_deliveries_webhook_id_fkey FOREIGN KEY (webhook_id) REFERENCES public.workspace_webhooks(id) ON DELETE CASCADE;

-- Name: workspace_webhooks workspace_webhooks_tenant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -

ALTER TABLE ONLY public.workspace_webhooks
    ADD CONSTRAINT workspace_webhooks_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- Name: app_scaling_config; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.app_scaling_config ENABLE ROW LEVEL SECURITY;

-- Name: app_scaling_config app_scaling_config_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY app_scaling_config_cloud_cross_tenant ON public.app_scaling_config TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: app_scaling_config app_scaling_config_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY app_scaling_config_tenant_isolation ON public.app_scaling_config USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: app_scaling_config app_scaling_config_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY app_scaling_config_worker_cross_tenant ON public.app_scaling_config TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: audit_log; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.audit_log ENABLE ROW LEVEL SECURITY;

-- Name: audit_log audit_log_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY audit_log_cloud_cross_tenant ON public.audit_log TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: audit_log audit_log_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY audit_log_tenant_isolation ON public.audit_log USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: audit_log audit_log_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY audit_log_worker_cross_tenant ON public.audit_log TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: builds; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.builds ENABLE ROW LEVEL SECURITY;

-- Name: builds builds_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY builds_cloud_cross_tenant ON public.builds TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: builds builds_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY builds_tenant_isolation ON public.builds USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: builds builds_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY builds_worker_cross_tenant ON public.builds TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: cost_counters; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.cost_counters ENABLE ROW LEVEL SECURITY;

-- Name: cost_counters cost_counters_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY cost_counters_cloud_cross_tenant ON public.cost_counters TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: cost_counters cost_counters_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY cost_counters_tenant_isolation ON public.cost_counters USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: cost_counters cost_counters_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY cost_counters_worker_cross_tenant ON public.cost_counters TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: custom_domains; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.custom_domains ENABLE ROW LEVEL SECURITY;

-- Name: custom_domains custom_domains_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY custom_domains_cloud_cross_tenant ON public.custom_domains TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: custom_domains custom_domains_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY custom_domains_tenant_isolation ON public.custom_domains USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: custom_domains custom_domains_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY custom_domains_worker_cross_tenant ON public.custom_domains TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: deploy_events; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.deploy_events ENABLE ROW LEVEL SECURITY;

-- Name: deploy_events deploy_events_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY deploy_events_cloud_cross_tenant ON public.deploy_events TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: deploy_events deploy_events_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY deploy_events_tenant_isolation ON public.deploy_events USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: deploy_events deploy_events_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY deploy_events_worker_cross_tenant ON public.deploy_events TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: environment_targets; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.environment_targets ENABLE ROW LEVEL SECURITY;

-- Name: environment_targets environment_targets_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY environment_targets_cloud_cross_tenant ON public.environment_targets TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: environment_targets environment_targets_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY environment_targets_tenant_isolation ON public.environment_targets USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: environment_targets environment_targets_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY environment_targets_worker_cross_tenant ON public.environment_targets TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: github_installations; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.github_installations ENABLE ROW LEVEL SECURITY;

-- Name: github_installations github_installations_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY github_installations_cloud_cross_tenant ON public.github_installations TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: github_installations github_installations_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY github_installations_tenant_isolation ON public.github_installations USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: github_installations github_installations_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY github_installations_worker_cross_tenant ON public.github_installations TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: idempotency_keys; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.idempotency_keys ENABLE ROW LEVEL SECURITY;

-- Name: idempotency_keys idempotency_keys_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY idempotency_keys_cloud_cross_tenant ON public.idempotency_keys TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: idempotency_keys idempotency_keys_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY idempotency_keys_tenant_isolation ON public.idempotency_keys USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: idempotency_keys idempotency_keys_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY idempotency_keys_worker_cross_tenant ON public.idempotency_keys TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: lens_alert_rules; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.lens_alert_rules ENABLE ROW LEVEL SECURITY;

-- Name: lens_alert_rules lens_alert_rules_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_alert_rules_cloud_cross_tenant ON public.lens_alert_rules TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: lens_alert_rules lens_alert_rules_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_alert_rules_tenant_isolation ON public.lens_alert_rules USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: lens_alert_rules lens_alert_rules_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_alert_rules_worker_cross_tenant ON public.lens_alert_rules TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: lens_saved_views; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.lens_saved_views ENABLE ROW LEVEL SECURITY;

-- Name: lens_saved_views lens_saved_views_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_saved_views_cloud_cross_tenant ON public.lens_saved_views TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: lens_saved_views lens_saved_views_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_saved_views_tenant_isolation ON public.lens_saved_views USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: lens_saved_views lens_saved_views_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY lens_saved_views_worker_cross_tenant ON public.lens_saved_views TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: loopzero_api_keys; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.loopzero_api_keys ENABLE ROW LEVEL SECURITY;

-- Name: loopzero_api_keys loopzero_api_keys_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY loopzero_api_keys_cloud_cross_tenant ON public.loopzero_api_keys TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: loopzero_api_keys loopzero_api_keys_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY loopzero_api_keys_tenant_isolation ON public.loopzero_api_keys USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: loopzero_api_keys loopzero_api_keys_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY loopzero_api_keys_worker_cross_tenant ON public.loopzero_api_keys TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: memory_exports; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.memory_exports ENABLE ROW LEVEL SECURITY;

-- Name: memory_exports memory_exports_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_exports_cloud_cross_tenant ON public.memory_exports TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: memory_exports memory_exports_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_exports_tenant_isolation ON public.memory_exports USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: memory_exports memory_exports_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_exports_worker_cross_tenant ON public.memory_exports TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: memory_webhooks; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.memory_webhooks ENABLE ROW LEVEL SECURITY;

-- Name: memory_webhooks memory_webhooks_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_webhooks_cloud_cross_tenant ON public.memory_webhooks TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: memory_webhooks memory_webhooks_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_webhooks_tenant_isolation ON public.memory_webhooks USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: memory_webhooks memory_webhooks_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY memory_webhooks_worker_cross_tenant ON public.memory_webhooks TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: metering_cursor; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.metering_cursor ENABLE ROW LEVEL SECURITY;

-- Name: metering_cursor metering_cursor_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY metering_cursor_cloud_cross_tenant ON public.metering_cursor TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: metering_cursor metering_cursor_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY metering_cursor_tenant_isolation ON public.metering_cursor USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: metering_cursor metering_cursor_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY metering_cursor_worker_cross_tenant ON public.metering_cursor TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: preview_environments; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.preview_environments ENABLE ROW LEVEL SECURITY;

-- Name: preview_environments preview_environments_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_environments_cloud_cross_tenant ON public.preview_environments TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: preview_environments preview_environments_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_environments_tenant_isolation ON public.preview_environments USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: preview_environments preview_environments_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_environments_worker_cross_tenant ON public.preview_environments TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: preview_slot; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.preview_slot ENABLE ROW LEVEL SECURITY;

-- Name: preview_slot preview_slot_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_slot_cloud_cross_tenant ON public.preview_slot TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: preview_slot preview_slot_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_slot_tenant_isolation ON public.preview_slot USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: preview_slot preview_slot_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY preview_slot_worker_cross_tenant ON public.preview_slot TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: project_repositories; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.project_repositories ENABLE ROW LEVEL SECURITY;

-- Name: project_repositories project_repositories_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY project_repositories_cloud_cross_tenant ON public.project_repositories TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: project_repositories project_repositories_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY project_repositories_tenant_isolation ON public.project_repositories USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: project_repositories project_repositories_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY project_repositories_worker_cross_tenant ON public.project_repositories TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: projects; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.projects ENABLE ROW LEVEL SECURITY;

-- Name: projects projects_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY projects_cloud_cross_tenant ON public.projects TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: projects projects_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY projects_tenant_isolation ON public.projects USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: projects projects_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY projects_worker_cross_tenant ON public.projects TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: release_app_attestations; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.release_app_attestations ENABLE ROW LEVEL SECURITY;

-- Name: release_app_attestations release_app_attestations_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY release_app_attestations_cloud_cross_tenant ON public.release_app_attestations TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: release_app_attestations release_app_attestations_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY release_app_attestations_tenant_isolation ON public.release_app_attestations USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: release_app_attestations release_app_attestations_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY release_app_attestations_worker_cross_tenant ON public.release_app_attestations TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: releases; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.releases ENABLE ROW LEVEL SECURITY;

-- Name: releases releases_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY releases_cloud_cross_tenant ON public.releases TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: releases releases_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY releases_tenant_isolation ON public.releases USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: releases releases_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY releases_worker_cross_tenant ON public.releases TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: secrets; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.secrets ENABLE ROW LEVEL SECURITY;

-- Name: secrets secrets_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY secrets_cloud_cross_tenant ON public.secrets TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: secrets secrets_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY secrets_tenant_isolation ON public.secrets USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: secrets secrets_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY secrets_worker_cross_tenant ON public.secrets TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: subscriptions; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.subscriptions ENABLE ROW LEVEL SECURITY;

-- Name: subscriptions subscriptions_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY subscriptions_cloud_cross_tenant ON public.subscriptions TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: subscriptions subscriptions_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY subscriptions_tenant_isolation ON public.subscriptions USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: subscriptions subscriptions_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY subscriptions_worker_cross_tenant ON public.subscriptions TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: tenant_members; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.tenant_members ENABLE ROW LEVEL SECURITY;

-- Name: tenant_members tenant_members_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_members_cloud_cross_tenant ON public.tenant_members TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: tenant_members tenant_members_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_members_tenant_isolation ON public.tenant_members USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: tenant_members tenant_members_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_members_worker_cross_tenant ON public.tenant_members TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: tenant_tombstones; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.tenant_tombstones ENABLE ROW LEVEL SECURITY;

-- Name: tenant_tombstones tenant_tombstones_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_tombstones_cloud_cross_tenant ON public.tenant_tombstones TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: tenant_tombstones tenant_tombstones_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_tombstones_tenant_isolation ON public.tenant_tombstones USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: tenant_tombstones tenant_tombstones_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenant_tombstones_worker_cross_tenant ON public.tenant_tombstones TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: tenants; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.tenants ENABLE ROW LEVEL SECURITY;

-- Name: tenants tenants_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_cloud_cross_tenant ON public.tenants TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: tenants_pending_deletion; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.tenants_pending_deletion ENABLE ROW LEVEL SECURITY;

-- Name: tenants_pending_deletion tenants_pending_deletion_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_pending_deletion_cloud_cross_tenant ON public.tenants_pending_deletion TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: tenants_pending_deletion tenants_pending_deletion_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_pending_deletion_tenant_isolation ON public.tenants_pending_deletion USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: tenants_pending_deletion tenants_pending_deletion_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_pending_deletion_worker_cross_tenant ON public.tenants_pending_deletion TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: tenants tenants_self_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_self_isolation ON public.tenants USING ((id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((id = current_setting('app.tenant_id'::text, true)));

-- Name: tenants tenants_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY tenants_worker_cross_tenant ON public.tenants TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: usage_records; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.usage_records ENABLE ROW LEVEL SECURITY;

-- Name: usage_records usage_records_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY usage_records_cloud_cross_tenant ON public.usage_records TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: usage_records usage_records_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY usage_records_tenant_isolation ON public.usage_records USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: usage_records usage_records_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY usage_records_worker_cross_tenant ON public.usage_records TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: workspace_webhook_deliveries; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.workspace_webhook_deliveries ENABLE ROW LEVEL SECURITY;

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhook_deliveries_cloud_cross_tenant ON public.workspace_webhook_deliveries TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhook_deliveries_tenant_isolation ON public.workspace_webhook_deliveries USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: workspace_webhook_deliveries workspace_webhook_deliveries_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhook_deliveries_worker_cross_tenant ON public.workspace_webhook_deliveries TO loopzero_worker USING (true) WITH CHECK (true);

-- Name: workspace_webhooks; Type: ROW SECURITY; Schema: public; Owner: -

ALTER TABLE public.workspace_webhooks ENABLE ROW LEVEL SECURITY;

-- Name: workspace_webhooks workspace_webhooks_cloud_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhooks_cloud_cross_tenant ON public.workspace_webhooks TO loopzero_cloud USING (true) WITH CHECK (true);

-- Name: workspace_webhooks workspace_webhooks_tenant_isolation; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhooks_tenant_isolation ON public.workspace_webhooks USING ((tenant_id = current_setting('app.tenant_id'::text, true))) WITH CHECK ((tenant_id = current_setting('app.tenant_id'::text, true)));

-- Name: workspace_webhooks workspace_webhooks_worker_cross_tenant; Type: POLICY; Schema: public; Owner: -

CREATE POLICY workspace_webhooks_worker_cross_tenant ON public.workspace_webhooks TO loopzero_worker USING (true) WITH CHECK (true);

-- PostgreSQL database dump complete

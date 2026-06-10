// TypeScript types mirroring Go API structs from `api/internal/routes/*.go`.
//
// Plan v1.0 T0.1 — single source of truth for API shapes. Each interface
// includes JSDoc pointing to the source Go file so drift is visible in code
// review (compare json:"tag" against TS field name).
//
// Conventions:
//   - Go `string`           → TS `string`
//   - Go `int`              → TS `number`
//   - Go `*int` / `*string` → TS `number | null` / `string | null`
//   - Go `omitempty`        → TS `field?: T` (optional)
//   - Go timestamps         → TS `string` (ISO 8601 RFC3339)
//   - Go `json.RawMessage`  → TS `unknown`
//
// When adding a new interface, also update lib/mocks/handlers/{domain}.ts to
// register a handler returning a fixture of this shape.

// ============================================================================
// Common error shape (used across all routes)
// ============================================================================

/** Canonical LoopZeroError envelope.
 * @see api/internal/routes/audit.go:jsonError + pkg/errors/http */
export interface LoopZeroError {
  code: string;
  message: string;
}

// ============================================================================
// Auth / Whoami
// ============================================================================

/** @see api/internal/routes/auth.go:Whoami response */
export interface Whoami {
  user_id: string;
  email: string;
  name?: string;
  team_id: string;
  team_slug: string;
  teams?: WhoamiTeam[];
}

export interface WhoamiTeam {
  id: string;
  slug: string;
  role: string;
}

export interface TeamSwitchInput {
  team_id: string;
}

// ============================================================================
// Projects (api/internal/routes/projects.go)
// ============================================================================

/** Single app within a project (server/worker/frontend).
 * @see api/internal/routes/projects.go:AppInput */
export interface App {
  name: string;
  type?: string;             // "server" | "worker" | "frontend"
  port?: number;
  path?: string;
  framework?: string;        // "express" | "go" | "fastapi" | etc.
  build?: string;            // "dockerfile" | "buildpack" | "custom"
  rendering?: string;        // "static" | "server" (Sprint 10)
  secrets?: string[];
  replicas?: number;
  status?: string;           // populated by status aggregator
}

export interface Project {
  name: string;
  apps?: App[];
  last_deploy?: string | null;     // ISO timestamp; null if never deployed
  last_deploy_status?: ProjectStatus;
  environment?: string;
  created_at?: string;
}

export type ProjectStatus = 'live' | 'building' | 'failed' | 'idle' | 'queued';

export interface ProjectListResponse {
  projects: Project[];
  total?: number;
}

export interface ProjectCreateInput {
  name: string;
  apps?: App[];
  environment?: string;
}

// ============================================================================
// Builds (api/internal/routes/builds.go + build_list.go)
// ============================================================================

/** @see api/internal/routes/builds.go:BuildResponse */
export interface Build {
  build_id: string;
  project_id: string;
  environment: string;
  status: BuildStatus;
  commit_sha: string;
  created_by: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
  duration_seconds?: number | null;
  error?: string;
  apps?: BuildApp[];
  manifests?: string;
}

export type BuildStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled'
  | 'cancelling';

/** @see api/internal/routes/builds.go:BuildAppInfo / BuildCallbackApp */
export interface BuildApp {
  name: string;
  image_digest: string;
  url?: string;
  type?: string;
  duration_ms?: number;
}

export interface BuildListResponse {
  builds: Build[];
  total: number;
}

export interface BuildCreateInput {
  project_id: string;
  environment: string;
  commit_sha: string;
}

export interface BuildCreateResponse {
  build_id: string;
  stream_url: string;
}

// ============================================================================
// Build artifacts (SBOM + Attestation)
// ============================================================================

/** CycloneDX SBOM JSON (subset).
 * @see api/internal/routes/build_sbom.go */
export interface BuildSBOM {
  bomFormat: 'CycloneDX';
  specVersion: string;
  serialNumber?: string;
  components: SBOMComponent[];
}

export interface SBOMComponent {
  type: string;       // "library" | "application" | "framework"
  'bom-ref'?: string;
  name: string;
  version: string;
  purl?: string;
  licenses?: { license: { id: string } }[];
}

/** Cosign attestation envelope.
 * @see api/internal/routes/build_attestation.go */
export interface BuildAttestation {
  payloadType: string;
  payload: string;        // base64-encoded predicate
  signatures: AttestationSignature[];
}

export interface AttestationSignature {
  keyid: string;
  sig: string;
}

// ============================================================================
// Logs (api/internal/routes/logs.go + logs_stream.go)
// ============================================================================

/** Single log line. SSE stream emits one of these per `message` event. */
export interface LogLine {
  ts: string;             // ISO timestamp
  level: LogLevel;
  msg: string;
  source?: string;        // "stdout" | "stderr" | pod name
  correlation_id?: string;
}

export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';

/** SSE event wrapper. The data field is a serialized LogLine. */
export interface LogStreamEvent {
  type: 'message' | 'heartbeat' | 'error' | 'close';
  data?: string;
  id?: string;
}

// ============================================================================
// Environments (api/internal/routes/environments.go)
// ============================================================================

export interface Environment {
  name: string;
  status?: string;
  projects?: number;
  protected?: boolean;
  domain?: string;
  created_at?: string;
}

export interface EnvironmentListResponse {
  environments: Environment[];
  total?: number;
}

export interface EnvironmentCreateInput {
  name: string;
  protected?: boolean;
}

// ============================================================================
// Domains (api/internal/routes/domains.go + domains_create.go + domains_dns.go)
// ============================================================================

export interface Domain {
  id: string;
  domain: string;
  project: string;
  app: string;
  environment: string;
  status: DomainStatus;
  tls?: DomainTLS;
  verification?: DomainVerification;
  cname_target: string;
  created_at: string;
  verified_at?: string;
}

export type DomainStatus =
  | 'pending_verification'
  | 'verified'
  | 'provisioning'
  | 'active'
  | 'failed'
  | 'removing';

export interface DomainTLS {
  status: 'pending' | 'issued' | 'expired' | 'renewing';
  expires_at?: string;
}

export interface DomainVerification {
  txt_name: string;
  txt_value: string;
}

export interface DomainListResponse {
  domains: Domain[];
  total: number;
}

export interface DomainCreateInput {
  domain: string;
  project: string;
  app: string;
  environment: string;
}

// ============================================================================
// Secrets (api/internal/routes/secrets.go)
// ============================================================================

/** Secret metadata (value never returned in list responses). */
export interface Secret {
  name: string;
  environments: string[];     // scoped to N envs
  last_updated_by?: string;
  updated_at?: string;
  eso_sync_status?: 'synced' | 'pending' | 'failed';
}

export interface SecretListResponse {
  secrets: Secret[];
  total: number;
}

export interface SecretSetInput {
  name: string;
  value: string;
  environments?: string[];
}

// ============================================================================
// Teams (api/internal/routes/teams.go)
// ============================================================================

export interface TeamMember {
  username?: string;
  name?: string;
  email: string;
  role: TeamRole;
  joined?: string;
}

export type TeamRole = 'owner' | 'admin' | 'member';

export interface TeamListResponse {
  members: TeamMember[];
}

export interface TeamMemberInput {
  email: string;
  role: TeamRole;
}

export interface TeamMemberRoleUpdate {
  role: TeamRole;
}

// ============================================================================
// Previews (api/internal/routes/preview.go)
// ============================================================================

export interface Preview {
  id: string;
  project: string;
  branch: string;
  pr_title?: string;
  pr_number?: number;
  pr_url?: string;
  url: string;                 // deployed preview URL
  status: PreviewStatus;
  created_by: string;
  created_at: string;
  expires_at: string;          // TTL deadline
}

export type PreviewStatus = 'creating' | 'active' | 'expiring' | 'failed' | 'deleting' | 'deleted';

export interface PreviewListResponse {
  previews: Preview[];
  total: number;
}

export interface PreviewCreateInput {
  branch: string;
  commit_sha: string;
}

// ============================================================================
// Status / Scale / Rollback / Promote (project-scoped operations)
// ============================================================================

/** @see api/internal/routes/status.go */
export interface ProjectStatusDetail {
  project: string;
  environment: string;
  replicas_ready: number;
  replicas_desired: number;
  deploy_status: BuildStatus;
  build_status: BuildStatus;
  traffic_5xx_pct_1h: number;
  traffic_5xx_pct_24h: number;
  alerts_active: number;
  last_deploy_at?: string;
}

export interface ScaleInput {
  replicas?: number;
  min?: number;
  max?: number;
  target_cpu?: number;
}

export interface ScaleResponse {
  project: string;
  applied: ScaleInput;
}

export interface RollbackInput {
  build_id: string;
}

export interface RollbackResponse {
  release_id: string;
  status: BuildStatus;
}

export interface PromoteInput {
  from_environment: string;
  to_environment: string;
}

export interface PromoteResponse {
  release_id: string;
  status: BuildStatus;
}

// ============================================================================
// GitHub repo connect (api/internal/routes/repositories.go)
// ============================================================================

export interface ProjectConnection {
  connected: boolean;
  repo_url?: string;
  branch?: string;
  auto_deploy?: boolean;
  installation_id?: number;
}

export interface ProjectConnectInput {
  repo_url: string;
  branch: string;
  auto_deploy: boolean;
}

// ============================================================================
// Billing (api/internal/routes/billing.go)
// ============================================================================

export interface BillingResponse {
  plan: string;
  team: string;
  usage: Record<string, BillingUsageMetric>;
  period_start?: string;
  period_end?: string;
  plan_cap?: BillingPlanCap;
}

export interface BillingUsageMetric {
  used: number;
  limit: number | null;       // null = unlimited (Enterprise)
}

export interface BillingPlanCap {
  max_projects: number | null;
  max_environments: number | null;
  max_builds_per_month: number | null;
  max_replicas: number | null;
  max_seats: number | null;
}

export interface BillingPortalResponse {
  url: string;                // Stripe customer portal URL
}

// ============================================================================
// Audit log (api/internal/routes/audit.go)
// ============================================================================

export interface AuditEntry {
  id: string;
  ts: string;
  actor_email: string;
  actor_kind: AuditActorKind;
  action: string;             // e.g., "project.create", "team.member.remove"
  resource_type?: string;     // e.g., "project", "team", "secret"
  resource_id?: string;
  severity: AuditSeverity;
  metadata?: Record<string, unknown>;
}

export type AuditActorKind = 'user' | 'system' | 'admin' | 'api';
export type AuditSeverity = 'info' | 'warning' | 'critical';

export interface AuditListResponse {
  entries: AuditEntry[];
  total: number;
  next_page_token?: string;
}

// ============================================================================
// Tenant lifecycle (api/internal/routes/tenants.go)
// ============================================================================

export interface TenantDeleteResponse {
  tenant_id: string;
  status: 'PENDING_DELETION' | 'DELETING' | 'DELETED';
  grace_period_days?: number;
}

export interface TenantRestoreResponse {
  tenant_id: string;
  status: 'ACTIVE';
}

// ============================================================================
// Pagination (api/internal/routes/pagination.go — shared envelope)
// ============================================================================

export interface PaginationParams {
  page?: number;
  page_size?: number;
  cursor?: string;
}

export interface Paginated<T> {
  items: T[];
  total: number;
  page?: number;
  page_size?: number;
  next_page_token?: string;
}

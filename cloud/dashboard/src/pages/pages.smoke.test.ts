import { describe, it, expect } from 'vitest';

/**
 * Pages smoke test — verifies each route page module compiles and exports
 * the expected named function. Satisfies subset of T-H.1 acceptance:
 * "Zero console errors on any route" (proves modules load without throwing).
 *
 * Does NOT replace visual smoke (pixel diff vs mockup) — that still
 * requires sponsor browser execution per T-H.1 DoD.
 *
 * NOTE: tests assert export EXISTS as function; do NOT render via
 * @testing-library/react because that would require:
 *   - Installing @testing-library/react + jest-dom + setup file
 *   - Mocking react-router-dom (pages use useNavigate/useLocation)
 *   - Mocking @/lib/api + @/lib/auth fetch handlers
 *   - Setting up LoopZeroUIProvider wrapper for primitives that need theme context
 * Out of scope for v1.2; defer to future test infra upgrade plan.
 */

describe('pages smoke — modules compile + export named function', () => {
  it('exports OverviewPage', async () => {
    const m = await import('./overview');
    expect(typeof m.OverviewPage).toBe('function');
  });

  it('exports ProjectsPage', async () => {
    const m = await import('./projects');
    expect(typeof m.ProjectsPage).toBe('function');
  });

  it('exports EnvironmentsPage', async () => {
    const m = await import('./environments');
    expect(typeof m.EnvironmentsPage).toBe('function');
  });

  it('exports TeamPage', async () => {
    const m = await import('./team');
    expect(typeof m.TeamPage).toBe('function');
  });

  it('exports BillingPage', async () => {
    const m = await import('./billing');
    expect(typeof m.BillingPage).toBe('function');
  });

  it('exports SettingsPage', async () => {
    const m = await import('./settings');
    expect(typeof m.SettingsPage).toBe('function');
  });

  it('exports ProfilePage', async () => {
    const m = await import('./profile');
    expect(typeof m.ProfilePage).toBe('function');
  });

  it('exports DomainsPage', async () => {
    const m = await import('./domains');
    expect(typeof m.DomainsPage).toBe('function');
  });

  it('exports LoginPage', async () => {
    const m = await import('./login');
    expect(typeof m.LoginPage).toBe('function');
  });

  it('exports RegisterPage', async () => {
    const m = await import('./register');
    expect(typeof m.RegisterPage).toBe('function');
  });

  it('exports VerificationPage', async () => {
    const m = await import('./verification');
    expect(typeof m.VerificationPage).toBe('function');
  });

  it('exports RecoveryPage', async () => {
    const m = await import('./recovery');
    expect(typeof m.RecoveryPage).toBe('function');
  });

  it('exports DeviceSuccessPage', async () => {
    const m = await import('./device-success');
    expect(typeof m.DeviceSuccessPage).toBe('function');
  });

  it('exports ProjectDetailPage (plan v1.2 T0.3 — closes SG-06)', async () => {
    const m = await import('./project-detail');
    expect(typeof m.ProjectDetailPage).toBe('function');
  });

  it('exports NotFoundPage (plan v1.2 T0.3 — catch-all 404)', async () => {
    const m = await import('./not-found');
    expect(typeof m.NotFoundPage).toBe('function');
  });

  it('exports DeploymentsPage (plan v1.0 T1.1 — deploy lifecycle list)', async () => {
    const m = await import('./deployments');
    expect(typeof m.DeploymentsPage).toBe('function');
  });

  it('exports DeploymentDetailPage (plan v1.0 T1.2 — detail + BuildTimeline)', async () => {
    const m = await import('./deployment-detail');
    expect(typeof m.DeploymentDetailPage).toBe('function');
  });

  it('exports BuildLogsPage (plan v1.0 T1.3 — SSE log viewer)', async () => {
    const m = await import('./build-logs');
    expect(typeof m.BuildLogsPage).toBe('function');
  });

  it('exports BuildSBOMPage (plan v1.0 T1.4 — CycloneDX viewer)', async () => {
    const m = await import('./build-sbom');
    expect(typeof m.BuildSBOMPage).toBe('function');
  });

  it('exports BuildAttestationPage (plan v1.0 T1.5 — cosign envelope viewer)', async () => {
    const m = await import('./build-attestation');
    expect(typeof m.BuildAttestationPage).toBe('function');
  });

  it('exports RuntimeLogsPage (plan v1.0 T2.1 — runtime SSE viewer)', async () => {
    const m = await import('./runtime-logs');
    expect(typeof m.RuntimeLogsPage).toBe('function');
  });

  it('exports ProjectStatusPage (plan v1.0 T2.2 — 4-signal health card)', async () => {
    const m = await import('./project-status');
    expect(typeof m.ProjectStatusPage).toBe('function');
  });

  it('exports SecretsPage (plan v1.0 T3.1 — secrets CRUD)', async () => {
    const m = await import('./secrets');
    expect(typeof m.SecretsPage).toBe('function');
  });

  // PreviewsPage smoke test deleted per plan
  // 2026-06-08-dashboard-screens-review-fixes T1.1 + ADR D1 — page deleted,
  // route now redirects to /environments?project=:name (see app.test.tsx
  // "App routing — /previews → /environments redirect" suite for new
  // regression coverage).

  it('exports ProjectConnectPage (plan v1.0 T3.3 — GitHub connect)', async () => {
    const m = await import('./project-connect');
    expect(typeof m.ProjectConnectPage).toBe('function');
  });

  it('exports AuditPage (plan v1.0 T5.1 — audit log)', async () => {
    const m = await import('./audit');
    expect(typeof m.AuditPage).toBe('function');
  });

  it('exports ProjectScalePage (post-FAANG-refactor: scale dialog → page)', async () => {
    const m = await import('./project-scale');
    expect(typeof m.ProjectScalePage).toBe('function');
  });
});

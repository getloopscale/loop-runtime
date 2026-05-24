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
});

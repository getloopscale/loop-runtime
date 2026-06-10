/**
 * Playwright browser-runtime E2E for Journey J-05 Device Success.
 *
 * Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md
 * Task: T4.1 — DOM-driven cenários (auto-close + Back link).
 * Bash contract layer lives in T4.2.
 *
 * Cenário 1 (auto-close OR fallback):
 *   page.goto('/device-success') → wait up to 7s → expect either window.closed
 *   OR the fallback message visible. window.close() in Chromium headless can
 *   silently fail when the tab was not opened by script — fallback is the
 *   contract for that case.
 *
 * Cenário 2 (Back link click — EC-9):
 *   beforeEach addCookies('ory_kratos_session') so root route does NOT
 *   redirect to /login → click 'Back to dashboard' → expect URL transition
 *   away from /device-success (tolerant: matches root OR /login OR /projects).
 */

import { expect, test } from '@playwright/test';

test.describe('J-05 Device Success — dogfood Cenários (DOM-driven)', () => {
  test('Cenário 1: device-success auto-closes OR shows fallback within 7s', async ({ page, context }) => {
    // Stub the telemetry beacon so the test does not depend on backend reachability.
    await context.route('**/v1/telemetry/events', async (route) => {
      await route.fulfill({ status: 202, body: '' });
    });

    await page.goto('/device-success');
    await expect(page.getByText('CLI authorized')).toBeVisible();

    // Wait up to 7s for either: the page closes (Chromium honors window.close)
    // OR the fallback message renders (window.close was a no-op).
    const fallbackOrClosed = await Promise.race([
      page
        .waitForEvent('close', { timeout: 7000 })
        .then(() => 'closed' as const)
        .catch(() => null),
      page
        .getByTestId('device-success-fallback')
        .waitFor({ state: 'visible', timeout: 7000 })
        .then(() => 'fallback' as const)
        .catch(() => null),
    ]);

    expect(fallbackOrClosed).not.toBeNull();
  });

  test('Cenário 2 (EC-9): Back to dashboard link navigates away from /device-success', async ({ page, context }) => {
    await context.route('**/v1/telemetry/events', async (route) => {
      await route.fulfill({ status: 202, body: '' });
    });

    // EC-9 — a logged-out user might be redirected away from root; the session
    // cookie keeps the assertion focused on "Back link navigated us OUT of
    // /device-success" without depending on what / renders.
    await context.addCookies([
      {
        name: 'ory_kratos_session',
        value: 'mock-session',
        domain: 'localhost',
        path: '/',
      },
    ]);

    await page.goto('/device-success');
    await expect(page.getByTestId('device-success-back-link')).toBeVisible();
    await page.getByTestId('device-success-back-link').click();

    // Tolerant assertion (EC-9): success = URL no longer ends with
    // /device-success. Root may render the dashboard, /login, OR a project
    // route depending on auth + cookie state — none of those matter here.
    await expect(page).not.toHaveURL(/\/device-success/);
  });
});

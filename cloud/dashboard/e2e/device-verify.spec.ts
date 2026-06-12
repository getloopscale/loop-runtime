/**
 * Playwright browser-runtime E2E for Journey J-05 Device Verify page.
 *
 * Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j05-device-success-plan.md
 *       (iter 3 amendment — Hydra v2.6 requires operator-hosted device-verify UI;
 *        Hydra's built-in /oauth2/fallbacks/device is the "config missing" error page)
 *
 * Cenário 1 (canonical happy path):
 *   page.goto('/device-verify?device_challenge=X&user_code=Y') →
 *   user_code input pre-filled → click Authorize → wait for fetch to
 *   /v1/auth/device/accept → assert window.location.href followed redirect_to.
 *
 * Cenário 2 (missing device_challenge):
 *   page.goto('/device-verify?user_code=Y') (no challenge) → error visible.
 *
 * Cenário 3 (Hydra rejects user_code):
 *   page.goto('/device-verify?device_challenge=X&user_code=BAD') → submit →
 *   mocked 400 response with actionable message → error visible.
 */

import { expect, test } from '@playwright/test';

test.describe('J-05 Device Verify — dogfood Cenários (DOM-driven)', () => {
  test('Cenário 1: device-verify pre-fills user_code and follows redirect_to on submit', async ({ page, context }) => {
    let observedBody: { device_challenge: string; user_code: string } | null = null;

    await context.route('**/v1/auth/device/accept', async (route) => {
      const raw = route.request().postData();
      if (raw) {
        try {
          observedBody = JSON.parse(raw);
        } catch {
          observedBody = null;
        }
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          redirect_to: '/device-success?simulated=1',
        }),
      });
    });

    await page.goto('/device-verify?device_challenge=chal-abc&user_code=YC7V3TSE');

    const input = page.getByTestId('device-verify-user-code');
    await expect(input).toBeVisible();
    await expect(input).toHaveValue('YC7V3TSE');

    await page.getByTestId('device-verify-submit').click();

    await expect.poll(() => observedBody?.user_code, { timeout: 5000 }).toBe('YC7V3TSE');
    expect(observedBody?.device_challenge).toBe('chal-abc');

    // Submitting redirects to /device-success (mocked path).
    await expect(page).toHaveURL(/\/device-success/);
  });

  test('Cenário 2: missing device_challenge shows actionable error', async ({ page }) => {
    await page.goto('/device-verify?user_code=YC7V3TSE');
    await expect(page.getByTestId('device-verify-error')).toBeVisible();
    await expect(page.getByTestId('device-verify-error')).toContainText(/run.+loopzero login/i);
    // Submit button stays disabled — no fetch should fire.
    await expect(page.getByTestId('device-verify-submit')).toBeDisabled();
  });

  test('Cenário 3: bad user_code surfaces server message', async ({ page, context }) => {
    await context.route('**/v1/auth/device/accept', async (route) => {
      await route.fulfill({
        status: 400,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 'INVALID_REQUEST',
          message: 'Invalid user code.',
        }),
      });
    });

    await page.goto('/device-verify?device_challenge=chal-abc&user_code=BADCODE');
    await page.getByTestId('device-verify-submit').click();
    await expect(page.getByTestId('device-verify-error')).toContainText(/Invalid user code/i);
  });
});

/**
 * Playwright browser-runtime E2E for Journey J-04 Recovery (dogfood Cenários B + F).
 *
 * Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j04-recovery-plan.md
 * Task: T5.3 — DOM-driven cenários that require browser state mutation (B + F).
 * Cenários A, C, C2, D, E live in T5.1 bash (HTTP + kubectl).
 *
 * Cenário B (OIDC educational link autofocus):
 *   /recovery → click "Sign in with GitHub" educational link
 *   → /login?provider=github → GitHub button receives autofocus + ring
 *
 * Cenário F (EC-4 retry currying — input mutation between fail and retry):
 *   /recovery → submit email "a@x.com" → mock network fail
 *   → user corrects to "b@x.com" → click Retry
 *   → assert submitFlow body contains CURRENT input "b@x.com"
 *
 * Mocked Kratos endpoints via page.route() — hermetic, runs in CI without
 * dev-public DNS dependency.
 */

import { expect, test } from '@playwright/test';

test.describe('J-04 Recovery — dogfood Cenários B + F (DOM-driven)', () => {
  // ────────────────────────────────────────────────────────────────────────
  // Cenário B — OIDC educational link autofocus on /login?provider=
  // ────────────────────────────────────────────────────────────────────────
  test('Cenário B: educational link to /login?provider=github focuses GitHub button', async ({ page }) => {
    // Bootstrap a mocked recovery flow so /recovery renders without errors.
    await page.route('**/self-service/recovery/flows*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'rec-flow-1',
          ui: {
            action: '/self-service/recovery?flow=rec-flow-1',
            method: 'POST',
            nodes: [
              { attributes: { name: 'csrf_token', value: 'csrf-test' }, messages: [] },
              { attributes: { name: 'email', type: 'email' }, messages: [] },
            ],
            messages: [],
          },
        }),
      });
    });

    await page.goto('/recovery?flow=rec-flow-1');
    await expect(page.getByLabel('Email')).toBeVisible();

    // Click educational GitHub link
    await page.getByRole('link', { name: /sign in with github/i }).click();
    await expect(page).toHaveURL(/\/login\?provider=github/);

    // GitHub button on /login should be autofocused + have visual ring
    const ghButton = page.getByRole('button', { name: /continue with github/i });
    await expect(ghButton).toBeVisible();
    await expect(ghButton).toBeFocused();
    const cls = await ghButton.getAttribute('class');
    expect(cls).toContain('ring-2');
  });

  test('Cenário B (Google): educational link to /login?provider=google focuses Google button', async ({ page }) => {
    await page.route('**/self-service/recovery/flows*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'rec-flow-1',
          ui: {
            action: '/self-service/recovery?flow=rec-flow-1',
            method: 'POST',
            nodes: [
              { attributes: { name: 'csrf_token', value: 'csrf-test' }, messages: [] },
              { attributes: { name: 'email', type: 'email' }, messages: [] },
            ],
            messages: [],
          },
        }),
      });
    });

    await page.goto('/recovery?flow=rec-flow-1');
    await expect(page.getByLabel('Email')).toBeVisible();

    await page.getByRole('link', { name: /sign in with google/i }).click();
    await expect(page).toHaveURL(/\/login\?provider=google/);

    const googleButton = page.getByRole('button', { name: /continue with google/i });
    await expect(googleButton).toBeVisible();
    await expect(googleButton).toBeFocused();
    const cls = await googleButton.getAttribute('class');
    expect(cls).toContain('ring-2');
  });

  // ────────────────────────────────────────────────────────────────────────
  // Cenário F — EC-4 currying: Retry must read CURRENT input, not stale closure
  // ────────────────────────────────────────────────────────────────────────
  test('Cenário F (EC-4): Retry uses CURRENT email input, not snapshot from first submit', async ({ page }) => {
    // First mock the flow GET
    await page.route('**/self-service/recovery/flows*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'rec-flow-1',
          ui: {
            action: '/self-service/recovery?flow=rec-flow-1',
            method: 'POST',
            nodes: [
              { attributes: { name: 'csrf_token', value: 'csrf-test' }, messages: [] },
              { attributes: { name: 'email', type: 'email' }, messages: [] },
            ],
            messages: [],
          },
        }),
      });
    });

    // Track submitFlow request bodies
    let firstSubmitBody: Record<string, string> | null = null;
    let secondSubmitBody: Record<string, string> | null = null;
    let submitCount = 0;

    await page.route(/\/self-service\/recovery\?flow=rec-flow-1/, async (route) => {
      if (route.request().method() === 'POST') {
        // Playwright postDataJSON returns parsed JSON if content-type is JSON
        let body: Record<string, string> = {};
        try {
          body = (route.request().postDataJSON() as Record<string, string>) ?? {};
        } catch {
          // Fallback: parse raw post data
          const raw = route.request().postData();
          if (raw) {
            try {
              body = JSON.parse(raw);
            } catch {
              body = {};
            }
          }
        }
        submitCount += 1;
        if (submitCount === 1) {
          firstSubmitBody = body;
          // First attempt: simulate network failure
          await route.abort('failed');
        } else {
          secondSubmitBody = body;
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              id: 'rec-flow-1',
              ui: {
                action: '/self-service/recovery?flow=rec-flow-1',
                method: 'POST',
                nodes: [
                  { attributes: { name: 'csrf_token', value: 'csrf-test' }, messages: [] },
                  { attributes: { name: 'code', type: 'text' }, messages: [] },
                ],
                messages: [],
              },
            }),
          });
        }
      } else {
        await route.continue();
      }
    });

    await page.goto('/recovery?flow=rec-flow-1');
    await expect(page.getByLabel('Email')).toBeVisible();

    // First submission with "a@x.com" → fails
    await page.getByLabel('Email').fill('a@x.com');
    await page.getByRole('button', { name: /send recovery code/i }).click();
    await expect(page.getByRole('button', { name: /retry/i })).toBeVisible();
    expect(firstSubmitBody?.email).toBe('a@x.com');

    // User corrects email to "b@x.com" and clicks Retry
    await page.getByLabel('Email').fill('b@x.com');
    await page.getByRole('button', { name: /retry/i }).click();

    // Second submission should carry CURRENT input "b@x.com" (EC-4)
    await expect.poll(() => secondSubmitBody?.email, { timeout: 5000 }).toBe('b@x.com');
  });
});

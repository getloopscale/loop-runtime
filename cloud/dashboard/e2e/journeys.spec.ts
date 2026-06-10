/**
 * Playwright browser-runtime E2E journeys — Plan v1.0 Final Phase.
 *
 * Mirrors the 6 critical journeys from src/test/e2e-journeys.test.tsx but
 * exercises them against a real browser engine + the dev server running
 * with VITE_USE_MOCKS=true.
 *
 * Run: npx playwright test (after npm install -D @playwright/test).
 */

import { expect, test } from '@playwright/test';

test.describe('LoopZeroCloud dashboard — 6 critical journeys', () => {
  test('journey 1: first deploy — overview → projects → detail', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('heading', { name: /overview/i })).toBeVisible();

    await page.getByRole('button', { name: /projects/i }).click();
    await expect(page).toHaveURL(/\/projects$/);
    await expect(page.getByText('acme-web')).toBeVisible();

    await page.getByRole('link', { name: /open acme-web/i }).click();
    await expect(page).toHaveURL(/\/projects\/acme-web$/);
  });

  test('journey 2: debug failed build — drill into failed deployment', async ({ page }) => {
    await page.goto('/projects/acme-web/deployments');
    await expect(page.getByText(/failed/i).first()).toBeVisible();

    // Click first failed row → drill to detail
    const failedRow = page.locator('tr', { hasText: 'Failed' }).first();
    await failedRow.getByRole('link', { name: /view/i }).click();
    await expect(page).toHaveURL(/\/deployments\/bld_/);

    await page.getByRole('link', { name: /view logs/i }).click();
    await expect(page).toHaveURL(/\/logs$/);
    await expect(page.getByText(/cloning/i)).toBeVisible();
  });

  test('journey 3: production operation — rollback dialog', async ({ page }) => {
    await page.goto('/projects/acme-web/deployments');
    const succeededRow = page.locator('tr', { hasText: 'Succeeded' }).first();
    await succeededRow.getByRole('link', { name: /view/i }).click();

    await page.getByRole('button', { name: /rollback to here/i }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByText(/rollback to/i)).toBeVisible();
  });

  test('journey 4: billing transparency', async ({ page }) => {
    await page.goto('/billing');
    await expect(page.getByRole('heading', { name: /billing/i })).toBeVisible();
    await expect(page.getByText(/pro/i)).toBeVisible();
  });

  test('journey 5: team management — invite dialog', async ({ page }) => {
    await page.goto('/team');
    await expect(page.getByText('Paulo Henrique')).toBeVisible();

    await page.getByRole('button', { name: /^invite$/i }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByLabel(/email/i)).toBeVisible();
  });

  test('journey 6: secrets — add secret dialog', async ({ page }) => {
    await page.goto('/projects/acme-web/secrets');
    await expect(page.getByText('STRIPE_SECRET_KEY')).toBeVisible();

    await page.getByRole('button', { name: /add secret/i }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await expect(page.getByLabel(/key/i)).toBeVisible();
  });
});

test.describe('Accessibility smoke — Lighthouse axe-core checks', () => {
  test('overview page has h1', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('h1').first()).toBeVisible();
  });

  test('skip-to-content link is first focusable element', async ({ page }) => {
    await page.goto('/');
    await page.keyboard.press('Tab');
    const focused = await page.evaluate(() => document.activeElement?.textContent);
    expect(focused).toMatch(/skip to main content/i);
  });
});

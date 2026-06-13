/**
 * Playwright browser-runtime E2E for Journey J-09 Project Connect.
 *
 * Plan: .claude/knowledge-base/plans/2026-06-05-dashboard-journey-j09-project-connect-plan.md
 * Task: T6.1 — 4 cenários driving the install/picker/connect FSM through
 *              page.route() stubs (EC-5 strategy).
 *
 * CRITICAL: route() MUST be set BEFORE goto() — otherwise the first
 * request bypasses the stub (Playwright does NOT queue handlers).
 * Cenário comments call this out explicitly per EC-18 of the plan.
 */

import { expect, test } from '@playwright/test';

const PROJECT = 'demo';
const CONNECT_PATH = `/projects/${PROJECT}/connect`;

// JWT_COOKIE is a no-op session cookie that keeps useRequireAuth from
// punting us to /login. The dashboard does not validate this cookie in
// dev mode — its presence alone is enough to keep the page mounted.
const JWT_COOKIE = {
  name: 'loopzero_session',
  value: 'pw-mock-session',
  url: 'http://localhost:5174',
};

test.describe('J-09 Project Connect — install + picker + connect flow', () => {
  // Cenários share network stubs that mutate per-test (e.g. Cenário 2's
  // getCalls counter). Running serially against the same dev server
  // avoids cross-test stub races; the suite still completes in <20s.
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ context, page }) => {
    await context.addCookies([JWT_COOKIE]);

    // EC-18: ALL auth + install stubs MUST be registered before any goto().
    // useRequireAuth chains: GET /sessions/whoami -> POST /api/v1/auth/session/token.
    // Without these stubs the dashboard immediately redirects to /login and
    // the connect page never mounts. We also stub the install endpoint so
    // the popup that handleInstallClick opens does NOT 404 (which would
    // trigger the same-tab fallback and navigate the test page away).
    await page.route('**/sessions/whoami', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'pw-session',
          active: true,
          identity: {
            id: 'pw-id',
            traits: { email: 'pw@loopzero.test' },
          },
        }),
      }),
    );
    await page.route('**/v1/auth/session/token', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        // Field names match the BFF contract (api.ts L71): access_token + expires_in.
        body: JSON.stringify({
          access_token: 'pw-jwt',
          expires_in: 3600,
        }),
      }),
    );
    await context.route('**/v1/integrations/github/install*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<html><body>install-stub</body></html>',
      }),
    );
  });

  test('Cenário 1 (T1.2): clicking "Install GitHub App" opens a new tab', async ({
    page,
    context,
  }) => {
    // EC-18: route MUST be registered before goto.
    await page.route('**/engine/api/v1/projects/*/connect', (route) =>
      route.fulfill({ status: 200, body: JSON.stringify({ connected: false }) }),
    );
    // Stub the install endpoint so the popup that opens has something to
    // render (the dashboard would normally let it 302 to github.com).
    // We return a 200 with a minimal HTML body -- enough for Playwright
    // to receive the 'page' event without the network round trip.
    await context.route('**/v1/integrations/github/install*', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'text/html',
        body: '<html><body>install-stub</body></html>',
      }),
    );

    await page.goto(CONNECT_PATH);
    const installButton = page.getByRole('button', { name: /install github app/i });
    await expect(installButton).toBeVisible();

    const popupPromise = context.waitForEvent('page', { timeout: 5_000 });
    await installButton.click();
    const popup = await popupPromise;
    const requestedURL = popup.url();
    expect(requestedURL).toMatch(
      /\/v1\/integrations\/github\/install\?return_to=/,
    );
  });

  test('Cenário 2 (T1.2): polling flips state to has_installation when GET returns installation_id', async ({
    page,
  }) => {
    let getCalls = 0;
    // Sequenced fulfill: first 2 GETs say "not connected, no install",
    // the 3rd brings the installation_id. Polling fires every 2s; the
    // FSM should transition within ~6s.
    await page.route('**/engine/api/v1/projects/*/connect', (route) => {
      if (route.request().method() !== 'GET') {
        return route.fulfill({ status: 200, body: JSON.stringify({ ok: true }) });
      }
      getCalls++;
      const body =
        getCalls < 3
          ? { connected: false }
          : { connected: false, installation_id: 777 };
      return route.fulfill({ status: 200, body: JSON.stringify(body) });
    });
    await page.route(
      '**/engine/api/v1/integrations/github/installations/*/repositories',
      (route) =>
        route.fulfill({
          status: 200,
          body: JSON.stringify({ repositories: [] }), // empty list keeps the FSM in has_installation
        }),
    );

    await page.goto(CONNECT_PATH);
    await page.getByRole('button', { name: /install github app/i }).click();

    // FSM should reach has_installation -- either the picker label appears
    // OR the empty-state configure-link is rendered (repos returned []).
    // Polling fires every 2s; need at least 3 ticks (6s) for installation_id
    // to arrive + 1 tick for the engine repositories fetch to settle.
    await expect(
      page.getByRole('link', { name: /configure app access/i }),
    ).toBeVisible({ timeout: 15_000 });
  });

  test('Cenário 3 (T2.2): repo picker filter narrows the option list', async ({
    page,
  }) => {
    await page.route('**/engine/api/v1/projects/*/connect', (route) => {
      if (route.request().method() !== 'GET') {
        return route.fulfill({ status: 200, body: JSON.stringify({ ok: true }) });
      }
      return route.fulfill({
        status: 200,
        body: JSON.stringify({ connected: false, installation_id: 555 }),
      });
    });
    await page.route(
      '**/engine/api/v1/integrations/github/installations/*/repositories',
      (route) =>
        route.fulfill({
          status: 200,
          body: JSON.stringify({
            repositories: [
              { full_name: 'owner/api', default_branch: 'main', private: false },
              { full_name: 'owner/web', default_branch: 'develop', private: true },
              { full_name: 'owner/cli', default_branch: 'main', private: false },
            ],
          }),
        }),
    );

    await page.goto(CONNECT_PATH);
    const repoSelect = page.getByLabel(/^repository$/i);
    await expect(repoSelect).toBeVisible();
    // 3 repo options + "Pick a repository..." default => 4 children.
    await expect(repoSelect.locator('option')).toHaveCount(4);

    await page.getByLabel(/filter/i).fill('web');
    // Filtering reduces options to {default, owner/web}. Options inside a
    // collapsed <select> are reported as hidden by Playwright, so we
    // assert via the value list rather than visibility.
    await expect(repoSelect.locator('option')).toHaveCount(2);
    const optionValues = await repoSelect.locator('option').evaluateAll((els) =>
      (els as HTMLOptionElement[]).map((o) => o.value),
    );
    expect(optionValues).toContain('owner/web');
  });

  test('Cenário 4 (T3.1): connect POST body shape is {repo, installation_id, branch, auto_deploy}', async ({
    page,
  }) => {
    let postBody: Record<string, unknown> | null = null;
    await page.route('**/engine/api/v1/projects/*/connect', async (route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        return route.fulfill({
          status: 200,
          body: JSON.stringify({ connected: false, installation_id: 42 }),
        });
      }
      if (req.method() === 'POST') {
        try {
          postBody = JSON.parse(req.postData() ?? '{}');
        } catch {
          postBody = null;
        }
        return route.fulfill({
          status: 200,
          body: JSON.stringify({
            connected: true,
            repo: (postBody as Record<string, unknown> | null)?.repo ?? '',
            branch: 'main',
            auto_deploy: true,
            installation_id: 42,
          }),
        });
      }
      return route.fulfill({ status: 200, body: '{}' });
    });
    await page.route(
      '**/engine/api/v1/integrations/github/installations/*/repositories',
      (route) =>
        route.fulfill({
          status: 200,
          body: JSON.stringify({
            repositories: [
              { full_name: 'owner/api', default_branch: 'main', private: false },
            ],
          }),
        }),
    );

    await page.goto(CONNECT_PATH);
    await page.getByLabel(/^repository$/i).selectOption('owner/api');
    await page.getByRole('button', { name: /connect repository/i }).click();

    // Poll until the POST has been observed -- avoids depending on
    // rendered-state heuristics like "Connected" text which depends on
    // Card.Title rendering + casing.
    await expect.poll(() => postBody, { timeout: 5_000 }).not.toBeNull();
    expect(postBody).toMatchObject({
      repo: 'owner/api',
      installation_id: 42,
      branch: 'main',
      auto_deploy: true,
    });
    // Critical: the legacy field MUST NOT be sent.
    expect(postBody).not.toHaveProperty('repo_url');
  });
});

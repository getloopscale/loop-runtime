// Playwright config — Plan v1.0 Final Phase Dogfood QA infrastructure.
//
// Skeleton ready for runtime. To use:
//   1. npm install -D @playwright/test
//   2. npx playwright install chromium firefox webkit
//   3. npm run test:e2e (or `npx playwright test`)
//
// The 6 critical journeys defined in src/test/e2e-journeys.test.tsx already
// validate the same flows against jsdom + mocks (executable now in CI). The
// Playwright suite below is the BROWSER-RUNTIME variant — adds real
// rendering, real CSS layout, screenshot diffing.
//
// Visual regression vendor decision is OPEN (Chromatic / Percy / self-host
// BackstopJS). Until decided, screenshot tests below use Playwright's native
// `toHaveScreenshot()` with file-system snapshots committed to repo.

import { defineConfig, devices } from '@playwright/test';

const DEV_URL = process.env.PW_DEV_URL ?? 'http://localhost:5174';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI
    ? [['github'], ['html', { open: 'never' }]]
    : 'list',
  use: {
    baseURL: DEV_URL,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
    { name: 'mobile-chrome', use: { ...devices['Pixel 7'] } },
  ],
  webServer: process.env.CI
    ? {
        command: 'npm run dev -- --port 5174',
        url: DEV_URL,
        timeout: 60_000,
        env: { VITE_USE_MOCKS: 'true' },
        reuseExistingServer: false,
      }
    : undefined,
});

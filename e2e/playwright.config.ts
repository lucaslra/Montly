import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests',
  // Tests share a single server — run serially to avoid state collisions
  fullyParallel: false,
  workers: 1,
  // Fail fast on CI; allow retries for flakes
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { outputFolder: 'playwright-report', open: 'never' }]],
  use: {
    baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
    trace: 'on-first-retry',
    // --host-resolver-rules resolves app.localtest.me (used as BASE_URL) to the "app"
    // container: Chromium hardcodes HSTS for the real .app gTLD, so the bare "app"
    // hostname is forced to HTTPS and fails with ERR_SSL_PROTOCOL_ERROR otherwise.
    launchOptions: {
      args: [
        '--no-sandbox',
        '--disable-setuid-sandbox',
        '--host-resolver-rules=MAP app.localtest.me app',
      ],
    },
  },
  globalSetup: require.resolve('./global-setup'),
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        // Reuse the admin session created in global-setup for all specs
        // (auth.spec.ts overrides this with an empty state to test login flows)
        storageState: 'playwright/.auth/admin.json',
      },
    },
  ],
})

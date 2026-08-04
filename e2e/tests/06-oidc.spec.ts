import { test, expect } from '@playwright/test'

// OIDC tests run without a stored session so they exercise the SSO flow from a
// logged-out state. The E2E stack wires the app to a mock OpenID Connect
// provider (see docker-compose.e2e.yml + e2e/mock-oidc/server.mjs).
test.use({ storageState: { cookies: [], origins: [] } })

test.describe('OIDC / SSO', () => {
  test('login screen offers the SSO button alongside the password form', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.login-card')).toBeVisible()
    // SSO button present (label comes from OIDC_PROVIDER_NAME).
    await expect(page.getByRole('link', { name: 'Sign in with Mock SSO' })).toBeVisible()
    // Password form still available (hybrid mode).
    await expect(page.locator('#login-username')).toBeVisible()
  })

  test('completes the full SSO authorization-code flow and signs in', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Sign in with Mock SSO' }).click()

    // The browser is redirected app → IdP → callback → app, landing authenticated.
    await expect(page.locator('.app-header')).toBeVisible({ timeout: 20000 })
    // Provisioned/linked as the mock identity's username (preferred_username claim).
    await expect(page.locator('button[aria-label="Sign out (e2e-sso)"]')).toBeVisible()
  })

  test('signed-in SSO user has an active session across reloads', async ({ page }) => {
    await page.goto('/')
    await page.getByRole('link', { name: 'Sign in with Mock SSO' }).click()
    await expect(page.locator('.app-header')).toBeVisible({ timeout: 20000 })

    await page.reload()
    // Session cookie persists → still authenticated after a reload.
    await expect(page.locator('.app-header')).toBeVisible()
    await expect(page.locator('.login-card')).toHaveCount(0)
  })

  test('logging out of an SSO session returns to a login screen that still offers SSO', async ({ page }) => {
    // Regression: after an SSO login the initial page load is authenticated, so the
    // sign-in methods must still be loaded — otherwise logout showed a password-only
    // (or empty) login screen with no SSO button.
    await page.goto('/')
    await page.getByRole('link', { name: 'Sign in with Mock SSO' }).click()
    await expect(page.locator('.app-header')).toBeVisible({ timeout: 20000 })

    await page.click('button.logout-btn')
    await expect(page.locator('.login-card')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Sign in with Mock SSO' })).toBeVisible()
  })

  test('callback with a missing state shows a friendly error on the login page', async ({ page }) => {
    // Hitting the callback without the signed state cookie must not sign the user in.
    await page.goto('/api/auth/oidc/callback?state=forged&code=abc')
    await expect(page.locator('.login-card')).toBeVisible()
    await expect(page.locator('[role="alert"]')).toContainText(/session expired/i)
  })
})

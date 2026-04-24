import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.ADMIN_USER ?? 'admin'
const ADMIN_PASS = process.env.ADMIN_PASS ?? 'adminpass123'

// Auth tests start without a session so they can exercise the login/logout flow
test.use({ storageState: { cookies: [], origins: [] } })

test.describe('Authentication', () => {
  test('shows login screen when unauthenticated', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.login-card')).toBeVisible()
    await expect(page.locator('h1')).toHaveText('Montly')
  })

  test('logs in with valid credentials', async ({ page }) => {
    await page.goto('/')
    await page.fill('#login-username', ADMIN_USER)
    await page.fill('#login-password', ADMIN_PASS)
    await page.click('button[type="submit"]')

    await expect(page.locator('.app-header')).toBeVisible()
    await expect(page.locator('button.app-title-btn')).toHaveText('Montly')
  })

  test('shows error on wrong password', async ({ page }) => {
    await page.goto('/')
    await page.fill('#login-username', ADMIN_USER)
    await page.fill('#login-password', 'not-the-right-password')
    await page.click('button[type="submit"]')

    await expect(page.locator('[role="alert"]')).toBeVisible()
    await expect(page.locator('.login-card')).toBeVisible()
  })

  test('shows error on unknown username', async ({ page }) => {
    await page.goto('/')
    await page.fill('#login-username', 'doesnotexist')
    await page.fill('#login-password', 'somepassword')
    await page.click('button[type="submit"]')

    await expect(page.locator('[role="alert"]')).toBeVisible()
  })

  test('logs out and returns to login screen', async ({ page }) => {
    // Log in first
    await page.goto('/')
    await page.fill('#login-username', ADMIN_USER)
    await page.fill('#login-password', ADMIN_PASS)
    await page.click('button[type="submit"]')
    await expect(page.locator('.app-header')).toBeVisible()

    // Log out
    await page.click('button.logout-btn')
    await expect(page.locator('.login-card')).toBeVisible()
  })

  test('redirects unauthenticated API calls back to login', async ({ page }) => {
    // Navigate to a protected view path without a session
    await page.goto('/settings')
    await expect(page.locator('.login-card')).toBeVisible()
  })
})

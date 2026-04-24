import { test, expect } from '@playwright/test'

test.describe.serial('Settings', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/settings')
    await expect(page.locator('h2', { hasText: 'Settings' })).toBeVisible()
  })

  // ── Preferences form ──────────────────────────────────────────────────────

  test('settings page renders all form fields', async ({ page }) => {
    await expect(page.locator('#s-currency')).toBeVisible()
    await expect(page.locator('#s-date-format')).toBeVisible()
    await expect(page.locator('#s-color-mode')).toBeVisible()
    await expect(page.locator('#s-task-sort')).toBeVisible()
    await expect(page.locator('#s-completed-last')).toBeVisible()
    await expect(page.locator('#s-fiscal-year-start')).toBeVisible()
    await expect(page.locator('#s-number-format')).toBeVisible()
  })

  test('changing currency marks form as dirty', async ({ page }) => {
    // Switch to a different currency to force a dirty state
    const current = await page.inputValue('#s-currency')
    const next = current === '$' ? '€' : '$'
    await page.selectOption('#s-currency', next)

    await expect(page.locator('.settings-dirty')).toBeVisible()
  })

  test('saves settings and shows confirmation', async ({ page }) => {
    await page.selectOption('#s-currency', '$')
    await page.click('button[type="submit"]:has-text("Save")')

    await expect(page.locator('.settings-saved-at')).toBeVisible()
    await expect(page.locator('.settings-dirty')).not.toBeVisible()
  })

  test('save button is disabled when form is unchanged', async ({ page }) => {
    await expect(page.locator('button[type="submit"]:has-text("Save")')).toBeDisabled()
  })

  test('settings preview updates when date format changes', async ({ page }) => {
    const preview = page.locator('.settings-preview-value')
    const initial = (await preview.textContent()) ?? ''

    await page.selectOption('#s-date-format', 'iso')
    const updated = (await preview.textContent()) ?? ''
    expect(updated).not.toBe(initial)
    expect(updated).toMatch(/\d{4}-\d{2}/) // ISO format: YYYY-MM

    // beforeEach navigates fresh for each test, so no need to save —
    // reverting to original makes the form clean (Save would be disabled anyway)
  })

  test('settings preview updates when number format changes', async ({ page }) => {
    const preview = page.locator('.settings-preview-value')

    await page.selectOption('#s-number-format', 'eu')
    const eu = (await preview.textContent()) ?? ''
    expect(eu).toContain('1.234,56')

    await page.selectOption('#s-number-format', 'en')
    const en = (await preview.textContent()) ?? ''
    expect(en).toContain('1,234.56')

    // beforeEach navigates fresh for each test; reverting to original 'en' makes
    // the form clean so there's nothing to save
  })

  // ── Change password ───────────────────────────────────────────────────────

  test('change password section is present', async ({ page }) => {
    await expect(page.locator('h3', { hasText: 'Change Password' })).toBeVisible()
    await expect(page.locator('#pw-current')).toBeVisible()
    await expect(page.locator('#pw-new')).toBeVisible()
    await expect(page.locator('#pw-confirm')).toBeVisible()
  })

  test('rejects mismatched new passwords', async ({ page }) => {
    await page.fill('#pw-current', process.env.ADMIN_PASS ?? 'adminpass123')
    await page.fill('#pw-new', 'newpassword99')
    await page.fill('#pw-confirm', 'differentpassword99')
    await page.click('button:has-text("Update password")')

    await expect(page.locator('.form-error')).toContainText('match')
  })

  test('rejects new password shorter than 8 characters', async ({ page }) => {
    await page.fill('#pw-current', process.env.ADMIN_PASS ?? 'adminpass123')
    await page.fill('#pw-new', 'short')
    await page.fill('#pw-confirm', 'short')
    await page.click('button:has-text("Update password")')

    await expect(page.locator('.form-error')).toContainText('8 characters')
  })

  // ── API Tokens ────────────────────────────────────────────────────────────

  test('API tokens section is present', async ({ page }) => {
    await expect(page.locator('h3', { hasText: 'API Tokens' })).toBeVisible()
    await expect(page.locator('#token-name-input')).toBeVisible()
  })

  test('creates a named API token', async ({ page }) => {
    await page.fill('#token-name-input', 'e2e-test-token')
    await page.click('button:has-text("Create token")')

    // Plaintext reveal appears
    await expect(page.locator('.token-reveal')).toBeVisible()
    await expect(page.locator('.token-reveal-label')).toContainText('not be shown again')

    const tokenText = (await page.locator('.token-reveal code').textContent()) ?? ''
    expect(tokenText.length).toBeGreaterThan(10)

    // Copy button works
    await page.click('.token-reveal button:has-text("Copy")')
    await expect(page.locator('.token-reveal button:has-text("Copied!")')).toBeVisible()
  })

  test('token appears in the list after creation', async ({ page }) => {
    await expect(page.locator('.settings-list-name', { hasText: 'e2e-test-token' })).toBeVisible()
  })

  test('dismisses token reveal', async ({ page }) => {
    // If the reveal is still visible, dismiss it
    const reveal = page.locator('.token-reveal')
    if (await reveal.isVisible()) {
      await page.click('button:has-text("Dismiss")')
    }
    await expect(reveal).not.toBeVisible()
  })

  test('creates an unnamed token', async ({ page }) => {
    await page.click('button:has-text("Create token")')
    await expect(page.locator('.token-reveal')).toBeVisible()
    await page.click('button:has-text("Dismiss")')
    // Unnamed token shows "unnamed" in italic
    await expect(page.locator('.settings-list-name em', { hasText: 'unnamed' }).first()).toBeVisible()
  })

  test('revokes a token with confirmation', async ({ page }) => {
    const tokenItem = page.locator('.settings-list-item', { hasText: 'e2e-test-token' })
    await tokenItem.locator('button:has-text("Revoke")').click()

    // Confirm dialog
    await expect(tokenItem.locator('[role="alert"]')).toContainText('Revoke?')
    await tokenItem.locator('button:has-text("Yes")').click()

    await expect(page.locator('.settings-list-name', { hasText: 'e2e-test-token' })).not.toBeVisible()
  })

  test('cancels token revocation via No button', async ({ page }) => {
    const tokenItem = page.locator('.settings-list-item').first()
    const name = (await tokenItem.locator('.settings-list-name').textContent()) ?? ''

    await tokenItem.locator('button:has-text("Revoke")').click()
    await tokenItem.locator('button:has-text("No")').click()

    await expect(page.locator('.settings-list-name', { hasText: name })).toBeVisible()
  })

  // Clean up the unnamed token left from earlier
  test('revokes the unnamed token', async ({ page }) => {
    const unnamedItem = page.locator('.settings-list-item', { has: page.locator('em', { hasText: 'unnamed' }) }).first()
    await unnamedItem.locator('button:has-text("Revoke")').click()
    await unnamedItem.locator('button:has-text("Yes")').click()
    await expect(page.locator('p.settings-empty', { hasText: 'No tokens' })).toBeVisible()
  })

  // ── Webhooks ──────────────────────────────────────────────────────────────

  test('webhooks section is present', async ({ page }) => {
    await expect(page.locator('h3', { hasText: 'Webhooks' })).toBeVisible()
    await expect(page.locator('#wh-url')).toBeVisible()
  })

  test('webhook creation requires a URL', async ({ page }) => {
    await page.click('button:has-text("Create webhook")')
    // HTML5 validation prevents submission — URL field should be invalid
    await expect(page.locator('#wh-url:invalid')).toBeVisible()
  })

  // ── User management (admin only) ──────────────────────────────────────────

  test('Users section is visible for admin', async ({ page }) => {
    await expect(page.locator('h3', { hasText: 'Users' })).toBeVisible()
  })

  test('creates a new user', async ({ page }) => {
    await page.locator('h3', { hasText: 'Users' }).scrollIntoViewIfNeeded()

    await page.fill('#u-username', 'e2etestuser')
    await page.fill('#u-password', 'testpassword123')
    await page.click('button:has-text("Create user")')

    await expect(page.locator('.settings-list-name', { hasText: 'e2etestuser' })).toBeVisible()
  })

  test('new user can log in', async ({ browser }) => {
    // Create a context with explicit empty storage and baseURL so goto('/') resolves correctly
    const ctx = await browser.newContext({
      baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
      storageState: { cookies: [], origins: [] },
    })
    const newPage = await ctx.newPage()

    await newPage.goto('/')
    await expect(newPage.locator('.login-card')).toBeVisible({ timeout: 15000 })

    await newPage.fill('#login-username', 'e2etestuser')
    await newPage.fill('#login-password', 'testpassword123')
    await newPage.click('button[type="submit"]')
    await expect(newPage.locator('.app-header')).toBeVisible()

    await ctx.close()
  })

  test('deletes the created user with confirmation', async ({ page }) => {
    await page.locator('h3', { hasText: 'Users' }).scrollIntoViewIfNeeded()

    const userItem = page.locator('.settings-list-item', { hasText: 'e2etestuser' })
    await userItem.locator('button:has-text("Delete")').click()

    await expect(userItem.locator('[role="alert"]')).toContainText('Delete?')
    await userItem.locator('button:has-text("Yes")').click()

    await expect(page.locator('.settings-list-name', { hasText: 'e2etestuser' })).not.toBeVisible()
  })

  test('admin cannot delete themselves', async ({ page }) => {
    await page.locator('h3', { hasText: 'Users' }).scrollIntoViewIfNeeded()

    // The admin's own row should not have a Delete button
    const adminName = process.env.ADMIN_USER ?? 'admin'
    const adminItem = page.locator('.settings-list-item', { hasText: adminName })
    await expect(adminItem.locator('button:has-text("Delete")')).not.toBeVisible()
  })

  // ── Audit log ─────────────────────────────────────────────────────────────

  test('audit log section is visible for admin', async ({ page }) => {
    await expect(page.locator('h3', { hasText: 'Audit Log' })).toBeVisible()
  })

  test('audit log shows entries for recent actions', async ({ page }) => {
    await page.locator('h3', { hasText: 'Audit Log' }).scrollIntoViewIfNeeded()
    // We've done several creates/updates in prior tests so there should be entries
    await expect(page.locator('.audit-log-table')).toBeVisible()
    const rows = page.locator('.audit-log-table tbody tr')
    expect(await rows.count()).toBeGreaterThan(0)
  })
})

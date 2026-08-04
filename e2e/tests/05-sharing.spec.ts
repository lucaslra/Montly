import { test, expect } from '@playwright/test'

const ADMIN = process.env.ADMIN_USER ?? 'admin'
const SHAREE = 'shareeuser'
const SHAREE_PASS = 'shareepass123'
const TASK_TITLE = 'Shared Monthly Bill'

// Tests run serially: each one builds on state left by the previous.
test.describe.serial('Task sharing', () => {
  // ── Setup ────────────────────────────────────────────────────────────────────

  test('admin creates the sharee user', async ({ page }) => {
    await page.goto('/settings')
    await page.getByRole('tab', { name: 'Users' }).click()

    await page.fill('#u-username', SHAREE)
    await page.fill('#u-password', SHAREE_PASS)
    await page.click('button:has-text("Create user")')

    await expect(page.locator('.settings-list-name', { hasText: SHAREE })).toBeVisible()
  })

  test('admin creates a task to share', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button:has-text("+ Add Task")')

    const dialog = page.locator('[role="dialog"]')
    await expect(dialog).toBeVisible()

    await page.fill('#task-title', TASK_TITLE)
    await page.selectOption('#task-type', 'bill')
    await page.fill('#task-amount', '50')
    await page.click('button[type="submit"]')

    await expect(dialog).not.toBeVisible()
    await expect(page.locator('.manage-item-title', { hasText: TASK_TITLE })).toBeVisible()
  })

  // ── Sharing via the Share panel ───────────────────────────────────────────────

  test('admin opens Share panel for the task', async ({ page }) => {
    await page.goto('/manage')
    const item = page.locator('.manage-item', { hasText: TASK_TITLE })
    await item.locator('button:has-text("Share")').click()

    await expect(page.locator('.share-panel')).toBeVisible()
    await expect(page.locator('.share-panel')).toContainText('Not shared with anyone yet.')
  })

  test('admin searches for sharee and adds them', async ({ page }) => {
    await page.goto('/manage')
    const item = page.locator('.manage-item', { hasText: TASK_TITLE })
    await item.locator('button:has-text("Share")').click()
    await expect(page.locator('.share-panel')).toBeVisible()

    // Type into the search field (debounce fires after 250 ms)
    await page.fill('.share-search-input', SHAREE.slice(0, 5))
    await expect(page.locator('.share-results')).toBeVisible({ timeout: 2000 })
    await page.locator('.share-result-btn', { hasText: SHAREE }).click()

    // Sharee now appears in the share list
    await expect(page.locator('.share-list')).toContainText(SHAREE)
  })

  // ── Sharee's perspective ──────────────────────────────────────────────────────

  test('sharee sees the task in the monthly view with a shared-by badge', async ({ browser }) => {
    const ctx = await browser.newContext({
      baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
      storageState: { cookies: [], origins: [] },
    })
    const p = await ctx.newPage()

    await p.goto('/')
    await p.fill('#login-username', SHAREE)
    await p.fill('#login-password', SHAREE_PASS)
    await p.click('button[type="submit"]')
    await expect(p.locator('.app-header')).toBeVisible()

    // Shared task is visible
    const task = p.locator('.task-item', { hasText: TASK_TITLE })
    await expect(task).toBeVisible()

    // Badge says "shared by <admin>"
    await expect(task.locator('.shared-by-badge')).toContainText(`shared by ${ADMIN}`)

    await ctx.close()
  })

  test('sharee can toggle the shared task to completed', async ({ browser }) => {
    const ctx = await browser.newContext({
      baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
      storageState: { cookies: [], origins: [] },
    })
    const p = await ctx.newPage()

    await p.goto('/')
    await p.fill('#login-username', SHAREE)
    await p.fill('#login-password', SHAREE_PASS)
    await p.click('button[type="submit"]')
    await expect(p.locator('.app-header')).toBeVisible()

    const task = p.locator('.task-item', { hasText: TASK_TITLE })
    await task.locator('.task-toggle-btn').click()
    await expect(task).toHaveClass(/completed/)

    await ctx.close()
  })

  test('admin sees the shared task as completed (shared state)', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: TASK_TITLE })
    await expect(task).toHaveClass(/completed/)
  })

  test('sharee can skip the shared task (un-completes and skips)', async ({ browser }) => {
    const ctx = await browser.newContext({
      baseURL: process.env.BASE_URL ?? 'http://localhost:8080',
      storageState: { cookies: [], origins: [] },
    })
    const p = await ctx.newPage()

    await p.goto('/')
    await p.fill('#login-username', SHAREE)
    await p.fill('#login-password', SHAREE_PASS)
    await p.click('button[type="submit"]')
    await expect(p.locator('.app-header')).toBeVisible()

    // Un-complete first (toggle back)
    const task = p.locator('.task-item', { hasText: TASK_TITLE })
    await task.locator('.task-toggle-btn').click()
    await expect(task).not.toHaveClass(/completed/)

    // Skip it
    await task.locator('button:has-text("skip")').click()
    await expect(task).toHaveClass(/skipped/)

    await ctx.close()
  })

  // ── Archive flow ──────────────────────────────────────────────────────────────

  test('admin archives the shared task', async ({ page }) => {
    await page.goto('/manage')
    const item = page.locator('.manage-item', { hasText: TASK_TITLE })
    await item.locator('button:has-text("Archive")').click()

    await expect(item.locator('[role="alert"]')).toContainText('Archive?')
    await item.locator('button:has-text("Yes")').click()

    await expect(page.locator('.manage-item', { hasText: TASK_TITLE })).not.toBeVisible()
  })

  test('archived task no longer appears in monthly view', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.task-item', { hasText: TASK_TITLE })).not.toBeVisible()
  })

  test('archived task appears in Archived section', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.archived-toggle')

    await expect(page.locator('.archived-list .manage-item-title', { hasText: TASK_TITLE })).toBeVisible()
  })

  test('admin can restore the archived task', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.archived-toggle')

    const archivedItem = page.locator('.archived-item', { hasText: TASK_TITLE })
    await archivedItem.locator('button:has-text("Restore")').click()

    // Task reappears in the active list
    await expect(page.locator('.manage-item:not(.archived-item)', { hasText: TASK_TITLE })).toBeVisible()
  })

  // ── Cleanup ───────────────────────────────────────────────────────────────────

  test('admin permanently deletes the task', async ({ page }) => {
    await page.goto('/manage')
    const item = page.locator('.manage-item', { hasText: TASK_TITLE })
    await item.locator('button:has-text("Archive")').click()
    await item.locator('button:has-text("Yes")').click()

    // Now delete permanently from Archived section
    await page.click('button.archived-toggle')
    const archivedItem = page.locator('.archived-item', { hasText: TASK_TITLE })
    await archivedItem.locator('button:has-text("Delete")').click()
    await expect(archivedItem.locator('[role="alert"]')).toContainText('permanently')
    await archivedItem.locator('button:has-text("Yes")').click()

    await expect(page.locator('.archived-item', { hasText: TASK_TITLE })).not.toBeVisible()
  })

  test('admin deletes the sharee user', async ({ page }) => {
    await page.goto('/settings')
    await page.getByRole('tab', { name: 'Users' }).click()

    const userItem = page.locator('.settings-list-item', { hasText: SHAREE })
    await userItem.locator('button:has-text("Delete")').click()
    await expect(userItem.locator('[role="alert"]')).toContainText('Delete?')
    await userItem.locator('button:has-text("Yes")').click()

    await expect(page.locator('.settings-list-name', { hasText: SHAREE })).not.toBeVisible()
  })
})

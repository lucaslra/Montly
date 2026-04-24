import { test, expect } from '@playwright/test'

// Tests run serially because they build and tear down shared state within this file
test.describe.serial('Task management', () => {
  test('shows empty state on the monthly view when no tasks exist', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.empty')).toBeVisible()
    await expect(page.locator('.empty')).toContainText('No tasks')
  })

  test('navigates to Manage view via header button', async ({ page }) => {
    await page.goto('/')
    await page.click('button.view-toggle')

    await expect(page.locator('h2', { hasText: 'All Tasks' })).toBeVisible()
    expect(page.url()).toContain('/manage')
  })

  test('navigates back to monthly view via Back button', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.view-toggle')

    await expect(page.locator('.app-header')).toBeVisible()
    expect(page.url()).not.toContain('/manage')
  })

  test('creates a payment task', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button:has-text("+ Add Task")')

    const dialog = page.locator('[role="dialog"]')
    await expect(dialog).toBeVisible()

    await page.fill('#task-title', 'Monthly Rent')
    await page.selectOption('#task-type', 'payment')
    await page.fill('#task-amount', '1200')
    await page.click('button[type="submit"]')

    await expect(dialog).not.toBeVisible()
    await expect(page.locator('.manage-item-title', { hasText: 'Monthly Rent' })).toBeVisible()
  })

  test('creates a subscription task', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button:has-text("+ Add Task")')

    await page.fill('#task-title', 'Streaming Service')
    await page.selectOption('#task-type', 'subscription')
    await page.fill('#task-amount', '15.99')
    await page.click('button[type="submit"]')

    await expect(page.locator('.manage-item-title', { hasText: 'Streaming Service' })).toBeVisible()
  })

  test('creates a reminder task', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button:has-text("+ Add Task")')

    await page.fill('#task-title', 'Call dentist')
    await page.selectOption('#task-type', 'reminder')
    await page.click('button[type="submit"]')

    await expect(page.locator('.manage-item-title', { hasText: 'Call dentist' })).toBeVisible()
  })

  test('task list shows all created tasks', async ({ page }) => {
    await page.goto('/manage')
    await expect(page.locator('.manage-item')).toHaveCount(3)
  })

  test('tasks appear on the monthly view', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.task-item')).toHaveCount(3)
    await expect(page.locator('.task-item', { hasText: 'Monthly Rent' })).toBeVisible()
    await expect(page.locator('.task-item', { hasText: 'Streaming Service' })).toBeVisible()
    await expect(page.locator('.task-item', { hasText: 'Call dentist' })).toBeVisible()
  })

  test('progress bar shows 0 of N tasks done', async ({ page }) => {
    await page.goto('/')
    // Use auto-retry assertion to wait for tasks to load before checking
    await expect(page.locator('.progress-text')).toHaveText('0/3')
  })

  test('monetary summary is visible for payment tasks', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.monetary-summary')).toBeVisible()
  })

  test('edits a task title and amount', async ({ page }) => {
    await page.goto('/manage')
    const rentItem = page.locator('.manage-item', { hasText: 'Monthly Rent' })
    await rentItem.locator('button', { hasText: 'Edit' }).click()

    const dialog = page.locator('[role="dialog"]')
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('h3')).toHaveText('Edit Task')

    await page.fill('#task-title', 'Monthly Rent (updated)')
    await page.fill('#task-amount', '1250')
    await page.click('button[type="submit"]')

    await expect(dialog).not.toBeVisible()
    await expect(page.locator('.manage-item-title', { hasText: 'Monthly Rent (updated)' })).toBeVisible()
  })

  test('shows amount in task meta after edit', async ({ page }) => {
    await page.goto('/manage')
    const rentItem = page.locator('.manage-item', { hasText: 'Monthly Rent (updated)' })
    await expect(rentItem.locator('.meta-amount')).toContainText('1,250')
  })

  test('cancelling the task form discards changes', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button:has-text("+ Add Task")')
    await page.fill('#task-title', 'Should not be saved')
    await page.click('button:has-text("Cancel")')

    await expect(page.locator('.manage-item-title', { hasText: 'Should not be saved' })).not.toBeVisible()
  })

  test('search filters tasks by title', async ({ page }) => {
    await page.goto('/manage')
    await page.fill('input[aria-label="Search tasks"]', 'Rent')

    await expect(page.locator('.manage-item')).toHaveCount(1)
    await expect(page.locator('.manage-item-title', { hasText: 'Monthly Rent (updated)' })).toBeVisible()
  })

  test('clearing search restores full list', async ({ page }) => {
    await page.goto('/manage')
    await page.fill('input[aria-label="Search tasks"]', 'Rent')
    await page.fill('input[aria-label="Search tasks"]', '')

    await expect(page.locator('.manage-item')).toHaveCount(3)
  })

  test('type filter: payment shows only payment tasks', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.type-filter-payment')

    await expect(page.locator('.manage-item')).toHaveCount(1)
    await expect(page.locator('.manage-item-title', { hasText: 'Monthly Rent (updated)' })).toBeVisible()
  })

  test('type filter: subscription shows only subscription tasks', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.type-filter-subscription')

    await expect(page.locator('.manage-item')).toHaveCount(1)
    await expect(page.locator('.manage-item-title', { hasText: 'Streaming Service' })).toBeVisible()
  })

  test('clicking active type filter again deselects it', async ({ page }) => {
    await page.goto('/manage')
    await page.click('button.type-filter-payment')
    await expect(page.locator('.manage-item')).toHaveCount(1)

    await page.click('button.type-filter-payment') // deselect
    await expect(page.locator('.manage-item')).toHaveCount(3)
  })

  test('month navigation: previous month', async ({ page }) => {
    await page.goto('/')
    const monthLabel = page.locator('.month-label-btn')
    const initial = (await monthLabel.textContent()) ?? ''

    await page.click('button[aria-label="Previous month"]')
    const prev = (await monthLabel.textContent()) ?? ''
    expect(prev).not.toBe(initial)
  })

  test('month navigation: next month returns to current', async ({ page }) => {
    await page.goto('/')
    const monthLabel = page.locator('.month-label-btn')
    const initial = (await monthLabel.textContent()) ?? ''

    await page.click('button[aria-label="Previous month"]')
    await page.click('button[aria-label="Next month"]')
    const current = (await monthLabel.textContent()) ?? ''
    expect(current).toBe(initial)
  })

  test('month picker opens on label click', async ({ page }) => {
    await page.goto('/')
    await page.click('.month-label-btn')
    await expect(page.locator('.month-jump-picker')).toBeVisible()
  })

  test('month picker Today button jumps to current month', async ({ page }) => {
    await page.goto('/')
    const monthLabel = page.locator('.month-label-btn')
    const initial = (await monthLabel.textContent()) ?? ''

    // Go two months back
    await page.click('button[aria-label="Previous month"]')
    await page.click('button[aria-label="Previous month"]')

    // Open picker and click Today
    await page.click('.month-label-btn')
    await page.click('button:has-text("Today")')

    const restored = (await monthLabel.textContent()) ?? ''
    expect(restored).toBe(initial)
  })

  test('deletes the reminder task via inline confirm', async ({ page }) => {
    await page.goto('/manage')
    const dentistItem = page.locator('.manage-item', { hasText: 'Call dentist' })

    await dentistItem.locator('button.btn-danger').click()
    // Confirm dialog appears inline
    await expect(dentistItem.locator('.delete-confirm')).toBeVisible()
    await dentistItem.locator('button', { hasText: 'Yes' }).click()

    await expect(page.locator('.manage-item', { hasText: 'Call dentist' })).not.toBeVisible()
    await expect(page.locator('.manage-item')).toHaveCount(2)
  })

  test('No button on delete confirm cancels deletion', async ({ page }) => {
    await page.goto('/manage')
    const rentItem = page.locator('.manage-item', { hasText: 'Monthly Rent (updated)' })

    await rentItem.locator('button.btn-danger').click()
    await expect(rentItem.locator('.delete-confirm')).toBeVisible()
    await rentItem.locator('button', { hasText: 'No' }).click()

    // Task is still there
    await expect(page.locator('.manage-item', { hasText: 'Monthly Rent (updated)' })).toBeVisible()
  })
})

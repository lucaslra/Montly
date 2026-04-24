import { test, expect } from '@playwright/test'
import * as path from 'path'

// These tests rely on the payment tasks created by tasks.spec.ts running first.
// Within this file, tests run serially to maintain completion state.
test.describe.serial('Completions', () => {
  test('monthly view shows due amount for uncompleted payment tasks', async ({ page }) => {
    await page.goto('/')
    await expect(page.locator('.monetary-summary')).toBeVisible()
    // At least one uncompleted task → "Due" label shown
    await expect(page.locator('.monetary-summary')).toContainText('Due')
  })

  test('toggles a task to completed', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await task.locator('.task-toggle-btn').click()

    await expect(task).toHaveClass(/completed/)
    // Checkbox should show a checkmark
    await expect(task.locator('.task-checkbox')).toHaveText('✓')
  })

  test('progress bar updates after completion', async ({ page }) => {
    await page.goto('/')
    // Auto-retry until tasks load and at least one completion is reflected
    await expect(page.locator('.progress-text')).toHaveText(/^[1-9]\d*\/\d+$/)
  })

  test('amount display is visible on a completed payment task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await expect(task.locator('.amount-display')).toBeVisible()
  })

  test('edits per-entry amount on a completed task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })

    await task.locator('.amount-display').click()
    await expect(task.locator('.amount-inline-input')).toBeVisible()

    await task.locator('.amount-inline-input').clear()
    await task.locator('.amount-inline-input').fill('1300')
    await task.locator('.amount-confirm-btn').click()

    await expect(task.locator('.amount-display')).toContainText('1300')
    await expect(task.locator('.override-badge')).toBeVisible()
  })

  test('monetary summary shows settled when paid ≥ due', async ({ page }) => {
    await page.goto('/')
    // Toggle the subscription task too so all monetary tasks are done
    const subTask = page.locator('.task-item', { hasText: 'Streaming Service' })
    await subTask.locator('.task-toggle-btn').click()
    await expect(subTask).toHaveClass(/completed/)

    // At this point both monetary tasks are complete → settled
    await expect(page.locator('.monetary-settled')).toBeVisible()
    await expect(page.locator('.monetary-settled')).toContainText('Settled')
  })

  test('can cancel amount edit with Escape', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    const original = (await task.locator('.amount-display').textContent()) ?? ''

    await task.locator('.amount-display').click()
    await task.locator('.amount-inline-input').fill('9999')
    await task.locator('.amount-inline-input').press('Escape')

    // Value should be unchanged
    await expect(task.locator('.amount-display')).toHaveText(original)
  })

  test('attaches a receipt to a completed task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    const attachBtn = task.locator('button[aria-label="Attach receipt"]')

    const fileChooserPromise = page.waitForEvent('filechooser')
    await attachBtn.click()
    const fileChooser = await fileChooserPromise
    await fileChooser.setFiles(path.join(__dirname, '../fixtures/receipt.png'))

    // Receipt link should appear
    await expect(task.locator('a[aria-label="View receipt"]')).toBeVisible({ timeout: 10000 })
  })

  test('receipt link is an attachment', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    const link = task.locator('a[aria-label="View receipt"]')

    // The href should point to the receipts API
    const href = await link.getAttribute('href')
    expect(href).toMatch(/\/api\/receipts\//)
  })

  test('unmark with receipt shows confirm dialog', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })

    // Clicking toggle on a task with a receipt should show the undo confirm
    await task.locator('.task-toggle-btn').click()
    await expect(task.locator('.undo-confirm')).toBeVisible()
    await expect(task.locator('.undo-confirm-label')).toContainText('receipt')

    // Cancel — keep the task completed
    await task.locator('button', { hasText: 'Cancel' }).click()
    await expect(task).toHaveClass(/completed/)
  })

  test('removes a receipt from a completed task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await task.locator('button[aria-label="Remove receipt"]').click()

    // Confirm removal
    await expect(task.locator('.undo-confirm')).toBeVisible()
    await task.locator('button', { hasText: 'Remove' }).click()

    // Receipt link gone, attach button back
    await expect(task.locator('a[aria-label="View receipt"]')).not.toBeVisible()
    await expect(task.locator('button[aria-label="Attach receipt"]')).toBeVisible()
  })

  test('adds a completion note', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })

    await task.locator('.note-display--empty').click()
    await task.locator('textarea[aria-label="Completion note"]').fill('Paid via bank transfer')
    await task.locator('.amount-confirm-btn').click()

    await expect(task.locator('.note-display--filled')).toHaveText('Paid via bank transfer')
  })

  test('toggles a task back to incomplete', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    // No receipt now, so direct toggle works
    await task.locator('.task-toggle-btn').click()

    await expect(task).not.toHaveClass(/completed/)
  })

  test('skips a task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await task.locator('button[aria-label*="Skip"]').click()

    await expect(task).toHaveClass(/skipped/)
    // Checkbox should show the skip dash
    await expect(task.locator('.task-checkbox')).toHaveText('—')
  })

  test('un-skips a task', async ({ page }) => {
    await page.goto('/')
    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await task.locator('button[aria-label*="Un-skip"]').click()

    await expect(task).not.toHaveClass(/skipped/)
  })

  test('completing a task in a different month does not affect the current month', async ({ page }) => {
    await page.goto('/')
    // Navigate to next month — tasks created this month are still active
    await page.click('button[aria-label="Next month"]')

    const task = page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    await task.locator('.task-toggle-btn').click()
    await expect(task).toHaveClass(/completed/)

    // Navigate back — current month should still show the task as incomplete
    await page.click('button[aria-label="Previous month"]')
    await expect(
      page.locator('.task-item', { hasText: 'Monthly Rent (updated)' })
    ).not.toHaveClass(/completed/)
  })
})

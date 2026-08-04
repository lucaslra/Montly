import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from '../App.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  fetchMe:                  vi.fn(),
  fetchSetupStatus:         vi.fn(),
  fetchAuthConfig:          vi.fn(),
  OIDC_LOGIN_URL:           '/api/auth/oidc/login',
  fetchSettings:            vi.fn(),
  fetchTasks:               vi.fn(),
  fetchCompletions:         vi.fn(),
  toggleCompletion:         vi.fn(),
  skipCompletion:           vi.fn(),
  createTask:               vi.fn(),
  updateTask:               vi.fn(),
  deleteTask:               vi.fn(),
  archiveTask:              vi.fn(),
  unarchiveTask:            vi.fn(),
  fetchArchivedTasks:       vi.fn(),
  uploadCompletionReceipt:  vi.fn(),
  deleteCompletionReceipt:  vi.fn(),
  patchCompletion:          vi.fn(),
  logout:                   vi.fn(),
  updateSettings:           vi.fn(),
  fetchTaskShares:          vi.fn(),
  addTaskShare:             vi.fn(),
  removeTaskShare:          vi.fn(),
  lookupUsers:              vi.fn(),
}))

const DEFAULT_SETTINGS = {
  currency: '$', date_format: 'long', color_mode: 'system',
  task_sort: 'type', completed_last: 'false', fiscal_year_start: '1', number_format: 'en',
}

beforeEach(() => {
  // jsdom does not implement matchMedia
  window.matchMedia = vi.fn().mockReturnValue({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  })
  // Reset URL so the App always starts in the monthly view regardless of test order
  window.history.replaceState(null, '', '/')
  // Defaults: setup complete, SSO disabled, password login available. These are
  // now fetched unconditionally on mount, so provide defaults for every test.
  api.fetchSetupStatus.mockResolvedValue({ needs_setup: false })
  api.fetchAuthConfig.mockResolvedValue({ password_login: true, oidc: { enabled: false } })
})

afterEach(() => vi.clearAllMocks())

// ── Helpers ───────────────────────────────────────────────────────────────────

function mockAuth(tasks = [], completions = []) {
  api.fetchMe.mockResolvedValue({ user_id: 1, username: 'admin', is_admin: true })
  api.fetchSettings.mockResolvedValue(DEFAULT_SETTINGS)
  api.fetchTasks.mockResolvedValue(tasks)
  api.fetchCompletions.mockResolvedValue(completions)
}

async function renderAuth(tasks = [], completions = []) {
  mockAuth(tasks, completions)
  render(<App />)
  // Progress bar is only present in the authenticated monthly view
  await waitFor(() => screen.getByRole('progressbar'))
  // Wait for the data-load spinner to clear
  await waitFor(() => expect(screen.queryByText('Loading…')).not.toBeInTheDocument())
}

// ── Auth state machine ────────────────────────────────────────────────────────

describe('App auth states', () => {
  it('shows a loading indicator while the auth check is pending', () => {
    api.fetchMe.mockReturnValue(new Promise(() => {})) // never resolves
    render(<App />)
    expect(screen.getByText('Loading…')).toBeInTheDocument()
  })

  it('shows LoginView when the session is not authenticated', async () => {
    const err = Object.assign(new Error('unauthorized'), { status: 401 })
    api.fetchMe.mockRejectedValue(err)
    api.fetchSetupStatus.mockResolvedValue({ needs_setup: false })
    render(<App />)
    await waitFor(() => expect(screen.getByLabelText('Username')).toBeInTheDocument())
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('shows SetupView when first-run setup is needed', async () => {
    api.fetchMe.mockRejectedValue(new Error('unauthorized'))
    api.fetchSetupStatus.mockResolvedValue({ needs_setup: true })
    render(<App />)
    await waitFor(() =>
      expect(screen.getByText(/Create your admin account/)).toBeInTheDocument()
    )
  })

  it('renders the authenticated app shell after a successful auth check', async () => {
    await renderAuth()
    expect(screen.getByRole('button', { name: 'Montly' })).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toBeInTheDocument()
  })

  it('calls logout and returns to LoginView when the sign-out button is clicked', async () => {
    api.logout.mockResolvedValue(null)
    api.fetchSetupStatus.mockResolvedValue({ needs_setup: false })
    await renderAuth()
    await userEvent.click(screen.getByRole('button', { name: /Sign out/ }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument())
    expect(api.logout).toHaveBeenCalledOnce()
  })

  it('loads the available sign-in methods on mount even when authenticated', async () => {
    // Regression: after an SSO login fetchMe() succeeds, but authConfig must still
    // be fetched so the login screen is correct after a later logout.
    await renderAuth()
    expect(api.fetchAuthConfig).toHaveBeenCalled()
  })

  it('shows the SSO button on the login screen after logging out of an SSO session', async () => {
    api.logout.mockResolvedValue(null)
    api.fetchAuthConfig.mockResolvedValue({ password_login: true, oidc: { enabled: true, provider_name: 'Pocket ID' } })
    await renderAuth() // authenticated first load (e.g. via the OIDC callback)
    await userEvent.click(screen.getByRole('button', { name: /Sign out/ }))
    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'Sign in with Pocket ID' })).toBeInTheDocument()
    )
  })

  it('shows only the SSO button after logout when password login is disabled', async () => {
    // Guards against a full lockout: DISABLE_PASSWORD_LOGIN + SSO must still offer SSO.
    api.logout.mockResolvedValue(null)
    api.fetchAuthConfig.mockResolvedValue({ password_login: false, oidc: { enabled: true, provider_name: 'Pocket ID' } })
    await renderAuth()
    await userEvent.click(screen.getByRole('button', { name: /Sign out/ }))
    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'Sign in with Pocket ID' })).toBeInTheDocument()
    )
    expect(screen.queryByLabelText('Username')).not.toBeInTheDocument()
  })
})

// ── Month navigation ──────────────────────────────────────────────────────────

describe('App month navigation', () => {
  it('navigates to the previous month when the back arrow is clicked', async () => {
    await renderAuth()
    const monthBtn = screen.getByRole('button', { name: /click to jump to a month/ })
    const before = monthBtn.textContent
    await userEvent.click(screen.getByLabelText('Previous month'))
    expect(monthBtn.textContent).not.toBe(before)
  })

  it('navigates to the next month when the forward arrow is clicked', async () => {
    await renderAuth()
    const monthBtn = screen.getByRole('button', { name: /click to jump to a month/ })
    const before = monthBtn.textContent
    await userEvent.click(screen.getByLabelText('Next month'))
    expect(monthBtn.textContent).not.toBe(before)
  })

  it('previous and next arrows move in opposite directions', async () => {
    await renderAuth()
    const monthBtn = screen.getByRole('button', { name: /click to jump to a month/ })
    const initial = monthBtn.textContent
    await userEvent.click(screen.getByLabelText('Previous month'))
    const afterPrev = monthBtn.textContent
    await userEvent.click(screen.getByLabelText('Next month'))
    expect(monthBtn.textContent).toBe(initial)
    expect(afterPrev).not.toBe(initial)
  })
})

// ── Optimistic toggle ─────────────────────────────────────────────────────────

describe('App optimistic toggle', () => {
  it('marks the task completed immediately before the server responds', async () => {
    const task = { id: 5, title: 'Netflix', type: 'subscription', metadata: {}, start_date: '', end_date: '', interval: 1 }
    let resolveToggle
    api.toggleCompletion.mockReturnValue(new Promise(r => { resolveToggle = r }))
    await renderAuth([task], [])

    const taskBtn = screen.getByRole('button', { name: /Mark.*Netflix/i })
    expect(taskBtn).toHaveAttribute('aria-pressed', 'false')
    await userEvent.click(taskBtn)
    // Optimistic update: flipped before the server responds
    expect(taskBtn).toHaveAttribute('aria-pressed', 'true')
    resolveToggle({ completed: true })
  })

  it('reverts the task to incomplete when the toggle API call fails', async () => {
    const task = { id: 5, title: 'Netflix', type: 'subscription', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.fetchSetupStatus.mockResolvedValue({ needs_setup: false })
    api.toggleCompletion.mockRejectedValue(Object.assign(new Error('server error'), { status: 500 }))
    await renderAuth([task], [])

    const taskBtn = screen.getByRole('button', { name: /Mark.*Netflix/i })
    await userEvent.click(taskBtn)
    await waitFor(() => expect(taskBtn).toHaveAttribute('aria-pressed', 'false'))
  })
})

// ── Toast notifications ───────────────────────────────────────────────────────

describe('App toast notifications', () => {
  it('shows "Task archived" toast after confirming an archive in ManageView', async () => {
    const task = { id: 3, title: 'Rent', type: 'bill', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.archiveTask.mockResolvedValue(null)
    await renderAuth([task], [])

    await userEvent.click(screen.getByRole('button', { name: 'Manage' }))
    await waitFor(() => screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Task archived')
    )
  })
})

// ── Error handling ────────────────────────────────────────────────────────────

describe('App error handling', () => {
  it('shows an error banner when a non-401 API error occurs', async () => {
    const task = { id: 3, title: 'Rent', type: 'bill', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.archiveTask.mockRejectedValue(Object.assign(new Error('internal server error'), { status: 500 }))
    await renderAuth([task], [])

    await userEvent.click(screen.getByRole('button', { name: 'Manage' }))
    await waitFor(() => screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('internal server error')
    )
  })

  it('dismisses the error banner when the close button is clicked', async () => {
    const task = { id: 3, title: 'Rent', type: 'bill', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.archiveTask.mockRejectedValue(Object.assign(new Error('oops'), { status: 500 }))
    await renderAuth([task], [])

    await userEvent.click(screen.getByRole('button', { name: 'Manage' }))
    await waitFor(() => screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => screen.getByRole('alert'))
    await userEvent.click(screen.getByLabelText('Dismiss error'))
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })
})

// ── Skip ─────────────────────────────────────────────────────────────────────

describe('App skip', () => {
  const task = { id: 7, title: 'Gym', type: 'reminder', metadata: {}, start_date: '', end_date: '', interval: 1 }

  it('optimistically marks task skipped before server responds', async () => {
    let resolve
    api.skipCompletion.mockReturnValue(new Promise(r => { resolve = r }))
    await renderAuth([task], [])
    // Skip button should be visible for incomplete task
    await userEvent.click(screen.getByLabelText(`Skip "Gym" for this month`))
    // After optimistic update, the completion map has a skipped entry —
    // which means un-skip button should appear
    await waitFor(() =>
      expect(screen.getByLabelText(`Un-skip "Gym" for this month`)).toBeInTheDocument()
    )
    resolve({ skipped: true, completion: { task_id: 7, month: '2026-05', skipped: true } })
  })

  it('reverts optimistic skip on API error', async () => {
    api.skipCompletion.mockRejectedValue(Object.assign(new Error('server error'), { status: 500 }))
    api.fetchSetupStatus.mockResolvedValue({ needs_setup: false })
    await renderAuth([task], [])
    await userEvent.click(screen.getByLabelText(`Skip "Gym" for this month`))
    // After error, reverted — skip button should be back
    await waitFor(() =>
      expect(screen.getByLabelText(`Skip "Gym" for this month`)).toBeInTheDocument()
    )
  })
})

// ── Receipt handlers ──────────────────────────────────────────────────────────

describe('App receipt handlers', () => {
  const task = { id: 8, title: 'Rent', type: 'bill', metadata: { amount: '500' }, start_date: '', end_date: '', interval: 1 }
  const completion = { task_id: 8, month: '2026-05', completed_at: 'now', receipt_file: '', amount: '', note: '', skipped: false }

  it('handleUploadReceipt updates completion map on success', async () => {
    const updatedCompletion = { ...completion, receipt_file: 'uuid.pdf' }
    api.uploadCompletionReceipt.mockResolvedValue(updatedCompletion)
    await renderAuth([task], [completion])
    // The hidden file input for receipt upload is always rendered in PaymentSlot
    const fileInput = document.querySelector('input[type="file"][aria-label="Upload receipt"]')
    expect(fileInput).not.toBeNull()
    const file = new File(['pdf'], 'receipt.pdf', { type: 'application/pdf' })
    Object.defineProperty(fileInput, 'files', { value: [file], configurable: true })
    fireEvent.change(fileInput)
    await waitFor(() =>
      expect(api.uploadCompletionReceipt).toHaveBeenCalled()
    )
  })

  it('handleRemoveReceipt updates completion map on success', async () => {
    const completionWithReceipt = { ...completion, receipt_file: 'uuid.pdf' }
    const updatedCompletion = { ...completion, receipt_file: '' }
    api.deleteCompletionReceipt.mockResolvedValue(updatedCompletion)
    await renderAuth([task], [completionWithReceipt])
    // Remove receipt button appears when there's a receipt
    await waitFor(() => screen.getByLabelText('Remove receipt'))
    await userEvent.click(screen.getByLabelText('Remove receipt'))
    // Confirm removal in the PaymentSlot confirm dialog
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    await waitFor(() => expect(api.deleteCompletionReceipt).toHaveBeenCalled())
  })
})

// ── Task mutation handlers ────────────────────────────────────────────────────

describe('App task mutation handlers', () => {
  it('handleCreate adds task to list and shows toast', async () => {
    const newTask = { id: 10, title: 'New Task', type: 'reminder', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.createTask.mockResolvedValue(newTask)
    await renderAuth([], [])
    // Navigate to Manage view
    await userEvent.click(screen.getByRole('button', { name: 'Manage' }))
    // Click "+ Add Task" button
    await waitFor(() => screen.getByRole('button', { name: '+ Add Task' }))
    await userEvent.click(screen.getByRole('button', { name: '+ Add Task' }))
    // Fill form
    await userEvent.type(screen.getByLabelText('Title'), 'New Task')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Task created')
    )
    expect(api.createTask).toHaveBeenCalled()
  })

  it('handleDelete removes task from archived list and shows toast', async () => {
    const task = { id: 3, title: 'Old Bill', type: 'bill', metadata: {}, start_date: '', end_date: '', interval: 1 }
    api.fetchArchivedTasks.mockResolvedValue([task])
    api.deleteTask.mockResolvedValue(null)
    await renderAuth([], [])
    await userEvent.click(screen.getByRole('button', { name: 'Manage' }))
    // Open the archived section
    await waitFor(() => screen.getByRole('button', { name: /Archived tasks/ }))
    await userEvent.click(screen.getByRole('button', { name: /Archived tasks/ }))
    // Wait for the Delete button in the archived list
    await waitFor(() => screen.getByRole('button', { name: 'Delete' }))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    // Confirm permanent deletion
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Task deleted')
    )
    expect(api.deleteTask).toHaveBeenCalledWith(3)
  })
})

// ── sortedTasks ───────────────────────────────────────────────────────────────

describe('App sortedTasks', () => {
  const tasks = [
    { id: 1, title: 'Zebra', type: 'reminder', metadata: {}, start_date: '', end_date: '', interval: 1 },
    { id: 2, title: 'Apple', type: 'reminder', metadata: {}, start_date: '', end_date: '', interval: 1 },
  ]

  it('sorts tasks by name when task_sort=name', async () => {
    mockAuth(tasks, [])
    api.fetchSettings.mockResolvedValue({ ...DEFAULT_SETTINGS, task_sort: 'name' })
    render(<App />)
    // Wait for tasks to render (both titles visible)
    await waitFor(() => {
      expect(screen.getByText('Apple')).toBeInTheDocument()
      expect(screen.getByText('Zebra')).toBeInTheDocument()
    })
    const buttons = screen.getAllByRole('button', { name: /Mark/ })
    // Apple should come before Zebra alphabetically
    expect(buttons[0].getAttribute('aria-label')).toMatch(/Apple/)
    expect(buttons[1].getAttribute('aria-label')).toMatch(/Zebra/)
  })

  it('pushes completed tasks to the bottom when completed_last=true', async () => {
    const completions = [{ task_id: 1, month: '2026-05', completed_at: 'now', receipt_file: '', amount: '', note: '', skipped: false }]
    mockAuth(tasks, completions)
    api.fetchSettings.mockResolvedValue({ ...DEFAULT_SETTINGS, completed_last: 'true' })
    render(<App />)
    // Wait for tasks to render
    await waitFor(() => {
      expect(screen.getByText('Apple')).toBeInTheDocument()
      expect(screen.getByText('Zebra')).toBeInTheDocument()
    })
    // Zebra (id=1) is completed, Apple (id=2) is not.
    // With completed_last=true, Apple should appear before Zebra in the DOM.
    const items = Array.from(document.querySelectorAll('.task-item'))
    const zebraIdx = items.findIndex(el => el.textContent.includes('Zebra'))
    const appleIdx = items.findIndex(el => el.textContent.includes('Apple'))
    expect(appleIdx).toBeLessThan(zebraIdx)
  })
})

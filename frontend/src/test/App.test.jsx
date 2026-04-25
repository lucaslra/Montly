import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from '../App.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  fetchMe:                  vi.fn(),
  fetchSetupStatus:         vi.fn(),
  fetchSettings:            vi.fn(),
  fetchTasks:               vi.fn(),
  fetchCompletions:         vi.fn(),
  toggleCompletion:         vi.fn(),
  createTask:               vi.fn(),
  updateTask:               vi.fn(),
  deleteTask:               vi.fn(),
  archiveTask:              vi.fn(),
  unarchiveTask:            vi.fn(),
  uploadCompletionReceipt:  vi.fn(),
  deleteCompletionReceipt:  vi.fn(),
  patchCompletion:          vi.fn(),
  logout:                   vi.fn(),
  updateSettings:           vi.fn(),
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

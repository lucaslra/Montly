import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ManageView from '../components/ManageView.jsx'

// ManageView delegates API calls to its onCreate/onUpdate/onDelete prop callbacks —
// no api.js mocking needed.

function makeTask(overrides = {}) {
  return {
    id: 1,
    title: 'Default Task',
    description: null,
    type: 'reminder',
    metadata: {},
    start_date: '',
    end_date: '',
    interval: 1,
    ...overrides,
  }
}

function renderManage(tasks = [], extra = {}) {
  const props = {
    tasks,
    onCreate: vi.fn(),
    onUpdate: vi.fn(),
    onDelete: vi.fn(),
    onArchive: vi.fn(),
    onUnarchive: vi.fn(),
    ...extra,
  }
  const result = render(<ManageView {...props} />)
  return { ...result, props }
}

describe('ManageView', () => {
  beforeEach(() => vi.clearAllMocks())

  // ── Empty state ────────────────────────────────────────────────────────────

  it('shows "No tasks yet" message when tasks array is empty', () => {
    renderManage([])
    expect(screen.getByText('No tasks yet. Add one above.')).toBeInTheDocument()
  })

  it('shows the "+ Add Task" button regardless of task count', () => {
    renderManage([])
    expect(screen.getByRole('button', { name: '+ Add Task' })).toBeInTheDocument()
  })

  // ── Task list rendering ────────────────────────────────────────────────────

  it('renders each task title and type badge', () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix', type: 'subscription' }),
      makeTask({ id: 2, title: 'Gym', type: 'reminder' }),
    ]
    renderManage(tasks)
    // Scope to the task list <ul> to avoid collision with the type-filter buttons
    const list = screen.getByRole('list')
    expect(within(list).getByText('Netflix')).toBeInTheDocument()
    expect(within(list).getByText('subscription')).toBeInTheDocument()
    expect(within(list).getByText('Gym')).toBeInTheDocument()
    expect(within(list).getByText('reminder')).toBeInTheDocument()
  })

  it('shows formatted monetary amount for payment-type tasks', () => {
    const tasks = [
      makeTask({ id: 1, title: 'Rent', type: 'bill', metadata: { amount: '850' } }),
    ]
    renderManage(tasks, { currency: '$', numberFormat: 'en' })
    expect(screen.getByText('$850.00')).toBeInTheDocument()
  })

  it('does not show amount for reminder-type tasks', () => {
    const tasks = [
      makeTask({ id: 1, title: 'Call mum', type: 'reminder', metadata: { amount: '99' } }),
    ]
    renderManage(tasks)
    expect(screen.queryByText(/\$99/)).not.toBeInTheDocument()
  })

  it('shows interval badge for multi-month recurrence', () => {
    const tasks = [
      makeTask({ id: 1, title: 'Tax', type: 'bill', interval: 3 }),
    ]
    renderManage(tasks)
    expect(screen.getByText('Trimestral')).toBeInTheDocument()
  })

  it('shows start_date badge when set', () => {
    const tasks = [
      makeTask({ id: 1, title: 'Pension', type: 'payment', start_date: '2026-01' }),
    ]
    renderManage(tasks)
    expect(screen.getByText(/from/)).toBeInTheDocument()
  })

  it('shows description when present', () => {
    const tasks = [makeTask({ id: 1, title: 'T', description: 'Some detail' })]
    renderManage(tasks)
    expect(screen.getByText('Some detail')).toBeInTheDocument()
  })

  // ── Search filtering ───────────────────────────────────────────────────────

  it('filters tasks by title substring', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix', type: 'subscription' }),
      makeTask({ id: 2, title: 'Spotify', type: 'subscription' }),
    ]
    renderManage(tasks)
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search tasks' }), 'net')
    expect(screen.getByText('Netflix')).toBeInTheDocument()
    expect(screen.queryByText('Spotify')).not.toBeInTheDocument()
  })

  it('filters tasks by description substring', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'T1', description: 'streaming service' }),
      makeTask({ id: 2, title: 'T2', description: 'music app' }),
    ]
    renderManage(tasks)
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search tasks' }), 'music')
    expect(screen.getByText('T2')).toBeInTheDocument()
    expect(screen.queryByText('T1')).not.toBeInTheDocument()
  })

  it('shows "No tasks match your filter" when search yields no results', async () => {
    const tasks = [makeTask({ id: 1, title: 'Netflix' })]
    renderManage(tasks)
    await userEvent.type(screen.getByRole('searchbox', { name: 'Search tasks' }), 'zzz')
    expect(screen.getByText('No tasks match your filter.')).toBeInTheDocument()
  })

  // ── Type filter ────────────────────────────────────────────────────────────

  it('filters to the selected type', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix', type: 'subscription' }),
      makeTask({ id: 2, title: 'Rent',    type: 'bill' }),
    ]
    renderManage(tasks)
    await userEvent.click(screen.getByRole('button', { name: 'subscription' }))
    expect(screen.getByText('Netflix')).toBeInTheDocument()
    expect(screen.queryByText('Rent')).not.toBeInTheDocument()
  })

  it('clicking the active type button again shows all tasks', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix', type: 'subscription' }),
      makeTask({ id: 2, title: 'Rent',    type: 'bill' }),
    ]
    renderManage(tasks)
    await userEvent.click(screen.getByRole('button', { name: 'subscription' }))
    await userEvent.click(screen.getByRole('button', { name: 'subscription' }))
    expect(screen.getByText('Netflix')).toBeInTheDocument()
    expect(screen.getByText('Rent')).toBeInTheDocument()
  })

  it('"All" button resets an active type filter', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix', type: 'subscription' }),
      makeTask({ id: 2, title: 'Rent',    type: 'bill' }),
    ]
    renderManage(tasks)
    await userEvent.click(screen.getByRole('button', { name: 'subscription' }))
    await userEvent.click(screen.getByRole('button', { name: 'All' }))
    expect(screen.getByText('Netflix')).toBeInTheDocument()
    expect(screen.getByText('Rent')).toBeInTheDocument()
  })

  // ── Edit ───────────────────────────────────────────────────────────────────

  it('opens the TaskForm modal when Edit is clicked', async () => {
    renderManage([makeTask({ id: 1, title: 'Netflix' })])
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Edit Task')).toBeInTheDocument()
  })

  it('passes the task data to TaskForm when editing', async () => {
    renderManage([makeTask({ id: 1, title: 'Netflix' })])
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    expect(screen.getByDisplayValue('Netflix')).toBeInTheDocument()
  })

  it('closes the modal when TaskForm cancel is clicked', async () => {
    renderManage([makeTask({ id: 1, title: 'Netflix' })])
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('calls onUpdate with the edited values and closes the modal', async () => {
    const onUpdate = vi.fn().mockResolvedValue(undefined)
    renderManage([makeTask({ id: 7, title: 'Netflix', type: 'reminder' })], { onUpdate })
    await userEvent.click(screen.getByRole('button', { name: 'Edit' }))
    // Clear title and type new one
    const titleInput = screen.getByDisplayValue('Netflix')
    await userEvent.clear(titleInput)
    await userEvent.type(titleInput, 'Netflix HD')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(onUpdate).toHaveBeenCalledWith(7, 'Netflix HD', '', 'reminder', expect.anything(), '', '', 1)
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  // ── Create ─────────────────────────────────────────────────────────────────

  it('opens an empty TaskForm when "+ Add Task" is clicked', async () => {
    renderManage([])
    await userEvent.click(screen.getByRole('button', { name: '+ Add Task' }))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('New Task')).toBeInTheDocument()
  })

  it('calls onCreate with the entered values and closes the modal on success', async () => {
    const onCreate = vi.fn().mockResolvedValue(undefined)
    renderManage([], { onCreate })
    await userEvent.click(screen.getByRole('button', { name: '+ Add Task' }))
    await userEvent.type(screen.getByLabelText('Title'), 'New Sub')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(onCreate).toHaveBeenCalledWith('New Sub', '', '', expect.anything(), '', '', 1)
    )
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('keeps the modal open when onCreate throws', async () => {
    const onCreate = vi.fn().mockRejectedValue(new Error('server error'))
    renderManage([], { onCreate })
    await userEvent.click(screen.getByRole('button', { name: '+ Add Task' }))
    await userEvent.type(screen.getByLabelText('Title'), 'Bad Task')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(onCreate).toHaveBeenCalled())
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  // ── Archive confirmation ───────────────────────────────────────────────────

  it('shows inline archive confirmation when Archive is clicked', async () => {
    renderManage([makeTask({ id: 1, title: 'Netflix' })])
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    expect(screen.getByText('Archive?')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Yes' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'No' })).toBeInTheDocument()
  })

  it('calls onArchive when Yes is confirmed', async () => {
    const onArchive = vi.fn()
    renderManage([makeTask({ id: 42, title: 'Netflix' })], { onArchive })
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    expect(onArchive).toHaveBeenCalledWith(42)
  })

  it('dismisses confirmation without archiving when No is clicked', async () => {
    const onArchive = vi.fn()
    renderManage([makeTask({ id: 1, title: 'Netflix' })], { onArchive })
    await userEvent.click(screen.getByRole('button', { name: 'Archive' }))
    await userEvent.click(screen.getByRole('button', { name: 'No' }))
    expect(onArchive).not.toHaveBeenCalled()
    expect(screen.queryByText('Archive?')).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument()
  })

  it('only shows confirmation for the clicked task when multiple tasks exist', async () => {
    const tasks = [
      makeTask({ id: 1, title: 'Netflix' }),
      makeTask({ id: 2, title: 'Spotify' }),
    ]
    renderManage(tasks)
    const archiveButtons = screen.getAllByRole('button', { name: 'Archive' })
    await userEvent.click(archiveButtons[0])
    // Only one confirmation appears
    expect(screen.getAllByText('Archive?')).toHaveLength(1)
    // Second task still shows Edit/Archive
    const secondItem = screen.getByText('Spotify').closest('li')
    expect(within(secondItem).getByRole('button', { name: 'Archive' })).toBeInTheDocument()
  })
})

import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import TaskList from '../components/TaskList.jsx'

const TASK = { id: 1, title: 'Rent', description: '', type: 'payment', metadata: { amount: '500' }, interval: 1 }
const TASK_NO_TYPE = { id: 2, title: 'Check email', description: '', type: '', metadata: {}, interval: 1 }

function renderList(tasks, completionMap = new Map(), overrides = {}) {
  const props = {
    tasks,
    completionMap,
    currency: '$',
    uploadingTaskId: null,
    onToggle: vi.fn(),
    onSkip: vi.fn(),
    onUploadReceipt: vi.fn(),
    onRemoveReceipt: vi.fn(),
    onUpdateCompletion: vi.fn(),
    onGoToManage: vi.fn(),
    ...overrides,
  }
  render(<TaskList {...props} />)
  return props
}

describe('TaskList', () => {
  describe('empty state', () => {
    it('shows empty message when tasks array is empty', () => {
      renderList([])
      expect(screen.getByText('No tasks yet.')).toBeInTheDocument()
    })

    it('shows "Add Tasks" button in empty state', () => {
      const props = renderList([])
      expect(screen.getByRole('button', { name: '+ Add Tasks' })).toBeInTheDocument()
    })

    it('calls onGoToManage when "Add Tasks" clicked', async () => {
      const props = renderList([])
      await userEvent.click(screen.getByRole('button', { name: '+ Add Tasks' }))
      expect(props.onGoToManage).toHaveBeenCalledOnce()
    })
  })

  describe('task rendering', () => {
    it('renders task title', () => {
      renderList([TASK])
      expect(screen.getByText('Rent')).toBeInTheDocument()
    })

    it('renders type badge', () => {
      renderList([TASK])
      expect(screen.getByText('payment')).toBeInTheDocument()
    })

    it('renders description when present', () => {
      const task = { ...TASK, description: 'Due 1st' }
      renderList([task])
      expect(screen.getByText('Due 1st')).toBeInTheDocument()
    })

    it('does not show checkbox checkmark when incomplete', () => {
      renderList([TASK])
      const item = screen.getByRole('button', { name: /Mark.*Rent/ })
      expect(item).toHaveAttribute('aria-pressed', 'false')
    })

    it('shows checkmark and aria-pressed=true when complete', () => {
      const map = new Map([[1, { task_id: 1, month: '2026-04', receipt_file: '', amount: '' }]])
      renderList([TASK], map)
      expect(screen.getByRole('button', { name: /Rent/ })).toHaveAttribute('aria-pressed', 'true')
    })
  })

  describe('toggle', () => {
    it('calls onToggle when incomplete task clicked', async () => {
      const props = renderList([TASK])
      await userEvent.click(screen.getByRole('button', { name: /Mark.*Rent/ }))
      expect(props.onToggle).toHaveBeenCalledWith(1)
    })

    it('calls onToggle when incomplete task activated with Enter', async () => {
      const props = renderList([TASK])
      screen.getByRole('button', { name: /Mark.*Rent/ }).focus()
      await userEvent.keyboard('{Enter}')
      expect(props.onToggle).toHaveBeenCalledWith(1)
    })

    it('calls onToggle immediately when done task has no receipt', async () => {
      const map = new Map([[1, { task_id: 1, month: '2026-04', receipt_file: '', amount: '' }]])
      const props = renderList([TASK], map)
      await userEvent.click(screen.getByRole('button', { name: /Rent/ }))
      expect(props.onToggle).toHaveBeenCalledWith(1)
    })

    it('shows confirm dialog instead of toggling when done task has a receipt', async () => {
      const map = new Map([[1, { task_id: 1, month: '2026-04', receipt_file: 'uuid.pdf', amount: '' }]])
      const props = renderList([TASK], map)
      await userEvent.click(screen.getByRole('button', { name: /Rent/ }))
      expect(props.onToggle).not.toHaveBeenCalled()
      expect(screen.getByText('Removes attached receipt.')).toBeInTheDocument()
    })

    it('calls onToggle after confirming unmark with receipt', async () => {
      const map = new Map([[1, { task_id: 1, month: '2026-04', receipt_file: 'uuid.pdf', amount: '' }]])
      const props = renderList([TASK], map)
      await userEvent.click(screen.getByRole('button', { name: /Rent/ }))
      await userEvent.click(screen.getByRole('button', { name: 'Unmark' }))
      expect(props.onToggle).toHaveBeenCalledWith(1)
    })

    it('dismisses confirm dialog on Cancel', async () => {
      const map = new Map([[1, { task_id: 1, month: '2026-04', receipt_file: 'uuid.pdf', amount: '' }]])
      const props = renderList([TASK], map)
      await userEvent.click(screen.getByRole('button', { name: /Rent/ }))
      await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
      expect(screen.queryByText('Removes attached receipt.')).not.toBeInTheDocument()
      expect(props.onToggle).not.toHaveBeenCalled()
    })
  })

  describe('PaymentSlot amount editing', () => {
    const completion = { task_id: 1, month: '2026-04', receipt_file: '', amount: '' }

    it('shows default amount from task metadata when done', () => {
      const map = new Map([[1, completion]])
      renderList([TASK], map)
      expect(screen.getByText('$500')).toBeInTheDocument()
    })

    it('shows overridden amount with "custom" badge when completion has amount', () => {
      const map = new Map([[1, { ...completion, amount: '450' }]])
      renderList([TASK], map)
      expect(screen.getByText('$450')).toBeInTheDocument()
      expect(screen.getByText('custom')).toBeInTheDocument()
    })

    it('amount button is disabled when task is not done', () => {
      renderList([TASK])
      expect(screen.getByTitle('Mark task as done to edit amount')).toBeDisabled()
    })

    it('enters edit mode on amount click when done', async () => {
      const map = new Map([[1, completion]])
      renderList([TASK], map)
      await userEvent.click(screen.getByText('$500'))
      expect(screen.getByRole('textbox')).toBeInTheDocument()
    })

    it('shows error for invalid amount in edit mode', async () => {
      const map = new Map([[1, completion]])
      renderList([TASK], map)
      await userEvent.click(screen.getByText('$500'))
      await userEvent.clear(screen.getByRole('textbox'))
      await userEvent.type(screen.getByRole('textbox'), 'xyz')
      await userEvent.click(screen.getByTitle('Save'))
      expect(screen.getByRole('alert')).toHaveTextContent('Enter a valid number')
    })

    it('calls onUpdateCompletion with new amount on save', async () => {
      const map = new Map([[1, completion]])
      const props = renderList([TASK], map)
      await userEvent.click(screen.getByText('$500'))
      await userEvent.clear(screen.getByRole('textbox'))
      await userEvent.type(screen.getByRole('textbox'), '450')
      await userEvent.click(screen.getByTitle('Save'))
      expect(props.onUpdateCompletion).toHaveBeenCalledWith(1, { amount: '450' })
    })

    it('cancels edit on Escape', async () => {
      const map = new Map([[1, completion]])
      renderList([TASK], map)
      await userEvent.click(screen.getByText('$500'))
      await userEvent.keyboard('{Escape}')
      expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    })
  })
})

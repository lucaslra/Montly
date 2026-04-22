import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import TaskForm from '../components/TaskForm.jsx'

function renderForm(props = {}) {
  const onSave = props.onSave ?? vi.fn().mockResolvedValue()
  const onClose = props.onClose ?? vi.fn()
  render(<TaskForm {...props} onSave={onSave} onClose={onClose} />)
  return { onSave, onClose }
}

describe('TaskForm', () => {
  describe('rendering', () => {
    it('shows "New Task" heading when no task prop', () => {
      renderForm()
      expect(screen.getByRole('heading', { name: 'New Task' })).toBeInTheDocument()
    })

    it('shows "Edit Task" heading when task prop is provided', () => {
      renderForm({ task: { title: 'Rent', description: '', type: '', interval: 1 } })
      expect(screen.getByRole('heading', { name: 'Edit Task' })).toBeInTheDocument()
    })

    it('pre-fills fields from task prop', () => {
      renderForm({ task: { title: 'Rent', description: 'Monthly', type: 'payment', interval: 1, metadata: { amount: '500' }, start_date: '', end_date: '' } })
      expect(screen.getByLabelText('Title')).toHaveValue('Rent')
      expect(screen.getByLabelText(/Description/)).toHaveValue('Monthly')
    })

    it('hides amount field when type is reminder', async () => {
      renderForm()
      await userEvent.selectOptions(screen.getByLabelText(/Type/), 'reminder')
      expect(screen.queryByLabelText(/Amount/)).not.toBeInTheDocument()
    })

    it('shows amount field when type is payment', async () => {
      renderForm()
      await userEvent.selectOptions(screen.getByLabelText(/Type/), 'payment')
      expect(screen.getByLabelText(/Amount/)).toBeInTheDocument()
    })

    it('shows amount field when type is subscription', async () => {
      renderForm()
      await userEvent.selectOptions(screen.getByLabelText(/Type/), 'subscription')
      expect(screen.getByLabelText(/Amount/)).toBeInTheDocument()
    })

    it('shows amount field when type is bill', async () => {
      renderForm()
      await userEvent.selectOptions(screen.getByLabelText(/Type/), 'bill')
      expect(screen.getByLabelText(/Amount/)).toBeInTheDocument()
    })
  })

  describe('amount validation', () => {
    beforeEach(async () => {
      renderForm()
      await userEvent.selectOptions(screen.getByLabelText(/Type/), 'payment')
    })

    it('does not show error on empty amount', async () => {
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })

    it('does not show error on valid numeric amount', async () => {
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.type(screen.getByLabelText(/Amount/), '99.99')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(screen.queryByText('Amount must be a number')).not.toBeInTheDocument()
    })

    it('shows error on non-numeric amount', async () => {
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.type(screen.getByLabelText(/Amount/), 'abc')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(screen.getByRole('alert')).toHaveTextContent('Amount must be a number')
    })

    it('clears amount error when user edits the field', async () => {
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.type(screen.getByLabelText(/Amount/), 'abc')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(screen.getByRole('alert')).toBeInTheDocument()
      await userEvent.clear(screen.getByLabelText(/Amount/))
      await userEvent.type(screen.getByLabelText(/Amount/), '1')
      expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    })
  })

  describe('form submission', () => {
    it('does not call onSave when title is empty', async () => {
      const { onSave } = renderForm()
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(onSave).not.toHaveBeenCalled()
    })

    it('calls onSave with trimmed title', async () => {
      const { onSave } = renderForm()
      await userEvent.type(screen.getByLabelText('Title'), '  Rent  ')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(onSave).toHaveBeenCalled())
      expect(onSave.mock.calls[0][0]).toBe('Rent')
    })

    it('calls onSave with correct interval', async () => {
      const { onSave } = renderForm()
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.selectOptions(screen.getByLabelText('Recurrence'), '3')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      await waitFor(() => expect(onSave).toHaveBeenCalled())
      expect(onSave.mock.calls[0][6]).toBe(3)
    })

    it('disables Save button while saving', async () => {
      let resolve
      const onSave = vi.fn(() => new Promise(r => { resolve = r }))
      render(<TaskForm onSave={onSave} onClose={vi.fn()} />)
      await userEvent.type(screen.getByLabelText('Title'), 'Rent')
      await userEvent.click(screen.getByRole('button', { name: 'Save' }))
      expect(screen.getByRole('button', { name: 'Saving…' })).toBeDisabled()
      resolve()
    })
  })

  describe('cancel', () => {
    it('calls onClose when Cancel button clicked', async () => {
      const { onClose } = renderForm()
      await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
      expect(onClose).toHaveBeenCalledOnce()
    })

    it('calls onClose on Escape key', async () => {
      const { onClose } = renderForm()
      await userEvent.keyboard('{Escape}')
      expect(onClose).toHaveBeenCalledOnce()
    })
  })
})

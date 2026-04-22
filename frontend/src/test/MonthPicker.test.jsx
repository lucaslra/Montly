import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MonthPicker } from '../components/TaskForm.jsx'

describe('MonthPicker (popover mode)', () => {
  it('shows placeholder when no value', () => {
    render(<MonthPicker value="" onChange={vi.fn()} />)
    expect(screen.getByText('Select month')).toBeInTheDocument()
  })

  it('shows formatted value when set', () => {
    render(<MonthPicker value="2026-04" onChange={vi.fn()} />)
    expect(screen.getByText('Apr 2026')).toBeInTheDocument()
  })

  it('opens panel on trigger click', async () => {
    render(<MonthPicker value="" onChange={vi.fn()} />)
    await userEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Jan')).toBeInTheDocument()
  })

  it('closes panel on Escape', async () => {
    render(<MonthPicker value="" onChange={vi.fn()} />)
    await userEvent.click(screen.getByRole('button'))
    expect(screen.getByText('Jan')).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByText('Jan')).not.toBeInTheDocument()
  })

  it('calls onChange with correct value when month selected', async () => {
    const onChange = vi.fn()
    render(<MonthPicker value="" onChange={onChange} />)
    await userEvent.click(screen.getByRole('button'))
    const currentYear = new Date().getFullYear()
    await userEvent.click(screen.getByText('Apr'))
    expect(onChange).toHaveBeenCalledWith(`${currentYear}-04`)
  })

  it('navigates to previous year', async () => {
    render(<MonthPicker value="2026-01" onChange={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /Jan 2026/ }))
    expect(screen.getByText('2026')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Previous year' }))
    expect(screen.getByText('2025')).toBeInTheDocument()
  })

  it('navigates to next year', async () => {
    render(<MonthPicker value="2026-01" onChange={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /Jan 2026/ }))
    await userEvent.click(screen.getByRole('button', { name: 'Next year' }))
    expect(screen.getByText('2027')).toBeInTheDocument()
  })

  it('shows Clear button when value is set', async () => {
    render(<MonthPicker value="2026-04" onChange={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: /Apr 2026/ }))
    expect(screen.getByText('Clear')).toBeInTheDocument()
  })

  it('calls onChange with empty string on Clear', async () => {
    const onChange = vi.fn()
    render(<MonthPicker value="2026-04" onChange={onChange} />)
    await userEvent.click(screen.getByRole('button', { name: /Apr 2026/ }))
    await userEvent.click(screen.getByText('Clear'))
    expect(onChange).toHaveBeenCalledWith('')
  })
})

describe('MonthPicker (inline mode)', () => {
  it('renders grid immediately without a trigger button', () => {
    render(<MonthPicker value="" onChange={vi.fn()} inline />)
    expect(screen.getByText('Jan')).toBeInTheDocument()
    expect(screen.queryByText('Select month')).not.toBeInTheDocument()
  })

  it('calls onChange when month clicked', async () => {
    const onChange = vi.fn()
    render(<MonthPicker value="" onChange={onChange} inline />)
    const currentYear = new Date().getFullYear()
    await userEvent.click(screen.getByText('Jul'))
    expect(onChange).toHaveBeenCalledWith(`${currentYear}-07`)
  })
})

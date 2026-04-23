import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SetupView from '../components/SetupView.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  setupAdmin: vi.fn(),
}))

describe('SetupView', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  // ── Rendering ──────────────────────────────────────────────────────────────

  it('renders username, password, and confirm fields', () => {
    render(<SetupView onComplete={vi.fn()} />)
    expect(screen.getByLabelText('Username')).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
    expect(screen.getByLabelText('Confirm password')).toBeInTheDocument()
  })

  it('renders the Create account button', () => {
    render(<SetupView onComplete={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Create account' })).toBeInTheDocument()
  })

  it('renders Show passwords checkbox', () => {
    render(<SetupView onComplete={vi.fn()} />)
    expect(screen.getByLabelText('Show passwords')).toBeInTheDocument()
  })

  // ── Show/hide password toggle ──────────────────────────────────────────────

  it('password fields default to type="password"', () => {
    render(<SetupView onComplete={vi.fn()} />)
    expect(screen.getByLabelText('Password')).toHaveAttribute('type', 'password')
    expect(screen.getByLabelText('Confirm password')).toHaveAttribute('type', 'password')
  })

  it('shows passwords as text when Show passwords is checked', async () => {
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.click(screen.getByLabelText('Show passwords'))
    expect(screen.getByLabelText('Password')).toHaveAttribute('type', 'text')
    expect(screen.getByLabelText('Confirm password')).toHaveAttribute('type', 'text')
  })

  // ── Client-side validation ─────────────────────────────────────────────────

  it('shows error when passwords do not match', async () => {
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'password1')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'password2')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Passwords do not match')
    expect(api.setupAdmin).not.toHaveBeenCalled()
  })

  it('shows error when password is shorter than 8 characters', async () => {
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'short')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Password must be at least 8 characters')
    expect(api.setupAdmin).not.toHaveBeenCalled()
  })

  // ── Successful submission ──────────────────────────────────────────────────

  it('calls setupAdmin with trimmed username and password', async () => {
    const user = { user_id: 1, username: 'admin', is_admin: true }
    api.setupAdmin.mockResolvedValue(user)
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), '  admin  ')
    await userEvent.type(screen.getByLabelText('Password'), 'securepass')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'securepass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() => expect(api.setupAdmin).toHaveBeenCalledWith('admin', 'securepass'))
  })

  it('calls onComplete with the returned user on success', async () => {
    const user = { user_id: 1, username: 'admin', is_admin: true }
    api.setupAdmin.mockResolvedValue(user)
    const onComplete = vi.fn()
    render(<SetupView onComplete={onComplete} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'securepass')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'securepass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() => expect(onComplete).toHaveBeenCalledWith(user))
  })

  // ── Loading state ──────────────────────────────────────────────────────────

  it('disables the button and shows "Creating account…" while submitting', async () => {
    let resolve
    api.setupAdmin.mockReturnValue(new Promise(r => { resolve = r }))
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'securepass')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'securepass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    expect(screen.getByRole('button', { name: 'Creating account…' })).toBeDisabled()
    resolve({ user_id: 1 })
  })

  it('re-enables the button after a failed submission', async () => {
    api.setupAdmin.mockRejectedValue(new Error('username taken'))
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'securepass')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'securepass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Create account' })).not.toBeDisabled()
    )
  })

  // ── API error handling ─────────────────────────────────────────────────────

  it('shows the API error message on failure', async () => {
    api.setupAdmin.mockRejectedValue(new Error('username already exists'))
    render(<SetupView onComplete={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'securepass')
    await userEvent.type(screen.getByLabelText('Confirm password'), 'securepass')
    await userEvent.click(screen.getByRole('button', { name: 'Create account' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('username already exists')
    )
  })
})

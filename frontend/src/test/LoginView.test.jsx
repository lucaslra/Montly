import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import LoginView from '../components/LoginView.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  login: vi.fn(),
}))

describe('LoginView', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => vi.clearAllMocks())

  it('renders username and password fields', () => {
    render(<LoginView onLogin={vi.fn()} />)
    expect(screen.getByLabelText('Username')).toBeInTheDocument()
    expect(screen.getByLabelText('Password')).toBeInTheDocument()
  })

  it('calls login API with entered credentials', async () => {
    api.login.mockResolvedValue({ id: 1, username: 'admin', is_admin: true })
    render(<LoginView onLogin={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'secret')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(api.login).toHaveBeenCalledWith('admin', 'secret')
  })

  it('calls onLogin with user on success', async () => {
    const user = { id: 1, username: 'admin', is_admin: true }
    api.login.mockResolvedValue(user)
    const onLogin = vi.fn()
    render(<LoginView onLogin={onLogin} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'secret')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() => expect(onLogin).toHaveBeenCalledWith(user))
  })

  it('shows error message on failed login', async () => {
    api.login.mockRejectedValue(new Error('invalid credentials'))
    render(<LoginView onLogin={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'wrong')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('invalid credentials')
    )
  })

  it('disables button while submitting', async () => {
    let resolve
    api.login.mockReturnValue(new Promise(r => { resolve = r }))
    render(<LoginView onLogin={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'secret')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(screen.getByRole('button', { name: 'Signing in…' })).toBeDisabled()
    resolve({ id: 1 })
  })

  it('re-enables button after failed login', async () => {
    api.login.mockRejectedValue(new Error('fail'))
    render(<LoginView onLogin={vi.fn()} />)
    await userEvent.type(screen.getByLabelText('Username'), 'admin')
    await userEvent.type(screen.getByLabelText('Password'), 'bad')
    await userEvent.click(screen.getByRole('button', { name: 'Sign in' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Sign in' })).not.toBeDisabled()
    )
  })
})

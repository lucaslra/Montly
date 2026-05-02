import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SettingsView from '../components/SettingsView.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  changePassword:  vi.fn(),
  fetchTokens:     vi.fn(),
  createToken:     vi.fn(),
  revokeToken:     vi.fn(),
  fetchUsers:      vi.fn(),
  createUser:      vi.fn(),
  deleteUser:      vi.fn(),
  fetchWebhooks:   vi.fn(),
  createWebhook:   vi.fn(),
  deleteWebhook:   vi.fn(),
  testWebhook:     vi.fn(),
  fetchAuditLogs:  vi.fn(),
}))

const defaultSettings = {
  currency: '€', date_format: 'long', color_mode: 'system',
  task_sort: 'type', completed_last: 'false', fiscal_year_start: '1', number_format: 'en',
}

const adminUser  = { id: 1, username: 'admin',  is_admin: true  }
const regularUser = { id: 2, username: 'alice', is_admin: false }

beforeEach(() => {
  // Auto-fetched sub-sections — default to empty so renders don't hang
  api.fetchTokens.mockResolvedValue([])
  api.fetchWebhooks.mockResolvedValue([])
  api.fetchUsers.mockResolvedValue([])
  api.fetchAuditLogs.mockResolvedValue({ logs: [], total: 0 })
})

afterEach(() => vi.clearAllMocks())

// ── Main settings form ────────────────────────────────────────────────────────

describe('SettingsView main form', () => {
  it('pre-fills all selects from the settings prop', () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    expect(screen.getByLabelText('Currency')).toHaveValue('€')
    expect(screen.getByLabelText('Appearance')).toHaveValue('system')
    expect(screen.getByLabelText('Task order')).toHaveValue('type')
    expect(screen.getByLabelText('Number format')).toHaveValue('en')
  })

  it('disables the Save button when no changes have been made', () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('enables the Save button after a field is changed', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Currency'), '$')
    expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled()
  })

  it('shows "Unsaved changes" indicator when dirty', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Currency'), '$')
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument()
  })

  it('calls onSave with all current field values on submit', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<SettingsView settings={defaultSettings} onSave={onSave} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Currency'), '$')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
        currency: '$',
        date_format: 'long',
        color_mode: 'system',
        number_format: 'en',
      }))
    )
  })

  it('shows an error banner when onSave rejects', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('network error'))
    render(<SettingsView settings={defaultSettings} onSave={onSave} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Currency'), '$')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByText('network error')).toBeInTheDocument()
    )
  })
})

// ── PasswordSection ───────────────────────────────────────────────────────────

describe('SettingsView PasswordSection', () => {
  it('shows a validation error when the new passwords do not match', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.type(screen.getByLabelText('Current password'), 'oldpass1')
    await userEvent.type(screen.getByLabelText('New password'), 'newpass1')
    await userEvent.type(screen.getByLabelText('Confirm new password'), 'newpass2')
    await userEvent.click(screen.getByRole('button', { name: 'Update password' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('New passwords do not match')
    )
    expect(api.changePassword).not.toHaveBeenCalled()
  })

  it('shows a validation error when the new password is too short', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.type(screen.getByLabelText('Current password'), 'oldpass1')
    await userEvent.type(screen.getByLabelText('New password'), 'short')
    await userEvent.type(screen.getByLabelText('Confirm new password'), 'short')
    await userEvent.click(screen.getByRole('button', { name: 'Update password' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('at least 8 characters')
    )
    expect(api.changePassword).not.toHaveBeenCalled()
  })

  it('shows a success message after a successful password change', async () => {
    api.changePassword.mockResolvedValue(undefined)
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.type(screen.getByLabelText('Current password'), 'oldpass1')
    await userEvent.type(screen.getByLabelText('New password'), 'newpass12')
    await userEvent.type(screen.getByLabelText('Confirm new password'), 'newpass12')
    await userEvent.click(screen.getByRole('button', { name: 'Update password' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Password updated.')
    )
  })

  it('shows an API error when changePassword rejects', async () => {
    api.changePassword.mockRejectedValue(new Error('incorrect current password'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await userEvent.type(screen.getByLabelText('Current password'), 'wrong')
    await userEvent.type(screen.getByLabelText('New password'), 'newpass12')
    await userEvent.type(screen.getByLabelText('Confirm new password'), 'newpass12')
    await userEvent.click(screen.getByRole('button', { name: 'Update password' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('incorrect current password')
    )
  })
})

// ── TokensSection ─────────────────────────────────────────────────────────────

describe('SettingsView TokensSection', () => {
  it('shows "No tokens yet" when fetchTokens returns an empty array', async () => {
    api.fetchTokens.mockResolvedValue([])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() =>
      expect(screen.getByText('No tokens yet.')).toBeInTheDocument()
    )
  })

  it('renders the list of existing tokens', async () => {
    api.fetchTokens.mockResolvedValue([
      { id: 1, name: 'ci', created_at: '2026-01-01', last_used_at: null },
    ])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => expect(screen.getByText('ci')).toBeInTheDocument())
  })

  it('creates a token and reveals the plaintext', async () => {
    api.fetchTokens.mockResolvedValue([])
    api.createToken.mockResolvedValue({
      token: { id: 2, name: 'deploy', created_at: '2026-04-01', last_used_at: null },
      plaintext: 'mnt_supersecrettoken',
    })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('No tokens yet.'))
    await userEvent.type(screen.getByLabelText('Token name (optional)'), 'deploy')
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }))
    await waitFor(() =>
      expect(screen.getByText('mnt_supersecrettoken')).toBeInTheDocument()
    )
    expect(screen.getByText(/Copy this token now/)).toBeInTheDocument()
  })

  it('shows revoke confirmation and calls revokeToken on Yes', async () => {
    api.fetchTokens.mockResolvedValue([
      { id: 3, name: 'old-ci', created_at: '2026-01-01', last_used_at: null },
    ])
    api.revokeToken.mockResolvedValue(undefined)
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('old-ci'))
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }))
    expect(screen.getByText('Revoke?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => expect(api.revokeToken).toHaveBeenCalledWith(3))
    await waitFor(() => expect(screen.queryByText('old-ci')).not.toBeInTheDocument())
  })
})

// ── WebhooksSection ───────────────────────────────────────────────────────────

describe('SettingsView WebhooksSection', () => {
  it('shows "No webhooks yet" when fetchWebhooks returns an empty array', async () => {
    api.fetchWebhooks.mockResolvedValue([])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() =>
      expect(screen.getByText('No webhooks yet.')).toBeInTheDocument()
    )
  })

  it('shows validation error when no events are selected', async () => {
    api.fetchWebhooks.mockResolvedValue([])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    // Uncheck both event checkboxes (both are checked by default)
    await userEvent.click(screen.getByLabelText('Task completed'))
    await userEvent.click(screen.getByLabelText('Task uncompleted'))
    await userEvent.type(screen.getByLabelText('URL'), 'https://example.com/hook')
    await userEvent.click(screen.getByRole('button', { name: 'Create webhook' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('Select at least one event')
    )
    expect(api.createWebhook).not.toHaveBeenCalled()
  })

  it('creates a webhook and adds it to the list', async () => {
    const hook = { id: 1, url: 'https://example.com/hook', events: 'task.completed', created_at: '2026-04-01' }
    api.fetchWebhooks.mockResolvedValue([])
    api.createWebhook.mockResolvedValue(hook)
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('No webhooks yet.'))
    await userEvent.type(screen.getByLabelText('URL'), 'https://example.com/hook')
    await userEvent.click(screen.getByRole('button', { name: 'Create webhook' }))
    await waitFor(() =>
      expect(screen.getByText('https://example.com/hook')).toBeInTheDocument()
    )
  })

  it('renders a Test button for each webhook', async () => {
    const hook = { id: 1, url: 'https://example.com/hook', events: 'task.completed', created_at: '2026-04-01' }
    api.fetchWebhooks.mockResolvedValue([hook])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('https://example.com/hook'))
    expect(screen.getByRole('button', { name: 'Test' })).toBeInTheDocument()
  })

  it('shows "Test delivered" after a successful test', async () => {
    const hook = { id: 1, url: 'https://example.com/hook', events: 'task.completed', created_at: '2026-04-01' }
    api.fetchWebhooks.mockResolvedValue([hook])
    api.testWebhook.mockResolvedValue({ ok: true, status: 200 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByRole('button', { name: 'Test' }))
    await userEvent.click(screen.getByRole('button', { name: 'Test' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Test delivered (HTTP 200)')
    )
  })

  it('shows "Test failed" after a failed test response', async () => {
    const hook = { id: 1, url: 'https://example.com/hook', events: 'task.completed', created_at: '2026-04-01' }
    api.fetchWebhooks.mockResolvedValue([hook])
    api.testWebhook.mockResolvedValue({ ok: false, status: 500 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByRole('button', { name: 'Test' }))
    await userEvent.click(screen.getByRole('button', { name: 'Test' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Test failed: HTTP 500')
    )
  })

  it('shows an error message when the test throws', async () => {
    const hook = { id: 1, url: 'https://example.com/hook', events: 'task.completed', created_at: '2026-04-01' }
    api.fetchWebhooks.mockResolvedValue([hook])
    api.testWebhook.mockRejectedValue(new Error('connection refused'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByRole('button', { name: 'Test' }))
    await userEvent.click(screen.getByRole('button', { name: 'Test' }))
    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent('Test failed: connection refused')
    )
  })
})

// ── AuditLogSection ───────────────────────────────────────────────────────────

describe('SettingsView AuditLogSection', () => {
  const makeLog = (id) => ({
    id,
    created_at: '2026-04-01T12:00:00Z',
    username: 'admin',
    action: 'complete',
    entity_type: 'task',
    entity_label: `Task ${id}`,
  })

  it('shows pagination controls when there are multiple pages', async () => {
    const logs = Array.from({ length: 50 }, (_, i) => makeLog(i + 1))
    api.fetchAuditLogs.mockResolvedValue({ logs, total: 100 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => screen.getByText(/Page 1 of 2/))
    expect(screen.getByRole('button', { name: '‹ Prev' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next ›' })).not.toBeDisabled()
  })

  it('keeps pagination visible when navigating to the next page', async () => {
    const logs = Array.from({ length: 50 }, (_, i) => makeLog(i + 1))
    api.fetchAuditLogs
      .mockResolvedValueOnce({ logs, total: 100 })
      .mockResolvedValueOnce({ logs, total: 100 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => screen.getByText(/Page 1 of 2/))
    await userEvent.click(screen.getByRole('button', { name: 'Next ›' }))
    await waitFor(() => screen.getByText(/Page 2 of 2/))
    expect(screen.getByRole('button', { name: 'Next ›' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '‹ Prev' })).not.toBeDisabled()
  })

  it('hides pagination when there is only one page', async () => {
    const logs = Array.from({ length: 10 }, (_, i) => makeLog(i + 1))
    api.fetchAuditLogs.mockResolvedValue({ logs, total: 10 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => expect(screen.queryByRole('button', { name: '‹ Prev' })).not.toBeInTheDocument())
  })
})

// ── UsersSection (admin only) ─────────────────────────────────────────────────

describe('SettingsView UsersSection', () => {
  it('is not rendered for non-admin users', () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    expect(screen.queryByRole('heading', { name: 'Users' })).not.toBeInTheDocument()
  })

  it('is rendered for admin users', async () => {
    api.fetchUsers.mockResolvedValue([adminUser])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Users' })).toBeInTheDocument()
    )
  })

  it('renders the list of users fetched on mount', async () => {
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true,  created_at: '2026-01-01' },
      { id: 2, username: 'alice', is_admin: false, created_at: '2026-02-01' },
    ])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => expect(screen.getByText('alice')).toBeInTheDocument())
    expect(screen.getAllByText('admin')[0]).toBeInTheDocument()
  })

  it('does not show a Delete button for the current user', async () => {
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true, created_at: '2026-01-01' },
      { id: 2, username: 'alice', is_admin: false, created_at: '2026-02-01' },
    ])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => screen.getByText('alice'))
    // Only alice's row should have a Delete button (admin is currentUserId=1)
    expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(1)
  })

  it('creates a new user and appends them to the list', async () => {
    const newUser = { id: 3, username: 'bob', is_admin: false, created_at: '2026-05-01' }
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true, created_at: '2026-01-01' },
    ])
    api.createUser.mockResolvedValue(newUser)
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    // Navigate to Users tab first
    await userEvent.click(screen.getByRole('tab', { name: 'Users' }))
    // Wait for users list to load
    await waitFor(() => screen.getAllByText('admin'))
    // The Users panel is now active; use within to scope the query
    const usersPanel = screen.getByRole('tabpanel', { name: 'Users' })
    await userEvent.type(within(usersPanel).getByLabelText('Username'), 'bob')
    await userEvent.type(within(usersPanel).getByLabelText('Password'), 'bobpassword1')
    await userEvent.click(within(usersPanel).getByRole('button', { name: 'Create user' }))
    await waitFor(() => expect(api.createUser).toHaveBeenCalledWith('bob', 'bobpassword1', false))
    await waitFor(() => expect(screen.getByText('bob')).toBeInTheDocument())
  })

  it('shows an error when createUser fails', async () => {
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true, created_at: '2026-01-01' },
    ])
    api.createUser.mockRejectedValue(new Error('username already taken'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    // Navigate to Users tab first
    await userEvent.click(screen.getByRole('tab', { name: 'Users' }))
    // Wait for users list to load (use getAllByText since "admin" appears as username + badge)
    await waitFor(() => screen.getAllByText('admin'))
    const usersPanel = screen.getByRole('tabpanel', { name: 'Users' })
    await userEvent.type(within(usersPanel).getByLabelText('Username'), 'duplicateuser')
    await userEvent.type(within(usersPanel).getByLabelText('Password'), 'password123')
    await userEvent.click(within(usersPanel).getByRole('button', { name: 'Create user' }))
    await waitFor(() =>
      expect(screen.getByText('username already taken')).toBeInTheDocument()
    )
  })

  it('shows delete confirmation and cancels with No', async () => {
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true, created_at: '2026-01-01' },
      { id: 2, username: 'alice', is_admin: false, created_at: '2026-02-01' },
    ])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => screen.getByText('alice'))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'No' }))
    expect(screen.queryByText('Delete?')).not.toBeInTheDocument()
    expect(api.deleteUser).not.toHaveBeenCalled()
    expect(screen.getByText('alice')).toBeInTheDocument()
  })

  it('shows an error when deleteUser fails', async () => {
    api.fetchUsers.mockResolvedValue([
      { id: 1, username: 'admin', is_admin: true, created_at: '2026-01-01' },
      { id: 2, username: 'alice', is_admin: false, created_at: '2026-02-01' },
    ])
    api.deleteUser.mockRejectedValue(new Error('cannot delete last admin'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() => screen.getByText('alice'))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByText('cannot delete last admin')).toBeInTheDocument()
    )
  })
})

// ── Tab switching ─────────────────────────────────────────────────────────────

describe('SettingsView tab switching', () => {
  it('switches to the API Tokens tab when clicked', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    const tokensTab = screen.getByRole('tab', { name: 'API Tokens' })
    expect(tokensTab).toHaveAttribute('aria-selected', 'false')
    await userEvent.click(tokensTab)
    expect(tokensTab).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', { name: 'Preferences' })).toHaveAttribute('aria-selected', 'false')
  })

  it('shows admin-only tabs (Users, Audit) for admin users', () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    expect(screen.getByRole('tab', { name: 'Users' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Audit' })).toBeInTheDocument()
  })

  it('does not show admin-only tabs for regular users', () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    expect(screen.queryByRole('tab', { name: 'Users' })).not.toBeInTheDocument()
    expect(screen.queryByRole('tab', { name: 'Audit' })).not.toBeInTheDocument()
  })
})

// ── Preferences: select changes, preview, and dirty tracking ──────────────────

describe('SettingsView preferences selects and preview', () => {
  it('includes all changed fields in the onSave payload', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<SettingsView settings={defaultSettings} onSave={onSave} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Appearance'), 'dark')
    await userEvent.selectOptions(screen.getByLabelText('Task order'), 'name')
    await userEvent.selectOptions(screen.getByLabelText('Completed tasks'), 'true')
    await userEvent.selectOptions(screen.getByLabelText('Fiscal year starts'), 'April')
    await userEvent.selectOptions(screen.getByLabelText('Number format'), 'eu')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(onSave).toHaveBeenCalledWith(expect.objectContaining({
        color_mode: 'dark',
        task_sort: 'name',
        completed_last: 'true',
        fiscal_year_start: '4',
        number_format: 'eu',
      }))
    )
  })

  it('updates the preview when the date format is changed', async () => {
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    // Change to ISO format — preview should show YYYY-MM style
    await userEvent.selectOptions(screen.getByLabelText('Month display'), 'iso')
    // Query the preview element directly by class rather than by text (option element also has ISO text)
    const previewEl = document.querySelector('.settings-preview-value')
    expect(previewEl).not.toBeNull()
    expect(previewEl.textContent).toMatch(/20\d\d-\d\d/)
  })

  it('dismisses the error banner when the dismiss button is clicked', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('save failed'))
    render(<SettingsView settings={defaultSettings} onSave={onSave} user={regularUser} />)
    await userEvent.selectOptions(screen.getByLabelText('Currency'), '$')
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => screen.getByText('save failed'))
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss error' }))
    expect(screen.queryByText('save failed')).not.toBeInTheDocument()
  })
})

// ── TokensSection: copy, dismiss reveal, create error, revoke error ───────────

describe('SettingsView TokensSection extended', () => {
  it('shows error when createToken fails', async () => {
    api.fetchTokens.mockResolvedValue([])
    api.createToken.mockRejectedValue(new Error('quota exceeded'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('No tokens yet.'))
    await userEvent.type(screen.getByLabelText('Token name (optional)'), 'my-token')
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }))
    await waitFor(() =>
      expect(screen.getByText('quota exceeded')).toBeInTheDocument()
    )
  })

  it('dismisses the token reveal box when Dismiss is clicked', async () => {
    api.fetchTokens.mockResolvedValue([])
    api.createToken.mockResolvedValue({
      token: { id: 5, name: 'temp', created_at: '2026-05-01', last_used_at: null },
      plaintext: 'mnt_revealedtoken',
    })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('No tokens yet.'))
    await userEvent.type(screen.getByLabelText('Token name (optional)'), 'temp')
    await userEvent.click(screen.getByRole('button', { name: 'Create token' }))
    await waitFor(() => screen.getByText('mnt_revealedtoken'))
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss' }))
    expect(screen.queryByText('mnt_revealedtoken')).not.toBeInTheDocument()
  })

  it('cancels revoke with No and keeps the token in the list', async () => {
    api.fetchTokens.mockResolvedValue([
      { id: 7, name: 'keep-me', created_at: '2026-01-01', last_used_at: null },
    ])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('keep-me'))
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }))
    expect(screen.getByText('Revoke?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'No' }))
    expect(screen.queryByText('Revoke?')).not.toBeInTheDocument()
    expect(api.revokeToken).not.toHaveBeenCalled()
    expect(screen.getByText('keep-me')).toBeInTheDocument()
  })

  it('shows error when revokeToken fails', async () => {
    api.fetchTokens.mockResolvedValue([
      { id: 8, name: 'bad-token', created_at: '2026-01-01', last_used_at: null },
    ])
    api.revokeToken.mockRejectedValue(new Error('revoke failed'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('bad-token'))
    await userEvent.click(screen.getByRole('button', { name: 'Revoke' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByText('revoke failed')).toBeInTheDocument()
    )
  })
})

// ── WebhooksSection: delete confirm, create error ─────────────────────────────

describe('SettingsView WebhooksSection extended', () => {
  const sampleHook = { id: 10, url: 'https://example.com/wh', events: 'task.completed', created_at: '2026-04-01' }

  it('deletes a webhook after confirming Yes', async () => {
    api.fetchWebhooks.mockResolvedValue([sampleHook])
    api.deleteWebhook.mockResolvedValue(undefined)
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('https://example.com/wh'))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() => expect(api.deleteWebhook).toHaveBeenCalledWith(10))
    await waitFor(() => expect(screen.queryByText('https://example.com/wh')).not.toBeInTheDocument())
  })

  it('cancels webhook delete with No', async () => {
    api.fetchWebhooks.mockResolvedValue([sampleHook])
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('https://example.com/wh'))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(screen.getByText('Delete?')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'No' }))
    expect(screen.queryByText('Delete?')).not.toBeInTheDocument()
    expect(api.deleteWebhook).not.toHaveBeenCalled()
    expect(screen.getByText('https://example.com/wh')).toBeInTheDocument()
  })

  it('shows error when createWebhook fails', async () => {
    api.fetchWebhooks.mockResolvedValue([])
    api.createWebhook.mockRejectedValue(new Error('invalid URL'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('No webhooks yet.'))
    await userEvent.type(screen.getByLabelText('URL'), 'https://example.com/fail')
    await userEvent.click(screen.getByRole('button', { name: 'Create webhook' }))
    await waitFor(() =>
      expect(screen.getByText('invalid URL')).toBeInTheDocument()
    )
  })

  it('shows error when deleteWebhook fails', async () => {
    api.fetchWebhooks.mockResolvedValue([sampleHook])
    api.deleteWebhook.mockRejectedValue(new Error('webhook not found'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={regularUser} />)
    await waitFor(() => screen.getByText('https://example.com/wh'))
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
    await userEvent.click(screen.getByRole('button', { name: 'Yes' }))
    await waitFor(() =>
      expect(screen.getByText('webhook not found')).toBeInTheDocument()
    )
  })
})

// ── AuditLogSection: error state ──────────────────────────────────────────────

describe('SettingsView AuditLogSection extended', () => {
  it('shows an error when fetchAuditLogs rejects', async () => {
    api.fetchAuditLogs.mockRejectedValue(new Error('forbidden'))
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() =>
      expect(screen.getByText('forbidden')).toBeInTheDocument()
    )
    expect(screen.queryByText('No activity recorded yet.')).not.toBeInTheDocument()
  })

  it('shows "No activity recorded yet" when logs are empty', async () => {
    api.fetchAuditLogs.mockResolvedValue({ logs: [], total: 0 })
    render(<SettingsView settings={defaultSettings} onSave={vi.fn()} user={adminUser} />)
    await waitFor(() =>
      expect(screen.getByText('No activity recorded yet.')).toBeInTheDocument()
    )
  })
})

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import {
  fetchMe, fetchSetupStatus, setupAdmin,
  login, logout,
  fetchSettings, updateSettings,
  fetchTasks, createTask, updateTask, deleteTask,
  fetchCompletions, toggleCompletion, patchCompletion,
  deleteCompletionReceipt, uploadCompletionReceipt, exportCompletionsCSV,
  changePassword,
  fetchTokens, createToken, revokeToken,
  fetchUsers, createUser, deleteUser,
  fetchWebhooks, createWebhook, deleteWebhook,
} from '../api.js'

function mockFetch(status, body) {
  global.fetch = vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
  })
}

beforeEach(() => { global.fetch = vi.fn() })
afterEach(() => { vi.restoreAllMocks() })

// ── request() base function ───────────────────────────────────────────────────

describe('api request()', () => {
  it('returns parsed JSON on 200', async () => {
    mockFetch(200, [{ id: 1 }])
    const result = await fetchTasks('2026-04')
    expect(result).toEqual([{ id: 1 }])
  })

  it('returns null on 204', async () => {
    global.fetch = vi.fn().mockResolvedValue({ status: 204, ok: true, json: () => Promise.resolve(null) })
    const result = await logout()
    expect(result).toBeNull()
  })

  it('throws with status 401 on unauthorized', async () => {
    mockFetch(401, { error: 'unauthorized' })
    const err = await login('x', 'y').catch(e => e)
    expect(err.message).toBe('unauthorized')
    expect(err.status).toBe(401)
  })

  it('throws with server error message on non-ok response', async () => {
    mockFetch(404, { error: 'task not found' })
    const err = await deleteTask(99).catch(e => e)
    expect(err.message).toBe('task not found')
  })

  it('falls back to HTTP status message when no error field', async () => {
    mockFetch(500, {})
    const err = await fetchTasks('2026-04').catch(e => e)
    expect(err.message).toBe('HTTP 500')
  })
})

// ── Auth ──────────────────────────────────────────────────────────────────────

describe('fetchMe', () => {
  it('sends GET to /auth/me', async () => {
    mockFetch(200, { user_id: 1, username: 'alice', is_admin: true })
    const result = await fetchMe()
    expect(global.fetch).toHaveBeenCalledWith('/api/auth/me', {})
    expect(result).toEqual({ user_id: 1, username: 'alice', is_admin: true })
  })
})

describe('fetchSetupStatus', () => {
  it('sends GET to /auth/setup and returns the payload', async () => {
    mockFetch(200, { needs_setup: false })
    const result = await fetchSetupStatus()
    expect(global.fetch).toHaveBeenCalledWith('/api/auth/setup', {})
    expect(result).toEqual({ needs_setup: false })
  })
})

describe('setupAdmin', () => {
  it('sends POST to /auth/setup with username and password', async () => {
    mockFetch(200, { user_id: 1, username: 'admin' })
    await setupAdmin('admin', 'secret123')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/setup')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ username: 'admin', password: 'secret123' })
  })
})

describe('login', () => {
  it('sends POST to /auth/login with credentials', async () => {
    mockFetch(200, { user_id: 1 })
    await login('alice', 'pass')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/login')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ username: 'alice', password: 'pass' })
  })
})

describe('logout', () => {
  it('sends POST to /auth/logout', async () => {
    mockFetch(204, null)
    await logout()
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/logout')
    expect(opts.method).toBe('POST')
  })
})

describe('changePassword', () => {
  it('sends PATCH to /auth/password with current and new password', async () => {
    mockFetch(200, {})
    await changePassword('old', 'new123')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/password')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ current_password: 'old', new_password: 'new123' })
  })
})

// ── Settings ──────────────────────────────────────────────────────────────────

describe('fetchSettings', () => {
  it('sends GET to /settings and returns the payload', async () => {
    mockFetch(200, { currency: 'USD', locale: 'en-US' })
    const result = await fetchSettings()
    expect(global.fetch).toHaveBeenCalledWith('/api/settings', {})
    expect(result).toEqual({ currency: 'USD', locale: 'en-US' })
  })
})

describe('updateSettings', () => {
  it('sends PUT to /settings with the settings object', async () => {
    mockFetch(200, { currency: 'EUR', locale: 'fr-FR' })
    await updateSettings({ currency: 'EUR', locale: 'fr-FR' })
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/settings')
    expect(opts.method).toBe('PUT')
    expect(JSON.parse(opts.body)).toEqual({ currency: 'EUR', locale: 'fr-FR' })
  })
})

// ── Tasks ─────────────────────────────────────────────────────────────────────

describe('fetchTasks', () => {
  it('encodes the month param in the URL', async () => {
    mockFetch(200, [])
    await fetchTasks('2026-04')
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('month=2026-04'),
      expect.anything(),
    )
  })

  it('passes signal option when provided', async () => {
    mockFetch(200, [])
    const controller = new AbortController()
    await fetchTasks('2026-04', controller.signal)
    expect(global.fetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ signal: controller.signal }),
    )
  })
})

describe('createTask', () => {
  it('sends POST to /tasks with all fields', async () => {
    mockFetch(201, { id: 10 })
    await createTask('Buy coffee', 'desc', 'expense', null, '2026-01', '2026-12', 2)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/tasks')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({
      title: 'Buy coffee',
      description: 'desc',
      type: 'expense',
      metadata: null,
      start_date: '2026-01',
      end_date: '2026-12',
      interval: 2,
    })
  })

  it('defaults start_date and end_date to empty string when null', async () => {
    mockFetch(201, { id: 11 })
    await createTask('Task', '', 'reminder', null, null, null)
    const body = JSON.parse(global.fetch.mock.calls[0][1].body)
    expect(body.start_date).toBe('')
    expect(body.end_date).toBe('')
  })

  it('defaults interval to 1 when not provided', async () => {
    mockFetch(201, { id: 12 })
    await createTask('Task', '', 'reminder', null, null, null)
    const body = JSON.parse(global.fetch.mock.calls[0][1].body)
    expect(body.interval).toBe(1)
  })
})

describe('updateTask', () => {
  it('sends PUT to /tasks/:id with all fields', async () => {
    mockFetch(200, { id: 5 })
    await updateTask(5, 'Updated', 'new desc', 'bill', '{}', '2026-03', '2026-09', 3)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/tasks/5')
    expect(opts.method).toBe('PUT')
    expect(JSON.parse(opts.body)).toEqual({
      title: 'Updated',
      description: 'new desc',
      type: 'bill',
      metadata: '{}',
      start_date: '2026-03',
      end_date: '2026-09',
      interval: 3,
    })
  })
})

describe('deleteTask', () => {
  it('sends DELETE to /tasks/:id', async () => {
    mockFetch(204, null)
    await deleteTask(7)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/tasks/7')
    expect(opts.method).toBe('DELETE')
  })
})

// ── Completions ───────────────────────────────────────────────────────────────

describe('fetchCompletions', () => {
  it('encodes the month param in the URL', async () => {
    mockFetch(200, [])
    await fetchCompletions('2026-04')
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/completions?month=2026-04'),
      expect.anything(),
    )
  })

  it('passes signal option when provided', async () => {
    mockFetch(200, [])
    const controller = new AbortController()
    await fetchCompletions('2026-04', controller.signal)
    expect(global.fetch).toHaveBeenCalledWith(
      expect.any(String),
      expect.objectContaining({ signal: controller.signal }),
    )
  })
})

describe('toggleCompletion', () => {
  it('sends task_id and month in the request body', async () => {
    mockFetch(200, { completed: true })
    await toggleCompletion(42, '2026-04')
    const body = JSON.parse(global.fetch.mock.calls[0][1].body)
    expect(body).toEqual({ task_id: 42, month: '2026-04' })
  })
})

describe('patchCompletion', () => {
  it('sends PATCH to /completions/:taskId/:month with the fields object', async () => {
    mockFetch(200, {})
    await patchCompletion(3, '2026-04', { amount: 12.5, note: 'paid' })
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/completions/3/2026-04')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ amount: 12.5, note: 'paid' })
  })
})

describe('deleteCompletionReceipt', () => {
  it('sends DELETE to /completions/:taskId/:month/receipt', async () => {
    mockFetch(204, null)
    await deleteCompletionReceipt(8, '2026-03')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/completions/8/2026-03/receipt')
    expect(opts.method).toBe('DELETE')
  })
})

describe('uploadCompletionReceipt', () => {
  it('sends POST with FormData body (no Content-Type override) and returns data', async () => {
    const data = { receipt_url: '/receipts/abc.jpg' }
    mockFetch(200, data)
    const file = new File(['img'], 'photo.jpg', { type: 'image/jpeg' })
    const result = await uploadCompletionReceipt(9, '2026-04', file)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/completions/9/2026-04/receipt')
    expect(opts.method).toBe('POST')
    expect(opts.body).toBeInstanceOf(FormData)
    expect(opts.headers).toBeUndefined()
    expect(result).toEqual(data)
  })

  it('throws when the upload fails', async () => {
    mockFetch(400, { error: 'file too large' })
    const file = new File(['x'], 'x.jpg', { type: 'image/jpeg' })
    const err = await uploadCompletionReceipt(9, '2026-04', file).catch(e => e)
    expect(err.message).toBe('file too large')
  })
})

describe('exportCompletionsCSV', () => {
  it('builds the URL with from and to params and returns the raw Response', async () => {
    const fakeResponse = { status: 200, ok: true }
    global.fetch = vi.fn().mockResolvedValue(fakeResponse)
    const result = await exportCompletionsCSV('2026-01', '2026-04')
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('from=2026-01'),
    )
    expect(global.fetch).toHaveBeenCalledWith(
      expect.stringContaining('to=2026-04'),
    )
    expect(result).toBe(fakeResponse)
  })

  it('omits params that are falsy', async () => {
    global.fetch = vi.fn().mockResolvedValue({ status: 200, ok: true })
    await exportCompletionsCSV('', '')
    const [url] = global.fetch.mock.calls[0]
    expect(url).not.toContain('from=')
    expect(url).not.toContain('to=')
    expect(url).toContain('/api/export/completions.csv')
  })
})

// ── Tokens ────────────────────────────────────────────────────────────────────

describe('fetchTokens', () => {
  it('sends GET to /auth/tokens', async () => {
    mockFetch(200, [{ id: 1, name: 'ci' }])
    const result = await fetchTokens()
    expect(global.fetch).toHaveBeenCalledWith('/api/auth/tokens', {})
    expect(result).toEqual([{ id: 1, name: 'ci' }])
  })
})

describe('createToken', () => {
  it('sends POST to /auth/tokens with name in body', async () => {
    mockFetch(201, { id: 2, name: 'deploy', token: 'tok_abc' })
    const result = await createToken('deploy')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/tokens')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ name: 'deploy' })
    expect(result.token).toBe('tok_abc')
  })
})

describe('revokeToken', () => {
  it('sends DELETE to /auth/tokens/:id', async () => {
    mockFetch(204, null)
    await revokeToken(3)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/auth/tokens/3')
    expect(opts.method).toBe('DELETE')
  })
})

// ── Users ─────────────────────────────────────────────────────────────────────

describe('fetchUsers', () => {
  it('sends GET to /users', async () => {
    mockFetch(200, [{ id: 1, username: 'alice', is_admin: true }])
    const result = await fetchUsers()
    expect(global.fetch).toHaveBeenCalledWith('/api/users', {})
    expect(result[0].username).toBe('alice')
  })
})

describe('createUser', () => {
  it('sends POST to /users with username, password, and is_admin', async () => {
    mockFetch(201, { id: 2, username: 'bob' })
    await createUser('bob', 'pass123', false)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/users')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ username: 'bob', password: 'pass123', is_admin: false })
  })
})

describe('deleteUser', () => {
  it('sends DELETE to /users/:id', async () => {
    mockFetch(204, null)
    await deleteUser(4)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/users/4')
    expect(opts.method).toBe('DELETE')
  })
})

// ── Webhooks ──────────────────────────────────────────────────────────────────

describe('fetchWebhooks', () => {
  it('sends GET to /webhooks', async () => {
    mockFetch(200, [{ id: 1, url: 'https://example.com/hook' }])
    const result = await fetchWebhooks()
    expect(global.fetch).toHaveBeenCalledWith('/api/webhooks', {})
    expect(result[0].url).toBe('https://example.com/hook')
  })
})

describe('createWebhook', () => {
  it('sends POST to /webhooks with url, events, and secret', async () => {
    mockFetch(201, { id: 5 })
    await createWebhook('https://example.com/hook', ['task.completed'], 'mysecret')
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/webhooks')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({
      url: 'https://example.com/hook',
      events: ['task.completed'],
      secret: 'mysecret',
    })
  })
})

describe('deleteWebhook', () => {
  it('sends DELETE to /webhooks/:id', async () => {
    mockFetch(204, null)
    await deleteWebhook(5)
    const [url, opts] = global.fetch.mock.calls[0]
    expect(url).toBe('/api/webhooks/5')
    expect(opts.method).toBe('DELETE')
  })
})

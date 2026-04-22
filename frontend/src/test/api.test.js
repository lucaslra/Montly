import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fetchTasks, login, logout, toggleCompletion, deleteTask } from '../api.js'

function mockFetch(status, body) {
  global.fetch = vi.fn().mockResolvedValue({
    status,
    ok: status >= 200 && status < 300,
    json: () => Promise.resolve(body),
  })
}

beforeEach(() => { global.fetch = vi.fn() })
afterEach(() => { vi.restoreAllMocks() })

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

describe('toggleCompletion', () => {
  it('sends task_id and month in the request body', async () => {
    mockFetch(200, { completed: true })
    await toggleCompletion(42, '2026-04')
    const body = JSON.parse(global.fetch.mock.calls[0][1].body)
    expect(body).toEqual({ task_id: 42, month: '2026-04' })
  })
})

const BASE = '/api'

async function request(path, options = {}) {
  const res = await fetch(BASE + path, options)
  if (res.status === 204) return null
  if (res.status === 401) {
    const err = new Error('unauthorized')
    err.status = 401
    throw err
  }
  const data = await res.json()
  if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`)
  return data
}

export const fetchMe          = () => request('/auth/me')
export const fetchSetupStatus = () => request('/auth/setup')
export const setupAdmin = (username, password) =>
  request('/auth/setup', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
export const login = (username, password) =>
  request('/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
export const logout = () => request('/auth/logout', { method: 'POST' })

export const fetchSettings = () => request('/settings')
export const updateSettings = (settings) =>
  request('/settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  })

export const fetchTasks = (month, signal) =>
  request(`/tasks?month=${encodeURIComponent(month)}`, signal ? { signal } : {})

export const createTask = (title, description, type, metadata, startDate, endDate, interval = 1) =>
  request('/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, description, type, metadata, start_date: startDate ?? '', end_date: endDate ?? '', interval }),
  })

export const updateTask = (id, title, description, type, metadata, startDate, endDate, interval = 1) =>
  request(`/tasks/${id}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title, description, type, metadata, start_date: startDate ?? '', end_date: endDate ?? '', interval }),
  })

export const deleteTask = (id) =>
  request(`/tasks/${id}`, { method: 'DELETE' })

export const fetchReport = (anchor) =>
  request(`/report?anchor=${encodeURIComponent(anchor)}`)

export const fetchCompletions = (month, signal) =>
  request(`/completions?month=${encodeURIComponent(month)}`, signal ? { signal } : {})

export const toggleCompletion = (taskId, month) =>
  request('/completions/toggle', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task_id: taskId, month }),
  })

export const skipCompletion = (taskId, month) =>
  request('/completions/skip', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ task_id: taskId, month }),
  })

export const patchCompletion = (taskId, month, fields) =>
  request(`/completions/${taskId}/${month}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(fields),
  })

export const deleteCompletionReceipt = (taskId, month) =>
  request(`/completions/${taskId}/${month}/receipt`, { method: 'DELETE' })

export async function uploadCompletionReceipt(taskId, month, file) {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`${BASE}/completions/${taskId}/${month}/receipt`, {
    method: 'POST',
    body: form,
  })
  const data = await res.json()
  if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`)
  return data
}

export const changePassword = (currentPassword, newPassword) =>
  request('/auth/password', {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  })

export const fetchTokens = () => request('/auth/tokens')
export const createToken = (name) =>
  request('/auth/tokens', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
export const revokeToken = (id) =>
  request(`/auth/tokens/${id}`, { method: 'DELETE' })

export const fetchUsers = () => request('/users')
export const createUser = (username, password, isAdmin) =>
  request('/users', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, is_admin: isAdmin }),
  })
export const deleteUser = (id) =>
  request(`/users/${id}`, { method: 'DELETE' })

export const fetchWebhooks = () => request('/webhooks')
export const createWebhook = (url, events, secret) =>
  request('/webhooks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ url, events, secret }),
  })
export const deleteWebhook = (id) =>
  request(`/webhooks/${id}`, { method: 'DELETE' })

export const testWebhook = (id) =>
  request(`/webhooks/${id}/test`, { method: 'POST' })

export const fetchAuditLogs = (limit = 50, offset = 0) =>
  request(`/audit-logs?limit=${limit}&offset=${offset}`)

export function exportCompletionsCSV(from, to) {
  const params = new URLSearchParams()
  if (from) params.set('from', from)
  if (to)   params.set('to', to)
  return fetch(`${BASE}/export/completions.csv?${params}`)
}

export async function importCompletionsCSV(file) {
  const form = new FormData()
  form.append('file', file)
  const res = await fetch(`${BASE}/import/completions.csv`, { method: 'POST', body: form })
  const data = await res.json()
  if (!res.ok) throw new Error(data.error || 'Import failed')
  return data
}

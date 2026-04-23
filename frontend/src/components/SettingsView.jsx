import { useState, useEffect, useRef } from 'react'
import {
  changePassword,
  fetchTokens, createToken, revokeToken,
  fetchUsers, createUser, deleteUser,
  fetchWebhooks, createWebhook, deleteWebhook,
} from '../api.js'
import { formatAmount } from '../utils.js'

// fix 7: grouped by region
const CURRENCY_GROUPS = [
  {
    label: 'Common',
    options: [
      { symbol: '$',  label: 'US Dollar ($)' },
      { symbol: '€',  label: 'Euro (€)' },
      { symbol: '£',  label: 'Pound (£)' },
      { symbol: '¥',  label: 'Yen / Yuan (¥)' },
    ],
  },
  {
    label: 'Other',
    options: [
      { symbol: 'Fr', label: 'Franc (Fr)' },
      { symbol: 'C$', label: 'Canadian Dollar (C$)' },
      { symbol: 'A$', label: 'Australian Dollar (A$)' },
      { symbol: 'R$', label: 'Real (R$)' },
      { symbol: '₹',  label: 'Rupee (₹)' },
      { symbol: '₩',  label: 'Won (₩)' },
    ],
  },
]

const DATE_FORMATS = [
  { value: 'long',    label: 'April 2026' },
  { value: 'short',   label: 'Apr 2026' },
  { value: 'numeric', label: '04/2026' },
  { value: 'iso',     label: '2026-04' },
]

const COLOR_MODES = [
  { value: 'system', label: 'System default' },
  { value: 'light',  label: 'Light' },
  { value: 'dark',   label: 'Dark' },
]

const TASK_SORT_OPTIONS = [
  { value: 'type',    label: 'By type' },
  { value: 'name',    label: 'Alphabetical' },
  { value: 'default', label: 'Creation order' },
]

const COMPLETED_LAST_OPTIONS = [
  { value: 'false', label: 'Mixed in (default)' },
  { value: 'true',  label: 'Completed at bottom' },
]

const FISCAL_YEAR_MONTHS = [
  'January','February','March','April','May','June',
  'July','August','September','October','November','December',
].map((label, i) => ({ value: String(i + 1), label }))

const NUMBER_FORMAT_OPTIONS = [
  { value: 'en', label: '1,234.56' },
  { value: 'eu', label: '1.234,56' },
]

// fix 5: preview formatter (mirrors App.jsx formatMonth)
function previewMonth(format) {
  const d = new Date()
  const year = d.getFullYear()
  const m = d.getMonth() + 1
  const mm = String(m).padStart(2, '0')
  switch (format) {
    case 'short':   return d.toLocaleString('default', { month: 'short', year: 'numeric' })
    case 'numeric': return `${mm}/${year}`
    case 'iso':     return `${year}-${mm}`
    default:        return d.toLocaleString('default', { month: 'long', year: 'numeric' })
  }
}

// ---------- Password section ----------

function PasswordSection() {
  const [current, setCurrent] = useState('')
  const [next, setNext]       = useState('')
  const [confirm, setConfirm] = useState('')
  const [saving, setSaving]   = useState(false)
  const [error, setError]     = useState(null)
  const [success, setSuccess] = useState(false)
  const [showPw, setShowPw]   = useState(false)
  const pwType = showPw ? 'text' : 'password'

  async function handleSubmit(e) {
    e.preventDefault()
    setError(null)
    setSuccess(false)
    if (next !== confirm) { setError('New passwords do not match'); return }
    if (next.length < 8) { setError('New password must be at least 8 characters'); return }
    setSaving(true)
    try {
      await changePassword(current, next)
      setCurrent(''); setNext(''); setConfirm('')
      setSuccess(true)
    } catch (err) {
      setError(err.message ?? 'Failed to change password')
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="settings-section-block">
      <h3 className="settings-section-title">Change Password</h3>
      {error   && <p className="form-error" role="alert">{error}</p>}
      {success && <p className="form-success" role="status">Password updated.</p>}
      <form onSubmit={handleSubmit} className="settings-sub-form">
        <div className="form-group">
          <label htmlFor="pw-current">Current password</label>
          <input id="pw-current" type={pwType} value={current} onChange={e => setCurrent(e.target.value)} autoComplete="current-password" required />
        </div>
        <div className="form-group">
          <label htmlFor="pw-new">New password</label>
          <input id="pw-new" type={pwType} value={next} onChange={e => setNext(e.target.value)} autoComplete="new-password" required />
        </div>
        <div className="form-group">
          <label htmlFor="pw-confirm">Confirm new password</label>
          <input id="pw-confirm" type={pwType} value={confirm} onChange={e => setConfirm(e.target.value)} autoComplete="new-password" required />
        </div>
        <div className="form-group form-group-inline">
          <input id="pw-show" type="checkbox" checked={showPw} onChange={e => setShowPw(e.target.checked)} />
          <label htmlFor="pw-show">Show passwords</label>
        </div>
        <button type="submit" className="btn-primary" disabled={saving}>
          {saving ? 'Saving…' : 'Update password'}
        </button>
      </form>
    </section>
  )
}

// ---------- API Tokens section ----------

function TokensSection() {
  const [tokens, setTokens]   = useState(null) // null = loading
  const [name, setName]       = useState('')
  const [creating, setCreating] = useState(false)
  const [revealed, setRevealed] = useState(null) // plaintext after create
  const [copied, setCopied]   = useState(false)
  const [error, setError]     = useState(null)
  const [confirmRevokeId, setConfirmRevokeId] = useState(null)

  useEffect(() => {
    fetchTokens().then(setTokens).catch(err => setError(err.message))
  }, [])

  async function handleCreate(e) {
    e.preventDefault()
    setError(null)
    setCreating(true)
    try {
      const { token, plaintext } = await createToken(name.trim())
      setTokens(prev => [token, ...(prev ?? [])])
      setName('')
      setRevealed(plaintext)
    } catch (err) {
      setError(err.message ?? 'Failed to create token')
    } finally {
      setCreating(false)
    }
  }

  async function handleRevoke(id) {
    setError(null)
    setConfirmRevokeId(null)
    try {
      await revokeToken(id)
      setTokens(prev => (prev ?? []).filter(t => t.id !== id))
      if (revealed) setRevealed(null)
    } catch (err) {
      setError(err.message ?? 'Failed to revoke token')
    }
  }

  return (
    <section className="settings-section-block">
      <h3 className="settings-section-title">API Tokens</h3>
      {error && <p className="form-error" role="alert">{error}</p>}

      {revealed && (
        <div className="token-reveal">
          <p className="token-reveal-label">Copy this token now — it will not be shown again.</p>
          <div className="token-reveal-box">
            <code>{revealed}</code>
            <button
              className="btn-secondary btn-sm"
              onClick={() => {
                navigator.clipboard?.writeText(revealed)
                setCopied(true)
                setTimeout(() => setCopied(false), 2000)
              }}
              title="Copy to clipboard"
            >
              {copied ? 'Copied!' : 'Copy'}
            </button>
          </div>
          <button className="btn-link" onClick={() => setRevealed(null)}>Dismiss</button>
        </div>
      )}

      <form onSubmit={handleCreate} className="settings-inline-form">
        <input
          id="token-name-input"
          type="text"
          placeholder="Token name (optional)"
          aria-label="Token name (optional)"
          value={name}
          onChange={e => setName(e.target.value)}
          maxLength={100}
        />
        <button type="submit" className="btn-primary btn-sm" disabled={creating}>
          {creating ? 'Creating…' : 'Create token'}
        </button>
      </form>

      {tokens === null ? (
        <p className="settings-empty">Loading…</p>
      ) : tokens.length > 0 ? (
        <ul className="settings-list">
          {tokens.map(tok => (
            <li key={tok.id} className="settings-list-item">
              <div className="settings-list-info">
                <span className="settings-list-name">{tok.name || <em>unnamed</em>}</span>
                <span className="settings-list-meta">
                  Created {tok.created_at?.slice(0, 10)}
                  {tok.last_used_at && ` · Last used ${tok.last_used_at.slice(0, 10)}`}
                </span>
              </div>
              {confirmRevokeId === tok.id ? (
                <span className="delete-confirm" role="alert" aria-live="assertive" aria-atomic="true">
                  <span className="delete-confirm-label">Revoke?</span>
                  <button className="btn-icon btn-danger btn-sm" onClick={() => handleRevoke(tok.id)}>Yes</button>
                  <button className="btn-icon btn-sm" onClick={() => setConfirmRevokeId(null)}>No</button>
                </span>
              ) : (
                <button className="btn-danger btn-sm" onClick={() => setConfirmRevokeId(tok.id)}>Revoke</button>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="settings-empty">No tokens yet.</p>
      )}
    </section>
  )
}

// ---------- User management section (admin only) ----------

function UsersSection({ currentUserId }) {
  const [users, setUsers]       = useState(null) // null = loading
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [isAdmin, setIsAdmin]   = useState(false)
  const [creating, setCreating] = useState(false)
  const [error, setError]       = useState(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState(null)

  useEffect(() => {
    fetchUsers().then(setUsers).catch(err => setError(err.message))
  }, [])

  async function handleCreate(e) {
    e.preventDefault()
    setError(null)
    setCreating(true)
    try {
      const user = await createUser(username.trim(), password, isAdmin)
      setUsers(prev => [...prev, user])
      setUsername(''); setPassword(''); setIsAdmin(false)
    } catch (err) {
      setError(err.message ?? 'Failed to create user')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(id) {
    setError(null)
    setConfirmDeleteId(null)
    try {
      await deleteUser(id)
      setUsers(prev => (prev ?? []).filter(u => u.id !== id))
    } catch (err) {
      setError(err.message ?? 'Failed to delete user')
    }
  }

  return (
    <section className="settings-section-block">
      <h3 className="settings-section-title">Users</h3>
      {error && <p className="form-error" role="alert">{error}</p>}

      <form onSubmit={handleCreate} className="settings-sub-form">
        <div className="form-group">
          <label htmlFor="u-username">Username</label>
          <input id="u-username" type="text" value={username} onChange={e => setUsername(e.target.value)} maxLength={64} required />
        </div>
        <div className="form-group">
          <label htmlFor="u-password">Password</label>
          <input id="u-password" type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" required />
        </div>
        <div className="form-group form-group-inline">
          <input id="u-admin" type="checkbox" checked={isAdmin} onChange={e => setIsAdmin(e.target.checked)} />
          <label htmlFor="u-admin">Admin</label>
        </div>
        <button type="submit" className="btn-primary" disabled={creating}>
          {creating ? 'Creating…' : 'Create user'}
        </button>
      </form>

      {users === null ? (
        <p className="settings-empty">Loading…</p>
      ) : users.length > 0 ? (
        <ul className="settings-list">
          {users.map(u => (
            <li key={u.id} className="settings-list-item">
              <div className="settings-list-info">
                <span className="settings-list-name">
                  {u.username}
                  {u.is_admin && <span className="user-badge">admin</span>}
                </span>
                <span className="settings-list-meta">Joined {u.created_at?.slice(0, 10)}</span>
              </div>
              {u.id !== currentUserId && (
                confirmDeleteId === u.id ? (
                  <span className="delete-confirm" role="alert" aria-live="assertive" aria-atomic="true">
                    <span className="delete-confirm-label">Delete?</span>
                    <button className="btn-icon btn-danger btn-sm" onClick={() => handleDelete(u.id)}>Yes</button>
                    <button className="btn-icon btn-sm" onClick={() => setConfirmDeleteId(null)}>No</button>
                  </span>
                ) : (
                  <button className="btn-danger btn-sm" onClick={() => setConfirmDeleteId(u.id)}>Delete</button>
                )
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="settings-empty">No users.</p>
      )}
    </section>
  )
}

// ---------- Webhooks section ----------

const WEBHOOK_EVENTS = [
  { value: 'task.completed',   label: 'Task completed' },
  { value: 'task.uncompleted', label: 'Task uncompleted' },
]

function WebhooksSection() {
  const [hooks,    setHooks]    = useState(null)
  const [url,      setUrl]      = useState('')
  const [events,   setEvents]   = useState(['task.completed', 'task.uncompleted'])
  const [secret,   setSecret]   = useState('')
  const [creating, setCreating] = useState(false)
  const [error,    setError]    = useState(null)
  const [confirmDeleteId, setConfirmDeleteId] = useState(null)

  useEffect(() => {
    fetchWebhooks().then(setHooks).catch(err => setError(err.message))
  }, [])

  function toggleEvent(ev) {
    setEvents(prev =>
      prev.includes(ev) ? prev.filter(e => e !== ev) : [...prev, ev]
    )
  }

  async function handleCreate(e) {
    e.preventDefault()
    setError(null)
    if (events.length === 0) { setError('Select at least one event'); return }
    setCreating(true)
    try {
      const hook = await createWebhook(url.trim(), events, secret.trim())
      setHooks(prev => [...(prev ?? []), hook])
      setUrl(''); setSecret('')
      setEvents(['task.completed', 'task.uncompleted'])
    } catch (err) {
      setError(err.message ?? 'Failed to create webhook')
    } finally {
      setCreating(false)
    }
  }

  async function handleDelete(id) {
    setError(null)
    setConfirmDeleteId(null)
    try {
      await deleteWebhook(id)
      setHooks(prev => (prev ?? []).filter(h => h.id !== id))
    } catch (err) {
      setError(err.message ?? 'Failed to delete webhook')
    }
  }

  return (
    <section className="settings-section-block">
      <h3 className="settings-section-title">Webhooks</h3>
      <p className="settings-section-desc">
        POST a JSON payload to a URL when a task is completed or uncompleted.
        Optionally set a secret to verify the <code>X-Montly-Signature</code> header (HMAC-SHA256).
      </p>
      {error && <p className="form-error" role="alert">{error}</p>}

      <form onSubmit={handleCreate} className="settings-sub-form">
        <div className="form-group">
          <label htmlFor="wh-url">URL</label>
          <input
            id="wh-url"
            type="url"
            placeholder="https://example.com/hook"
            value={url}
            onChange={e => setUrl(e.target.value)}
            maxLength={2048}
            required
          />
        </div>
        <div className="form-group">
          <label>Events</label>
          <div className="webhook-events">
            {WEBHOOK_EVENTS.map(ev => (
              <label key={ev.value} className="webhook-event-label">
                <input
                  type="checkbox"
                  checked={events.includes(ev.value)}
                  onChange={() => toggleEvent(ev.value)}
                />
                {ev.label}
              </label>
            ))}
          </div>
        </div>
        <div className="form-group">
          <label htmlFor="wh-secret">Secret <span className="settings-optional">(optional)</span></label>
          <input
            id="wh-secret"
            type="text"
            placeholder="signing secret"
            value={secret}
            onChange={e => setSecret(e.target.value)}
            maxLength={200}
            autoComplete="off"
          />
        </div>
        <button type="submit" className="btn-primary" disabled={creating}>
          {creating ? 'Creating…' : 'Create webhook'}
        </button>
      </form>

      {hooks === null ? (
        <p className="settings-empty">Loading…</p>
      ) : hooks.length > 0 ? (
        <ul className="settings-list">
          {hooks.map(hook => (
            <li key={hook.id} className="settings-list-item">
              <div className="settings-list-info">
                <span className="settings-list-name webhook-url">{hook.url}</span>
                <span className="settings-list-meta">
                  {hook.events.split(',').join(' · ')} · Created {hook.created_at?.slice(0, 10)}
                </span>
              </div>
              {confirmDeleteId === hook.id ? (
                <span className="delete-confirm" role="alert" aria-live="assertive" aria-atomic="true">
                  <span className="delete-confirm-label">Delete?</span>
                  <button className="btn-icon btn-danger btn-sm" onClick={() => handleDelete(hook.id)}>Yes</button>
                  <button className="btn-icon btn-sm" onClick={() => setConfirmDeleteId(null)}>No</button>
                </span>
              ) : (
                <button className="btn-danger btn-sm" onClick={() => setConfirmDeleteId(hook.id)}>Delete</button>
              )}
            </li>
          ))}
        </ul>
      ) : (
        <p className="settings-empty">No webhooks yet.</p>
      )}
    </section>
  )
}

// ---------- Main SettingsView ----------

export default function SettingsView({ settings, onSave, user }) {
  const [currency,        setCurrency]        = useState(settings.currency          ?? '€')
  const [dateFormat,      setDateFormat]      = useState(settings.date_format       ?? 'long')
  const [colorMode,       setColorMode]       = useState(settings.color_mode        ?? 'system')
  const [taskSort,        setTaskSort]        = useState(settings.task_sort         ?? 'type')
  const [completedLast,   setCompletedLast]   = useState(settings.completed_last    ?? 'false')
  const [fiscalYearStart, setFiscalYearStart] = useState(settings.fiscal_year_start ?? '1')
  const [numberFormat,    setNumberFormat]    = useState(settings.number_format     ?? 'en')
  const [saving,  setSaving]  = useState(false)
  const [savedAt, setSavedAt] = useState(null)
  const [error,   setError]   = useState(null)
  const savedAtTimer = useRef(null)

  useEffect(() => { setColorMode(settings.color_mode ?? 'system') }, [settings.color_mode])

  const isDirty =
    currency        !== (settings.currency          ?? '€')      ||
    dateFormat      !== (settings.date_format       ?? 'long')   ||
    colorMode       !== (settings.color_mode        ?? 'system') ||
    taskSort        !== (settings.task_sort         ?? 'type')   ||
    completedLast   !== (settings.completed_last    ?? 'false')  ||
    fiscalYearStart !== (settings.fiscal_year_start ?? '1')      ||
    numberFormat    !== (settings.number_format     ?? 'en')

  async function handleSubmit(e) {
    e.preventDefault()
    setSaving(true)
    setError(null)
    try {
      await onSave({
        currency, date_format: dateFormat, color_mode: colorMode,
        task_sort: taskSort, completed_last: completedLast,
        fiscal_year_start: fiscalYearStart, number_format: numberFormat,
      })
      setSavedAt(new Date().toLocaleTimeString('default', { hour: '2-digit', minute: '2-digit' }))
      clearTimeout(savedAtTimer.current)
      savedAtTimer.current = setTimeout(() => setSavedAt(null), 30_000)
    } catch (err) {
      setError(err.message ?? 'Failed to save settings')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="settings-view">
      <h2>Settings</h2>

      {/* fix 1: save error */}
      {error && (
        <div className="error-banner">
          <span>{error}</span>
          <button className="error-dismiss" onClick={() => setError(null)} title="Dismiss" aria-label="Dismiss error">✕</button>
        </div>
      )}

      <form onSubmit={handleSubmit} className="settings-form">
        <div className="settings-section">

          <div className="form-group">
            <label htmlFor="s-currency">Currency</label>
            <select id="s-currency" value={currency} onChange={e => setCurrency(e.target.value)}>
              {CURRENCY_GROUPS.map(g => (
                <optgroup key={g.label} label={g.label}>
                  {g.options.map(c => (
                    <option key={c.symbol} value={c.symbol}>{c.label}</option>
                  ))}
                </optgroup>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-date-format">Month display</label>
            <select id="s-date-format" value={dateFormat} onChange={e => setDateFormat(e.target.value)}>
              {DATE_FORMATS.map(f => (
                <option key={f.value} value={f.value}>{f.label}</option>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-color-mode">Appearance</label>
            <select id="s-color-mode" value={colorMode} onChange={e => setColorMode(e.target.value)}>
              {COLOR_MODES.map(m => (
                <option key={m.value} value={m.value}>{m.label}</option>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-task-sort">Task order</label>
            <select id="s-task-sort" value={taskSort} onChange={e => setTaskSort(e.target.value)}>
              {TASK_SORT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-completed-last">Completed tasks</label>
            <select id="s-completed-last" value={completedLast} onChange={e => setCompletedLast(e.target.value)}>
              {COMPLETED_LAST_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-fiscal-year-start">Fiscal year starts</label>
            <select id="s-fiscal-year-start" value={fiscalYearStart} onChange={e => setFiscalYearStart(e.target.value)}>
              {FISCAL_YEAR_MONTHS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>

          <div className="form-group">
            <label htmlFor="s-number-format">Number format</label>
            <select id="s-number-format" value={numberFormat} onChange={e => setNumberFormat(e.target.value)}>
              {NUMBER_FORMAT_OPTIONS.map(o => (
                <option key={o.value} value={o.value}>{o.label}</option>
              ))}
            </select>
          </div>

        </div>

        {/* live preview */}
        <div className="settings-preview">
          <span className="settings-preview-label">Preview</span>
          <span className="settings-preview-value">
            {previewMonth(dateFormat)} · {formatAmount(1234.56, currency, numberFormat)}
          </span>
        </div>

        {/* fix 3 + 6 + 8 */}
        <div className="settings-save-row">
          <span className="settings-save-status" aria-live="polite">
            {isDirty
              ? <span className="settings-dirty">Unsaved changes</span>
              : savedAt
                ? <span className="settings-saved-at">Saved at {savedAt}</span>
                : null}
          </span>
          <button type="submit" className="btn-primary" disabled={saving || !isDirty}>
            {saving
              ? <><span className="btn-spinner" aria-hidden="true" /> Saving…</>
              : 'Save'}
          </button>
        </div>
      </form>

      <PasswordSection />
      <TokensSection />
      <WebhooksSection />
      {user?.is_admin && <UsersSection currentUserId={user.id} />}
    </div>
  )
}

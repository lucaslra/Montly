import { useState, useEffect, useCallback, useMemo } from 'react'
import TaskList from './components/TaskList.jsx'
import ManageView from './components/ManageView.jsx'
import SettingsView from './components/SettingsView.jsx'
import ReportView from './components/ReportView.jsx'
import LoginView from './components/LoginView.jsx'
import SetupView from './components/SetupView.jsx'
import { MonthPicker } from './components/TaskForm.jsx'
import {
  fetchSettings, updateSettings,
  fetchTasks, fetchCompletions,
  toggleCompletion, createTask, updateTask, deleteTask,
  uploadCompletionReceipt, deleteCompletionReceipt, patchCompletion,
  fetchMe, logout, fetchSetupStatus,
} from './api.js'
import './styles/main.css'
import { formatAmount } from './utils.js'

const DEFAULT_SETTINGS = {
  currency: '€', date_format: 'long', color_mode: 'system',
  task_sort: 'type', completed_last: 'false', fiscal_year_start: '1', number_format: 'en',
}

const TYPE_SORT_ORDER = { payment: 0, subscription: 1, bill: 2, reminder: 3 }

function pathToView(path) {
  if (path === '/manage')   return 'manage'
  if (path === '/settings') return 'settings'
  if (path === '/report')   return 'report'
  return 'monthly'
}

function viewToPath(view) {
  if (view === 'manage')   return '/manage'
  if (view === 'settings') return '/settings'
  if (view === 'report')   return '/report'
  return '/'
}

function useView() {
  const [view, setViewState] = useState(() => pathToView(window.location.pathname))

  useEffect(() => {
    const handler = () => setViewState(pathToView(window.location.pathname))
    window.addEventListener('popstate', handler)
    return () => window.removeEventListener('popstate', handler)
  }, [])

  function setView(next) {
    const v = typeof next === 'function' ? next(pathToView(window.location.pathname)) : next
    window.history.pushState(null, '', viewToPath(v))
    setViewState(v)
  }

  return [view, setView]
}

function getCurrentMonth() {
  const now = new Date()
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`
}

function addMonths(month, delta) {
  const [year, m] = month.split('-').map(Number)
  const d = new Date(year, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function formatMonth(month, format = 'long') {
  const [year, m] = month.split('-')
  const d = new Date(Number(year), Number(m) - 1, 1)
  switch (format) {
    case 'short':   return d.toLocaleString('default', { month: 'short', year: 'numeric' })
    case 'numeric': return `${String(Number(m)).padStart(2, '0')}/${year}`
    case 'iso':     return month
    default:        return d.toLocaleString('default', { month: 'long', year: 'numeric' })
  }
}

export default function App() {
  const [month, setMonth] = useState(getCurrentMonth)
  const [tasks, setTasks] = useState([])
  const [completionMap, setCompletionMap] = useState(new Map())
  const [view, setView] = useView()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [settings, setSettings] = useState(DEFAULT_SETTINGS)
  const [isDark, setIsDark] = useState(false)
  const [initialized, setInitialized] = useState(false)
  const [toast, setToast] = useState(null)
  const [toastFading, setToastFading] = useState(false)
  const [uploadingTaskId, setUploadingTaskId] = useState(null)
  const [showMonthPicker, setShowMonthPicker] = useState(false)
  // null = unknown (checking), false = not logged in, object = logged-in user
  const [user, setUser] = useState(null)
  const [authChecked, setAuthChecked] = useState(false)
  const [needsSetup, setNeedsSetup] = useState(false)

  // Check existing session on mount; if not authenticated, check first-run setup.
  useEffect(() => {
    fetchMe()
      .then(u => { setUser(u); setAuthChecked(true) })
      .catch(() => {
        fetchSetupStatus()
          .then(s => setNeedsSetup(s.needs_setup === true))
          .catch(() => {})
          .finally(() => { setUser(false); setAuthChecked(true) })
      })
  }, [])

  const onApiError = useCallback((e) => {
    if (e.status === 401) { setUser(false) } else { setError(e.message) }
  }, [])

  async function handleLogout() {
    await logout().catch(() => {})
    setUser(false)
  }

  // Load settings once on mount (after auth confirmed).
  useEffect(() => {
    if (!user) return
    fetchSettings().then(setSettings).catch(() => {})
  }, [user])

  // Apply color theme to <html>.
  useEffect(() => {
    const mode = settings.color_mode ?? 'system'
    const apply = (dark) => {
      document.documentElement.setAttribute('data-theme', dark ? 'dark' : 'light')
      setIsDark(dark)
    }
    if (mode === 'dark') { apply(true); return }
    if (mode === 'light') { apply(false); return }
    const mq = window.matchMedia('(prefers-color-scheme: dark)')
    apply(mq.matches)
    const handler = (e) => apply(e.matches)
    mq.addEventListener('change', handler)
    return () => mq.removeEventListener('change', handler)
  }, [settings.color_mode])

  async function handleToggleTheme() {
    const next = isDark ? 'light' : 'dark'
    const saved = await updateSettings({ ...settings, color_mode: next })
    setSettings(saved)
  }

  useEffect(() => {
    if (view !== 'monthly') setShowMonthPicker(false)
  }, [view])

  const loadData = useCallback(async (signal) => {
    if (!user) return
    setLoading(true)
    setError(null)
    try {
      const [taskList, completions] = await Promise.all([
        fetchTasks(month, signal),
        fetchCompletions(month, signal),
      ])
      setTasks(taskList)
      setCompletionMap(new Map(completions.map(c => [c.task_id, c])))
      setInitialized(true)
    } catch (e) {
      if (e.name === 'AbortError') return
      onApiError(e)
    } finally {
      setLoading(false)
    }
  }, [month, user, onApiError])

  useEffect(() => {
    const controller = new AbortController()
    loadData(controller.signal)
    return () => controller.abort()
  }, [loadData])

  const showToast = useCallback((msg) => {
    setToast(msg)
    setToastFading(false)
    const fadeTimer = setTimeout(() => setToastFading(true), 2000)
    const clearTimer = setTimeout(() => { setToast(null); setToastFading(false) }, 2500)
    return () => { clearTimeout(fadeTimer); clearTimeout(clearTimer) }
  }, [])

  const handleToggle = useCallback(async (taskId) => {
    // Optimistic update: flip completion state immediately for snappy UI.
    const wasCompleted = completionMap.has(taskId)
    const prevEntry    = completionMap.get(taskId)
    setCompletionMap(prev => {
      const next = new Map(prev)
      if (wasCompleted) { next.delete(taskId) }
      else { next.set(taskId, { task_id: taskId, month, completed_at: '', receipt_file: '', amount: '' }) }
      return next
    })
    try {
      const { completed } = await toggleCompletion(taskId, month)
      // Sync with definitive server state (covers edge cases like concurrent edits).
      setCompletionMap(prev => {
        const next = new Map(prev)
        if (completed) {
          next.set(taskId, prev.get(taskId) ?? { task_id: taskId, month, completed_at: '', receipt_file: '', amount: '' })
        } else {
          next.delete(taskId)
        }
        return next
      })
    } catch (e) {
      // Revert to pre-toggle state on failure.
      setCompletionMap(prev => {
        const next = new Map(prev)
        if (wasCompleted && prevEntry) { next.set(taskId, prevEntry) }
        else { next.delete(taskId) }
        return next
      })
      onApiError(e)
    }
  }, [month, completionMap, onApiError])

  const handleUploadReceipt = useCallback(async (taskId, file) => {
    setUploadingTaskId(taskId)
    try {
      const completion = await uploadCompletionReceipt(taskId, month, file)
      setCompletionMap(prev => new Map(prev).set(taskId, completion))
      showToast('Receipt uploaded')
    } catch (e) {
      onApiError(e)
    } finally {
      setUploadingTaskId(null)
    }
  }, [month, onApiError, showToast])

  const handleRemoveReceipt = useCallback(async (taskId) => {
    try {
      const completion = await deleteCompletionReceipt(taskId, month)
      setCompletionMap(prev => new Map(prev).set(taskId, completion))
    } catch (e) {
      onApiError(e)
    }
  }, [month, onApiError])

  const handleUpdateCompletion = useCallback(async (taskId, fields) => {
    try {
      const completion = await patchCompletion(taskId, month, fields)
      setCompletionMap(prev => new Map(prev).set(taskId, completion))
    } catch (e) {
      onApiError(e)
    }
  }, [month, onApiError])

  async function handleCreate(title, description, type, metadata, startDate, endDate, interval) {
    try {
      const task = await createTask(title, description, type, metadata, startDate, endDate, interval)
      setTasks(prev => [...prev, task])
      showToast('Task created')
    } catch (e) {
      onApiError(e)
      throw e
    }
  }

  async function handleUpdate(id, title, description, type, metadata, startDate, endDate, interval) {
    try {
      const task = await updateTask(id, title, description, type, metadata, startDate, endDate, interval)
      setTasks(prev => prev.map(t => (t.id === id ? task : t)))
      showToast('Task updated')
    } catch (e) {
      onApiError(e)
      throw e
    }
  }

  async function handleDelete(id) {
    try {
      await deleteTask(id)
      setTasks(prev => prev.filter(t => t.id !== id))
      setCompletionMap(prev => { const next = new Map(prev); next.delete(id); return next })
      showToast('Task deleted')
    } catch (e) {
      onApiError(e)
    }
  }

  async function handleSaveSettings(newSettings) {
    try {
      const saved = await updateSettings(newSettings)
      setSettings(saved)
    } catch (e) {
      onApiError(e)
    }
  }

  const { done, total, pct } = useMemo(() => {
    const d = tasks.filter(t => completionMap.has(t.id)).length
    const t = tasks.length
    return { done: d, total: t, pct: t ? (d / t) * 100 : 0 }
  }, [tasks, completionMap])

  const { hasMonetary, dueAmount, paidAmount, isSettled } = useMemo(() => {
    const MONETARY_TYPES = ['payment', 'subscription', 'bill']
    const monetaryTasks = tasks.filter(t => MONETARY_TYPES.includes(t.type))
    const due = monetaryTasks.reduce((sum, t) =>
      sum + (parseFloat(t.metadata?.amount ?? '') || 0), 0)
    const paid = monetaryTasks
      .filter(t => completionMap.has(t.id))
      .reduce((sum, t) => {
        const c = completionMap.get(t.id)
        return sum + (parseFloat(c?.amount || t.metadata?.amount || '') || 0)
      }, 0)
    return {
      hasMonetary: monetaryTasks.length > 0,
      dueAmount: due,
      paidAmount: paid,
      isSettled: monetaryTasks.length > 0 && due > 0 && paid >= due,
    }
  }, [tasks, completionMap])

  const sortedTasks = useMemo(() => {
    const sort       = settings.task_sort    ?? 'type'
    const doneBottom = settings.completed_last === 'true'
    return [...tasks].sort((a, b) => {
      if (doneBottom) {
        const diff = (completionMap.has(a.id) ? 1 : 0) - (completionMap.has(b.id) ? 1 : 0)
        if (diff !== 0) return diff
      }
      if (sort === 'name') return a.title.localeCompare(b.title)
      if (sort === 'type') {
        const ra = TYPE_SORT_ORDER[a.type] ?? 4
        const rb = TYPE_SORT_ORDER[b.type] ?? 4
        return ra !== rb ? ra - rb : a.title.localeCompare(b.title)
      }
      return 0
    })
  }, [tasks, completionMap, settings.task_sort, settings.completed_last])

  if (!authChecked) return <div className="loading">Loading…</div>
  if (!user && needsSetup) return <SetupView onComplete={u => { setNeedsSetup(false); setUser(u) }} />
  if (!user) return <LoginView onLogin={u => setUser(u)} />

  const currentMonth = getCurrentMonth()

  return (
    <div className="app">
      <header className="app-header">
        <div className="header-top">
          <button className="app-title-btn" onClick={() => setView('monthly')}>Montly</button>
          <div className="header-actions">
            <button
              className="theme-toggle-btn"
              onClick={handleToggleTheme}
              title="Toggle theme"
              aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
            >
              <span aria-hidden="true">{isDark ? '☀' : '☾'}</span>
            </button>
            <>
              <button
                className={`view-toggle${view === 'manage' ? ' active' : ''}`}
                onClick={() => setView(v => v === 'monthly' ? 'manage' : 'monthly')}
              >
                {view === 'monthly' ? 'Manage' : '← Back'}
              </button>
              <button
                className={`settings-btn${view === 'report' ? ' active' : ''}`}
                onClick={() => setView(v => v === 'report' ? 'monthly' : 'report')}
                title="Reports"
                aria-label="Reports"
                aria-pressed={view === 'report'}
              >
                <span aria-hidden="true">📊</span>
              </button>
              <button
                className={`settings-btn${view === 'settings' ? ' active' : ''}`}
                onClick={() => setView(v => v === 'settings' ? 'monthly' : 'settings')}
                title="Settings"
                aria-label="Settings"
                aria-pressed={view === 'settings'}
              >
                <span aria-hidden="true">⚙</span>
              </button>
            </>
            <button
              className="logout-btn"
              onClick={handleLogout}
              title={`Sign out (${user.username})`}
              aria-label={`Sign out (${user.username})`}
            >
              <span aria-hidden="true">⏏</span>
            </button>
          </div>
        </div>

        {view === 'monthly' && (
          <>
            <div className="month-nav">
              <button className="nav-btn" onClick={() => setMonth(m => addMonths(m, -1))} aria-label="Previous month"><span aria-hidden="true">‹</span></button>
              <button
                className="month-label-btn"
                onClick={() => setShowMonthPicker(v => !v)}
                aria-label={`${formatMonth(month, settings.date_format)} — click to jump to a month`}
                aria-expanded={showMonthPicker}
              >
                {formatMonth(month, settings.date_format)}
                {month === currentMonth && <span className="current-month-dot" aria-hidden="true" />}
              </button>
              <button className="nav-btn" onClick={() => setMonth(m => addMonths(m, 1))} aria-label="Next month"><span aria-hidden="true">›</span></button>
            </div>
            {showMonthPicker && (
              <div className="month-jump-picker">
                <MonthPicker
                  inline
                  value={month}
                  onChange={m => { if (m) { setMonth(m); setShowMonthPicker(false) } }}
                />
                <button
                  className="btn-secondary btn-sm"
                  onClick={() => { setMonth(currentMonth); setShowMonthPicker(false) }}
                >
                  Today
                </button>
              </div>
            )}
            <div className="progress">
              <div
                className="progress-bar"
                role="progressbar"
                aria-valuenow={done}
                aria-valuemin={0}
                aria-valuemax={total}
                aria-label={`${done} of ${total} tasks complete`}
              >
                <div className="progress-fill" style={{ width: `${pct}%` }} />
              </div>
              <span className="progress-text" aria-hidden="true">{done}/{total}</span>
            </div>
            {hasMonetary && (
              <div className="monetary-summary">
                {isSettled ? (
                  <span className="monetary-settled">✓ Settled {formatAmount(paidAmount, settings.currency, settings.number_format)}</span>
                ) : (
                  <>
                    <span>Due {formatAmount(dueAmount, settings.currency, settings.number_format)}</span>
                    <span className="monetary-divider">·</span>
                    <span className="monetary-paid">Paid {formatAmount(paidAmount, settings.currency, settings.number_format)}</span>
                  </>
                )}
              </div>
            )}
          </>
        )}
      </header>

      <main className="app-main">
        {error && (
          <div className="error-banner" role="alert">
            <span>{error}</span>
            <button className="error-dismiss" onClick={() => setError(null)} aria-label="Dismiss error">✕</button>
          </div>
        )}
        {/* fix 6: spinner only on first load; subsequent loads fade the list */}
        {!initialized && loading ? (
          <div className="loading" aria-busy="true" aria-live="polite">Loading…</div>
        ) : view === 'monthly' ? (
          <div style={{ opacity: loading ? 0.5 : 1, transition: 'opacity 0.15s', pointerEvents: loading ? 'none' : 'auto' }}>
            <TaskList
              tasks={sortedTasks}
              completionMap={completionMap}
              currency={settings.currency}
              uploadingTaskId={uploadingTaskId}
              onToggle={handleToggle}
              onUploadReceipt={handleUploadReceipt}
              onRemoveReceipt={handleRemoveReceipt}
              onUpdateCompletion={handleUpdateCompletion}
              onGoToManage={() => setView('manage')}
            />
          </div>
        ) : view === 'manage' ? (
          <ManageView
            tasks={tasks}
            currency={settings.currency}
            numberFormat={settings.number_format}
            onCreate={handleCreate}
            onUpdate={handleUpdate}
            onDelete={handleDelete}
          />
        ) : view === 'report' ? (
          <ReportView
            month={month}
            tasks={tasks}
            completionMap={completionMap}
            currency={settings.currency}
            numberFormat={settings.number_format}
            fiscalYearStart={parseInt(settings.fiscal_year_start ?? '1', 10)}
          />
        ) : (
          <SettingsView settings={settings} onSave={handleSaveSettings} user={user} />
        )}
      </main>
      {toast && (
        <div className={`toast${toastFading ? ' toast--fading' : ''}`} role="status" aria-live="polite">
          {toast}
        </div>
      )}
    </div>
  )
}

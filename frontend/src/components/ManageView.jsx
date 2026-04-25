import { useState, useEffect } from 'react'
import TaskForm from './TaskForm.jsx'
import { formatAmount } from '../utils.js'
import { fetchArchivedTasks, deleteTask } from '../api.js'

const INTERVAL_LABELS = { 2: 'Bimestral', 3: 'Trimestral', 6: 'Semestral', 12: 'Annual' }
function intervalLabel(n) { return INTERVAL_LABELS[n] ?? `Every ${n}m` }

function fmtMonth(ym) {
  if (!ym) return ''
  const [year, m] = ym.split('-')
  return new Date(Number(year), Number(m) - 1, 1)
    .toLocaleString('default', { month: 'short', year: 'numeric' })
}

const TASK_TYPES = ['payment', 'subscription', 'bill', 'reminder']

export default function ManageView({ tasks, currency = '$', numberFormat = 'en', onCreate, onUpdate, onDelete, onArchive, onUnarchive }) {
  const [editing, setEditing] = useState(null)
  const [adding, setAdding] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(null)
  const [confirmArchive, setConfirmArchive] = useState(null)
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [showArchived, setShowArchived] = useState(false)
  const [archivedTasks, setArchivedTasks] = useState([])
  const [loadingArchived, setLoadingArchived] = useState(false)
  const [confirmPermDelete, setConfirmPermDelete] = useState(null)

  useEffect(() => {
    if (!showArchived) return
    setLoadingArchived(true)
    fetchArchivedTasks()
      .then(setArchivedTasks)
      .catch(() => {})
      .finally(() => setLoadingArchived(false))
  }, [showArchived])

  async function handleCreate(title, description, type, metadata, startDate, endDate, interval) {
    try {
      await onCreate(title, description, type, metadata, startDate, endDate, interval)
      setAdding(false)
    } catch { /* error shown by App */ }
  }

  async function handleUpdate(title, description, type, metadata, startDate, endDate, interval) {
    try {
      await onUpdate(editing.id, title, description, type, metadata, startDate, endDate, interval)
      setEditing(null)
    } catch { /* error shown by App */ }
  }

  async function handleUnarchive(id) {
    await onUnarchive(id)
    setArchivedTasks(prev => prev.filter(t => t.id !== id))
  }

  async function handlePermDelete(id) {
    try {
      await onDelete(id)
      setArchivedTasks(prev => prev.filter(t => t.id !== id))
      setConfirmPermDelete(null)
    } catch { /* error shown by App */ }
  }

  const q = search.trim().toLowerCase()
  const visibleTasks = tasks.filter(task => {
    if (typeFilter && task.type !== typeFilter) return false
    if (q && !task.title.toLowerCase().includes(q) && !(task.description ?? '').toLowerCase().includes(q)) return false
    return true
  })

  return (
    <div className="manage-view">
      <div className="manage-header">
        <h2>All Tasks</h2>
        <button className="btn-primary" onClick={() => setAdding(true)}>+ Add Task</button>
      </div>

      <div className="manage-filters">
        <input
          className="manage-search"
          type="search"
          placeholder="Search tasks…"
          aria-label="Search tasks"
          value={search}
          onChange={e => setSearch(e.target.value)}
        />
        <div className="manage-type-filters" role="group" aria-label="Filter by type">
          <button
            className={`type-filter-btn${typeFilter === '' ? ' active' : ''}`}
            onClick={() => setTypeFilter('')}
          >
            All
          </button>
          {TASK_TYPES.map(t => (
            <button
              key={t}
              className={`type-filter-btn type-filter-${t}${typeFilter === t ? ' active' : ''}`}
              onClick={() => setTypeFilter(prev => prev === t ? '' : t)}
            >
              {t}
            </button>
          ))}
        </div>
      </div>

      {tasks.length === 0 ? (
        <div className="empty">No tasks yet. Add one above.</div>
      ) : visibleTasks.length === 0 ? (
        <div className="empty">No tasks match your filter.</div>
      ) : (
        <ul className="manage-list">
          {visibleTasks.map(task => (
            <li
              key={task.id}
              className={`manage-item${['payment','subscription','bill'].includes(task.type) ? ` type-${task.type}-item` : ''}`}
            >
              <div className="manage-item-content">
                <div className="manage-item-title-row">
                  <span className="manage-item-title">{task.title}</span>
                  {task.type && <span className={`type-badge type-${task.type}`}>{task.type}</span>}
                  {task.interval > 1 && <span className="interval-badge">{intervalLabel(task.interval)}</span>}
                  {task.start_date && <span className="end-date-badge">from {fmtMonth(task.start_date)}</span>}
                  {task.end_date   && <span className="end-date-badge">ends {fmtMonth(task.end_date)}</span>}
                </div>
                {task.description && <span className="manage-item-desc">{task.description}</span>}
                <TaskMeta task={task} currency={currency} numberFormat={numberFormat} />
              </div>

              <div className="manage-item-actions">
                {confirmArchive === task.id ? (
                  <div className="delete-confirm" role="alert" aria-live="assertive" aria-atomic="true">
                    <span className="delete-confirm-label">Archive?</span>
                    <button
                      className="btn-icon btn-warning"
                      onClick={() => { onArchive(task.id); setConfirmArchive(null) }}
                    >
                      Yes
                    </button>
                    <button className="btn-icon" onClick={() => setConfirmArchive(null)}>No</button>
                  </div>
                ) : (
                  <>
                    <button className="btn-icon" onClick={() => setEditing(task)}>Edit</button>
                    <button
                      className="btn-icon btn-warning"
                      onClick={() => setConfirmArchive(task.id)}
                    >
                      Archive
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <div className="archived-section">
        <button
          className="archived-toggle"
          onClick={() => setShowArchived(prev => !prev)}
          aria-expanded={showArchived}
        >
          {showArchived ? '▾' : '▸'} Archived tasks
        </button>

        {showArchived && (
          loadingArchived ? (
            <div className="empty">Loading…</div>
          ) : archivedTasks.length === 0 ? (
            <div className="empty">No archived tasks.</div>
          ) : (
            <ul className="manage-list archived-list">
              {archivedTasks.map(task => (
                <li key={task.id} className="manage-item archived-item">
                  <div className="manage-item-content">
                    <div className="manage-item-title-row">
                      <span className="manage-item-title">{task.title}</span>
                      {task.type && <span className={`type-badge type-${task.type}`}>{task.type}</span>}
                    </div>
                    {task.description && <span className="manage-item-desc">{task.description}</span>}
                    <TaskMeta task={task} currency={currency} numberFormat={numberFormat} />
                  </div>

                  <div className="manage-item-actions">
                    {confirmPermDelete === task.id ? (
                      <div className="delete-confirm" role="alert" aria-live="assertive" aria-atomic="true">
                        <span className="delete-confirm-label">Delete permanently?</span>
                        <button
                          className="btn-icon btn-danger"
                          onClick={() => handlePermDelete(task.id)}
                        >
                          Yes
                        </button>
                        <button className="btn-icon" onClick={() => setConfirmPermDelete(null)}>No</button>
                      </div>
                    ) : (
                      <>
                        <button className="btn-icon" onClick={() => handleUnarchive(task.id)}>Restore</button>
                        <button
                          className="btn-icon btn-danger"
                          onClick={() => setConfirmPermDelete(task.id)}
                        >
                          Delete
                        </button>
                      </>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )
        )}
      </div>

      {(adding || editing) && (
        <TaskForm
          task={editing ?? null}
          currency={currency}
          numberFormat={numberFormat}
          onSave={editing ? handleUpdate : handleCreate}
          onClose={() => { setAdding(false); setEditing(null) }}
        />
      )}
    </div>
  )
}

const MONETARY_TYPES = ['payment', 'subscription', 'bill']

function TaskMeta({ task, currency = '$', numberFormat = 'en' }) {
  if (!MONETARY_TYPES.includes(task.type)) return null
  const { amount } = task.metadata ?? {}
  if (!amount) return null
  return (
    <div className="task-meta">
      <span className={`meta-amount meta-amount-${task.type}`}>{formatAmount(parseFloat(amount), currency, numberFormat)}</span>
    </div>
  )
}

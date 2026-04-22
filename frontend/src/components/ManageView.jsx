import { useState } from 'react'
import TaskForm from './TaskForm.jsx'

const INTERVAL_LABELS = { 2: 'Bimestral', 3: 'Trimestral', 6: 'Semestral', 12: 'Annual' }
function intervalLabel(n) { return INTERVAL_LABELS[n] ?? `Every ${n}m` }

// fix 3: human-readable month label
function fmtMonth(ym) {
  if (!ym) return ''
  const [year, m] = ym.split('-')
  return new Date(Number(year), Number(m) - 1, 1)
    .toLocaleString('default', { month: 'short', year: 'numeric' })
}

export default function ManageView({ tasks, currency = '$', onCreate, onUpdate, onDelete }) {
  const [editing, setEditing] = useState(null)
  const [adding, setAdding] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(null) // fix 4: task id pending delete

  // fix 1: keep modal open on error (App re-throws)
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

  return (
    <div className="manage-view">
      <div className="manage-header">
        <h2>All Tasks</h2>
        <button className="btn-primary" onClick={() => setAdding(true)}>+ Add Task</button>
      </div>

      {tasks.length === 0 ? (
        <div className="empty">No tasks yet. Add one above.</div>
      ) : (
        <ul className="manage-list">
          {tasks.map(task => (
            <li
              key={task.id}
              // fix 5: left accent for payment tasks
              className={`manage-item${['payment','subscription','bill'].includes(task.type) ? ` type-${task.type}-item` : ''}`}
            >
              <div className="manage-item-content">
                <div className="manage-item-title-row">
                  <span className="manage-item-title">{task.title}</span>
                  {task.type && <span className={`type-badge type-${task.type}`}>{task.type}</span>}
                  {/* fix 3: formatted date badges */}
                  {task.interval > 1 && <span className="interval-badge">{intervalLabel(task.interval)}</span>}
                  {task.start_date && <span className="end-date-badge">from {fmtMonth(task.start_date)}</span>}
                  {task.end_date   && <span className="end-date-badge">ends {fmtMonth(task.end_date)}</span>}
                </div>
                {task.description && <span className="manage-item-desc">{task.description}</span>}
                <TaskMeta task={task} currency={currency} />
              </div>

              <div className="manage-item-actions">
                {/* fix 4: inline delete confirm */}
                {confirmDelete === task.id ? (
                  <div className="delete-confirm" role="status" aria-live="polite" aria-atomic="true">
                    <span className="delete-confirm-label">Delete?</span>
                    <button
                      className="btn-icon btn-danger"
                      onClick={() => { onDelete(task.id); setConfirmDelete(null) }}
                    >
                      Yes
                    </button>
                    <button className="btn-icon" onClick={() => setConfirmDelete(null)}>No</button>
                  </div>
                ) : (
                  <>
                    <button className="btn-icon" onClick={() => setEditing(task)}>Edit</button>
                    <button
                      className="btn-icon btn-danger"
                      onClick={() => setConfirmDelete(task.id)}
                    >
                      Delete
                    </button>
                  </>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {(adding || editing) && (
        <TaskForm
          task={editing ?? null}
          currency={currency}
          onSave={editing ? handleUpdate : handleCreate}
          onClose={() => { setAdding(false); setEditing(null) }}
        />
      )}
    </div>
  )
}

const MONETARY_TYPES = ['payment', 'subscription', 'bill']

function TaskMeta({ task, currency = '$' }) {
  if (!MONETARY_TYPES.includes(task.type)) return null
  const { amount } = task.metadata ?? {}
  if (!amount) return null
  return (
    <div className="task-meta">
      <span className={`meta-amount meta-amount-${task.type}`}>{currency}{amount}</span>
    </div>
  )
}

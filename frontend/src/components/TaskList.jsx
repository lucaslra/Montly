import { memo, useRef, useState } from 'react'

const ACCEPT = '.pdf,.jpg,.jpeg,.png,.webp,.gif'

const TaskList = memo(function TaskList({ tasks, completionMap, currency = '$', uploadingTaskId, onToggle, onUploadReceipt, onRemoveReceipt, onUpdateCompletion, onGoToManage }) {
  const [confirmUndo, setConfirmUndo] = useState(null)

  if (tasks.length === 0) {
    return (
      <div className="empty">
        <p>No tasks yet.</p>
        <button className="btn-primary" style={{ marginTop: 12 }} onClick={onGoToManage}>
          + Add Tasks
        </button>
      </div>
    )
  }

  return (
    <ul className="task-list">
      {tasks.map(task => {
        const completion = completionMap.get(task.id)
        const done = Boolean(completion)
        const showConfirm = confirmUndo === task.id

        return (
          <li
            key={task.id}
            className={`task-item${done ? ' completed' : ''}`}
          >
            <button
              className="task-toggle-btn"
              aria-pressed={done}
              aria-label={`${done ? 'Unmark' : 'Mark'} "${task.title}"`}
              onClick={() => {
                if (showConfirm) { setConfirmUndo(null); return }
                if (done && completion?.receipt_file) {
                  setConfirmUndo(task.id)
                } else {
                  onToggle(task.id)
                }
              }}
            >
              <span className="task-checkbox" aria-hidden="true">{done ? '✓' : ''}</span>
            </button>
            <div className="task-content">
              <div className="task-title-row">
                <span className="task-title">{task.title}</span>
                {task.type && <span className={`type-badge type-${task.type}`}>{task.type}</span>}
              </div>
              {task.description && <span className="task-desc">{task.description}</span>}
              {['payment', 'subscription', 'bill'].includes(task.type) && (
                <PaymentSlot
                  taskId={task.id}
                  taskType={task.type}
                  defaultAmount={task.metadata?.amount ?? ''}
                  completion={completion}
                  currency={currency}
                  isUploading={uploadingTaskId === task.id}
                  onSaveAmount={amount => onUpdateCompletion(task.id, { amount })}
                  onUploadReceipt={file => onUploadReceipt(task.id, file)}
                  onRemoveReceipt={() => onRemoveReceipt(task.id)}
                />
              )}
              {done && (
                <NoteSlot
                  note={completion?.note ?? ''}
                  onSave={note => onUpdateCompletion(task.id, { note })}
                />
              )}
              {showConfirm && (
                <div
                  className="undo-confirm"
                  role="status"
                  aria-live="polite"
                  aria-atomic="true"
                >
                  <span className="undo-confirm-label">Removes attached receipt.</span>
                  <button
                    className="btn-icon btn-danger"
                    onClick={() => { onToggle(task.id); setConfirmUndo(null) }}
                  >
                    Unmark
                  </button>
                  <button className="btn-icon" onClick={() => setConfirmUndo(null)}>
                    Cancel
                  </button>
                </div>
              )}
            </div>
          </li>
        )
      })}
    </ul>
  )
})

export default TaskList

function PaymentSlot({ taskId, taskType, defaultAmount, completion, currency = '$', isUploading, onSaveAmount, onUploadReceipt, onRemoveReceipt }) {
  const done = Boolean(completion)
  const overrideAmount = completion?.amount ?? ''
  const effectiveAmount = overrideAmount || defaultAmount
  const isOverridden = Boolean(overrideAmount)

  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')
  const [amountError, setAmountError] = useState('') // fix 5
  const [confirmRemove, setConfirmRemove] = useState(false)
  const fileRef = useRef(null)

  function startEdit(e) {
    e.stopPropagation()
    setDraft(overrideAmount || defaultAmount)
    setAmountError('')
    setEditing(true)
  }

  function cancelEdit(e) {
    e?.stopPropagation()
    setAmountError('')
    setEditing(false)
  }

  function saveEdit(e) {
    e?.stopPropagation()
    const val = draft.trim()
    // fix 5: inline validation
    if (val !== '' && isNaN(Number(val))) {
      setAmountError('Enter a valid number')
      return
    }
    setAmountError('')
    onSaveAmount(val)
    setEditing(false)
  }

  function handleKeyDown(e) {
    e.stopPropagation()
    if (e.key === 'Enter') saveEdit(e)
    if (e.key === 'Escape') cancelEdit(e)
  }

  function handleFileChange(e) {
    const file = e.target.files[0]
    if (file) onUploadReceipt(file)
    e.target.value = ''
  }

  function handleRemoveReceipt(e) {
    e.stopPropagation()
    setConfirmRemove(true)
  }

  if (!effectiveAmount && !done) return null

  return (
    <div className="payment-slot" onClick={e => e.stopPropagation()}>
      {/* Amount */}
      {(effectiveAmount || done) && (
        editing ? (
          <span className="amount-edit-row">
            <span className="input-prefix small">{currency}</span>
            <span className="amount-edit-wrap">
              <input
                className={`amount-inline-input${amountError ? ' input-error' : ''}`}
                value={draft}
                onChange={e => { setDraft(e.target.value); setAmountError('') }}
                onKeyDown={handleKeyDown}
                autoFocus
                inputMode="decimal"
                autoComplete="off"
                placeholder={defaultAmount || '0.00'}
                aria-describedby={amountError ? `amount-error-${taskId}` : undefined}
              />
              {amountError && (
                <span id={`amount-error-${taskId}`} role="alert" className="amount-error">{amountError}</span>
              )}
            </span>
            <button className="amount-confirm-btn" onClick={saveEdit} title="Save">✓</button>
            <button className="amount-cancel-btn" onClick={cancelEdit} title="Cancel">✕</button>
          </span>
        ) : (
          // fix 3: meaningful disabled tooltip
          <button
            className={`amount-display${isOverridden ? ' overridden' : ''}${taskType ? ` type-${taskType}` : ''}`}
            onClick={done ? startEdit : undefined}
            disabled={!done}
            title={done ? 'Click to override for this month' : 'Mark task as done to edit amount'}
          >
            {effectiveAmount ? `${currency}${effectiveAmount}` : '—'}
            {/* fix 8: replace asterisk with legible badge */}
            {isOverridden && <span className="override-badge">custom</span>}
          </button>
        )
      )}

      {/* Receipt */}
      {done && (
        completion?.receipt_file ? (
          confirmRemove ? (
            <span className="undo-confirm" onClick={e => e.stopPropagation()}>
              <span className="undo-confirm-label">Remove receipt?</span>
              <button
                className="btn-icon btn-danger"
                onClick={e => { e.stopPropagation(); onRemoveReceipt(); setConfirmRemove(false) }}
              >
                Remove
              </button>
              <button
                className="btn-icon"
                onClick={e => { e.stopPropagation(); setConfirmRemove(false) }}
              >
                Cancel
              </button>
            </span>
          ) : (
            <>
              <a
                href={`/api/receipts/${completion.receipt_file}`}
                target="_blank"
                rel="noreferrer"
                className="receipt-link-inline"
                aria-label="View receipt"
              >
                📎 receipt
              </a>
              <button
                className="receipt-replace-btn"
                onClick={e => { e.stopPropagation(); fileRef.current?.click() }}
                disabled={isUploading}
                aria-label="Replace receipt"
              >
                {isUploading ? 'Uploading…' : 'replace'}
              </button>
              <button className="receipt-remove-btn" onClick={handleRemoveReceipt} aria-label="Remove receipt">
                remove
              </button>
            </>
          )
        ) : (
          <button
            className="receipt-attach-btn"
            onClick={() => fileRef.current?.click()}
            disabled={isUploading}
            aria-label="Attach receipt"
          >
            {isUploading ? '⏳ Uploading…' : '📎 attach receipt'}
          </button>
        )
      )}
      <input ref={fileRef} type="file" accept={ACCEPT} style={{ display: 'none' }} onChange={handleFileChange} aria-label="Upload receipt" />
    </div>
  )
}

function NoteSlot({ note, onSave }) {
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState('')

  function startEdit(e) {
    e.stopPropagation()
    setDraft(note)
    setEditing(true)
  }

  function save(e) {
    e?.stopPropagation()
    onSave(draft.trim())
    setEditing(false)
  }

  function cancel(e) {
    e?.stopPropagation()
    setEditing(false)
  }

  function handleKeyDown(e) {
    e.stopPropagation()
    if (e.key === 'Escape') cancel(e)
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) save(e)
  }

  if (editing) {
    return (
      <div className="note-slot note-slot--editing" onClick={e => e.stopPropagation()}>
        <textarea
          className="note-textarea"
          value={draft}
          onChange={e => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Add a note…"
          maxLength={1000}
          rows={2}
          autoFocus
          aria-label="Completion note"
        />
        <span className="note-actions">
          <button className="amount-confirm-btn" onClick={save} title="Save (Ctrl+Enter)">✓</button>
          <button className="amount-cancel-btn" onClick={cancel} title="Cancel (Escape)">✕</button>
        </span>
      </div>
    )
  }

  return (
    <div className="note-slot" onClick={e => e.stopPropagation()}>
      {note ? (
        <button className="note-display note-display--filled" onClick={startEdit} title="Edit note">
          {note}
        </button>
      ) : (
        <button className="note-display note-display--empty" onClick={startEdit}>
          + add note
        </button>
      )}
    </div>
  )
}

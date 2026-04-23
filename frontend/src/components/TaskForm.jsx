import { useState, useRef, useEffect } from 'react'

const MONTH_LABELS = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']

function formatValue(value) {
  if (!value) return null
  const [y, m] = value.split('-')
  return `${MONTH_LABELS[parseInt(m) - 1]} ${y}`
}

// Grid-based month picker. Two modes:
//   popover (default) — shows a trigger button that opens a floating panel
//   inline            — always shows the grid, no trigger (pass inline={true})
export function MonthPicker({ value, onChange, inline = false, label }) {
  const [open, setOpen] = useState(false)
  const [viewYear, setViewYear] = useState(() =>
    value ? parseInt(value.split('-')[0]) : new Date().getFullYear()
  )
  const rootRef = useRef(null)

  // Sync viewYear when value changes from outside
  useEffect(() => {
    if (value) setViewYear(parseInt(value.split('-')[0]))
  }, [value])

  // Close on outside click or Escape (popover mode only)
  useEffect(() => {
    if (inline || !open) return
    function handleClick(e) {
      if (!rootRef.current?.contains(e.target)) setOpen(false)
    }
    function handleKeyDown(e) {
      if (e.key === 'Escape') { e.stopPropagation(); setOpen(false) }
    }
    document.addEventListener('mousedown', handleClick)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [open, inline])

  const selYear  = value ? value.split('-')[0] : null
  const selMonth = value ? value.split('-')[1] : null

  function select(idx) {
    const m = String(idx + 1).padStart(2, '0')
    onChange(`${viewYear}-${m}`)
    if (!inline) setOpen(false)
  }

  function clear(e) {
    e.stopPropagation()
    onChange('')
    if (!inline) setOpen(false)
  }

  const grid = (
    <div className="mp-panel" onClick={e => e.stopPropagation()}>
      <div className="mp-year-nav">
        <button type="button" className="mp-nav-btn" onClick={() => setViewYear(y => y - 1)} aria-label="Previous year"><span aria-hidden="true">‹</span></button>
        <span className="mp-year-label" aria-live="polite" aria-atomic="true">{viewYear}</span>
        <button type="button" className="mp-nav-btn" onClick={() => setViewYear(y => y + 1)} aria-label="Next year"><span aria-hidden="true">›</span></button>
      </div>
      <div className="mp-grid">
        {MONTH_LABELS.map((label, idx) => {
          const m = String(idx + 1).padStart(2, '0')
          const selected = selYear === String(viewYear) && selMonth === m
          return (
            <button
              key={m}
              type="button"
              className={`mp-month${selected ? ' mp-month--selected' : ''}`}
              onClick={() => select(idx)}
              aria-pressed={selected}
            >
              {label}
            </button>
          )
        })}
      </div>
      {value && (
        <button type="button" className="mp-clear-btn" onClick={clear}>Clear</button>
      )}
    </div>
  )

  if (inline) return grid

  return (
    <div className="mp-root" ref={rootRef}>
      <button
        type="button"
        className={`mp-trigger${value ? '' : ' mp-trigger--empty'}`}
        onClick={() => setOpen(o => !o)}
        aria-label={label ? `${label}: ${formatValue(value) ?? 'not set'}` : undefined}
        aria-expanded={open}
        aria-haspopup="dialog"
      >
        {formatValue(value) ?? <span className="mp-placeholder">Select month</span>}
      </button>
      {open && grid}
    </div>
  )
}

export default function TaskForm({ task, currency = '$', onSave, onClose }) {
  const [title, setTitle] = useState(task?.title ?? '')
  const [description, setDescription] = useState(task?.description ?? '')
  const [type, setType] = useState(task?.type ?? '')
  const [amount, setAmount] = useState(task?.metadata?.amount ?? '')
  const [startDate, setStartDate] = useState(task?.start_date ?? '')
  const [endDate, setEndDate] = useState(task?.end_date ?? '')
  const [interval, setInterval] = useState(task?.interval ?? 1)
  const [saving, setSaving] = useState(false)
  const [dateError, setDateError] = useState('')
  const [amountError, setAmountError] = useState('')
  const modalRef = useRef(null)

  useEffect(() => {
    // Lock body scroll while modal is open.
    // iOS Safari ignores overflow:hidden on body, so we use the position:fixed trick.
    const scrollY = window.scrollY
    document.body.style.position = 'fixed'
    document.body.style.top = `-${scrollY}px`
    document.body.style.width = '100%'
    return () => {
      document.body.style.position = ''
      document.body.style.top = ''
      document.body.style.width = ''
      window.scrollTo(0, scrollY)
    }
  }, [])

  useEffect(() => {
    const prev = document.activeElement
    const focusable = modalRef.current?.querySelectorAll(
      'input, select, button, [tabindex]:not([tabindex="-1"])'
    )
    if (focusable?.length) focusable[0].focus()
    return () => prev?.focus()
  }, [])

  function handleOverlayKeyDown(e) {
    if (e.key === 'Escape') { onClose(); return }
    if (e.key !== 'Tab') return
    const focusable = Array.from(
      modalRef.current?.querySelectorAll(
        'input, select, button, [tabindex]:not([tabindex="-1"])'
      ) ?? []
    )
    if (!focusable.length) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault(); last.focus()
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault(); first.focus()
    }
  }

  function buildMetadata() {
    if (['payment', 'subscription', 'bill'].includes(type)) return { amount: amount.trim() }
    return {}
  }

  async function handleSubmit(e) {
    e.preventDefault()
    if (!title.trim()) return
    if (amount.trim() !== '' && isNaN(Number(amount.trim()))) {
      setAmountError('Amount must be a number')
      return
    }
    if (startDate && endDate && startDate > endDate) {
      setDateError('Start date must be on or before end date')
      return
    }
    setAmountError('')
    setDateError('')
    setSaving(true)
    try {
      await onSave(title.trim(), description.trim(), type, buildMetadata(), startDate, endDate, interval)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div
      className="modal-overlay"
      onClick={onClose}
      onKeyDown={handleOverlayKeyDown}
    >
      <div
        className="modal"
        ref={modalRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="task-form-title"
        onClick={e => e.stopPropagation()}
      >
        <h3 id="task-form-title">{task ? 'Edit Task' : 'New Task'}</h3>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="task-title">Title</label>
            <input
              id="task-title"
              type="text"
              value={title}
              onChange={e => setTitle(e.target.value)}
              placeholder="Task name"
              required
            />
          </div>
          <div className="form-group">
            <label htmlFor="task-desc">Description <span className="label-optional">(optional)</span></label>
            <input
              id="task-desc"
              type="text"
              value={description}
              onChange={e => setDescription(e.target.value)}
              placeholder="Optional details"
            />
          </div>
          <div className="form-group">
            <label htmlFor="task-type">Type <span className="label-optional">(optional)</span></label>
            <select id="task-type" value={type} onChange={e => setType(e.target.value)}>
              <option value="">— none —</option>
              <option value="payment">Payment</option>
              <option value="subscription">Subscription</option>
              <option value="bill">Bill</option>
              <option value="reminder">Reminder</option>
            </select>
          </div>

          {['payment', 'subscription', 'bill'].includes(type) && (
            <div className="type-fields">
              <div className="form-group">
                <label htmlFor="task-amount">Amount <span className="label-optional">(optional)</span></label>
                <div className="input-prefix-wrap">
                  <span className="input-prefix">{currency}</span>
                  <input
                    id="task-amount"
                    type="text"
                    inputMode="decimal"
                    value={amount}
                    onChange={e => { setAmount(e.target.value); setAmountError('') }}
                    placeholder="0.00"
                    className="input-with-prefix"
                    aria-describedby={amountError ? 'task-amount-error' : undefined}
                  />
                </div>
                {amountError && <p id="task-amount-error" className="form-error" role="alert">{amountError}</p>}
              </div>
            </div>
          )}

          <div className="form-group">
            <label>Start date <span className="label-optional">(optional)</span></label>
            <MonthPicker value={startDate} onChange={v => { setStartDate(v); setDateError('') }} label="Start date" />
          </div>
          <div className="form-group">
            <label>End date <span className="label-optional">(optional)</span></label>
            <MonthPicker value={endDate} onChange={v => { setEndDate(v); setDateError('') }} label="End date" />
          </div>
          {dateError && <p className="form-error" role="alert">{dateError}</p>}

          <div className="form-group">
            <label htmlFor="task-interval">Recurrence</label>
            <select id="task-interval" value={interval} onChange={e => setInterval(Number(e.target.value))}>
              <option value={1}>Monthly</option>
              <option value={2}>Bimestral (every 2 months)</option>
              <option value={3}>Trimestral (every 3 months)</option>
              <option value={6}>Semestral (every 6 months)</option>
              <option value={12}>Annual</option>
            </select>
          </div>

          <div className="form-actions">
            <button type="button" className="btn-secondary" onClick={onClose}>Cancel</button>
            <button type="submit" className="btn-primary" disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

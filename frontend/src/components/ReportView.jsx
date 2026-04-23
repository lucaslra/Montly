import { useState, useEffect, useMemo, useRef } from 'react'
import { fetchTasks, fetchCompletions, exportCompletionsCSV } from '../api.js'
import { formatAmount } from '../utils.js'

// ── Constants ─────────────────────────────────────────────────────────────────

const MONETARY_TYPES = ['payment', 'subscription', 'bill']

const CATEGORY_COLOR = {
  payment:      '#2563eb',
  subscription: '#8b5cf6',
  bill:         '#0ea5e9',
}
const CATEGORY_LABEL = {
  payment:      'Payments',
  subscription: 'Subscriptions',
  bill:         'Bills',
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function addMonths(month, delta) {
  const [y, m] = month.split('-').map(Number)
  const d = new Date(y, m - 1 + delta, 1)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

function shortMonth(ym) {
  const [y, m] = ym.split('-')
  return new Date(Number(y), Number(m) - 1, 1)
    .toLocaleString('default', { month: 'short' })
}

function calcActual(tasks, cmap) {
  return tasks
    .filter(t => MONETARY_TYPES.includes(t.type) && cmap.has(t.id))
    .reduce((sum, t) => {
      const c = cmap.get(t.id)
      return sum + (parseFloat(c.amount || t.metadata?.amount || '') || 0)
    }, 0)
}

function calcExpected(tasks) {
  return tasks
    .filter(t => MONETARY_TYPES.includes(t.type))
    .reduce((sum, t) => sum + (parseFloat(t.metadata?.amount || '') || 0), 0)
}

function calcByCategory(tasks, cmap) {
  const acc = { payment: 0, subscription: 0, bill: 0 }
  tasks
    .filter(t => MONETARY_TYPES.includes(t.type) && cmap.has(t.id))
    .forEach(t => {
      const c = cmap.get(t.id)
      acc[t.type] += parseFloat(c.amount || t.metadata?.amount || '') || 0
    })
  return acc
}

// ── SpendingChart ─────────────────────────────────────────────────────────────

function SpendingChart({ data, currency, numberFormat }) {
  const [hovered, setHovered] = useState(null)

  const maxValue = Math.max(...data.map(d => Math.max(d.actual, d.expected)), 0.01)

  // 4 evenly spaced grid lines
  const gridLines = [0.25, 0.5, 0.75, 1].map(f => ({
    pct: f * 100,
    label: formatAmount(f * maxValue, currency, numberFormat, 0),
  }))

  return (
    <div className="spending-chart">
      {/* Grid + bars */}
      <div className="chart-canvas">
        {/* Horizontal grid lines */}
        {gridLines.map(({ pct, label }) => (
          <div key={pct} className="chart-gridline" style={{ bottom: `${pct}%` }}>
            <span className="chart-gridline-label">{label}</span>
          </div>
        ))}

        {/* Bars */}
        <div className="chart-bars">
          {data.map(d => {
            const actualPct  = (d.actual   / maxValue) * 100
            const expectedPct = (d.expected / maxValue) * 100
            const isActive = hovered?.month === d.month
            return (
              <button
                key={d.month}
                className={[
                  'bar-col',
                  d.isCurrent  ? 'bar-col--current'  : '',
                  d.isForecast ? 'bar-col--forecast'  : '',
                  isActive     ? 'bar-col--hovered'   : '',
                ].filter(Boolean).join(' ')}
                onPointerEnter={() => setHovered(d)}
                onPointerLeave={() => setHovered(null)}
                onFocus={() => setHovered(d)}
                onBlur={() => setHovered(null)}
                aria-label={
                  d.isForecast
                    ? `${shortMonth(d.month)}: forecast ${formatAmount(d.expected, currency, numberFormat)}`
                    : `${shortMonth(d.month)}: paid ${formatAmount(d.actual, currency, numberFormat)}`
                }
              >
                <div className="bar-track">
                  {/* Expected-amount tick mark (history only) */}
                  {!d.isForecast && d.expected > 0 && (
                    <div className="bar-expected-mark" style={{ bottom: `${expectedPct}%` }} />
                  )}
                  {/* Actual bar (history) or expected bar (forecast) */}
                  <div
                    className={`bar-fill${d.isForecast ? ' bar-fill--forecast' : ''}`}
                    style={{ height: `${Math.max(d.isForecast ? expectedPct : actualPct, d.actual > 0 || d.expected > 0 ? 1 : 0)}%` }}
                  />
                </div>
                <span className="bar-label" aria-hidden="true">
                  {d.label}
                  {d.isCurrent && <span className="bar-current-dot" />}
                </span>
              </button>
            )
          })}
        </div>
      </div>

      {/* Tooltip */}
      <div className={`chart-tooltip${hovered ? ' chart-tooltip--visible' : ''}`} aria-live="polite">
        {hovered ? (
          <>
            <strong>{shortMonth(hovered.month)} {hovered.month.slice(0, 4)}</strong>
            {hovered.isForecast ? (
              <span>Forecast: {formatAmount(hovered.expected, currency, numberFormat)}</span>
            ) : (
              <>
                <span>Paid: {formatAmount(hovered.actual, currency, numberFormat)}</span>
                {hovered.expected > 0 && <span className="chart-tooltip-muted">Expected: {formatAmount(hovered.expected, currency, numberFormat)}</span>}
              </>
            )}
          </>
        ) : <span className="chart-tooltip-hint">Select a bar for details</span>}
      </div>

      {/* Legend */}
      <div className="chart-legend">
        <span className="legend-item"><span className="legend-swatch legend-swatch--actual" />Paid</span>
        <span className="legend-item"><span className="legend-swatch legend-swatch--forecast" />Forecast</span>
        <span className="legend-item"><span className="legend-mark--expected" />Expected</span>
      </div>
    </div>
  )
}

// ── CategoryChart ─────────────────────────────────────────────────────────────

function CategoryChart({ data, currency, numberFormat }) {
  const total = data.reduce((s, d) => s + d.amount, 0)

  if (total === 0) {
    return <p className="report-empty">No completed spending tasks in the last 3 months.</p>
  }

  const R  = 48
  const CX = 64
  const CY = 64
  const SW = 18
  const circumference = 2 * Math.PI * R

  let cumOffset = 0
  const segments = data.map(({ type, amount }) => {
    const dash = (amount / total) * circumference
    const seg  = { type, dash, offset: cumOffset }
    cumOffset += dash
    return seg
  })

  return (
    <div className="category-chart">
      <svg width="128" height="128" viewBox="0 0 128 128" aria-hidden="true">
        {/* Background ring */}
        <circle cx={CX} cy={CY} r={R} fill="none" stroke="var(--border)" strokeWidth={SW} />
        {/* Segments */}
        {segments.map(({ type, dash, offset }) => (
          <circle
            key={type}
            cx={CX} cy={CY} r={R}
            fill="none"
            stroke={CATEGORY_COLOR[type]}
            strokeWidth={SW}
            strokeDasharray={`${dash} ${circumference - dash}`}
            strokeDashoffset={circumference * 0.25 - offset}
          />
        ))}
        {/* Center total */}
        <text x={CX} y={CY - 5} textAnchor="middle" fontSize="9" fill="var(--muted)">Total</text>
        <text x={CX} y={CY + 11} textAnchor="middle" fontSize="14" fontWeight="700" fill="var(--text)">{formatAmount(total, currency, numberFormat, 0)}</text>
      </svg>

      <ul className="category-legend">
        {data.map(({ type, amount }) => (
          <li key={type} className="category-legend-item">
            <span className="category-legend-dot" style={{ background: CATEGORY_COLOR[type] }} />
            <span className="category-legend-label">{CATEGORY_LABEL[type]}</span>
            <span className="category-legend-pct">{Math.round((amount / total) * 100)}%</span>
            <span className="category-legend-amount">{formatAmount(amount, currency, numberFormat)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

// ── ReportView ────────────────────────────────────────────────────────────────

export default function ReportView({ month, tasks, completionMap, currency, numberFormat = 'en', fiscalYearStart = 1 }) {
  const [monthData, setMonthData] = useState([])
  const [loading,   setLoading]   = useState(true)
  const [error,     setError]     = useState(null)
  // Session-level cache: avoid re-fetching months already loaded in this session.
  const cache = useRef(new Map())

  useEffect(() => {
    const historyMonths  = Array.from({ length: 6 }, (_, i) => addMonths(month, -(6 - i)))
    const forecastMonths = Array.from({ length: 3 }, (_, i) => addMonths(month, i + 1))

    function fetchHistory(m) {
      if (cache.current.has(`h:${m}`)) return Promise.resolve(cache.current.get(`h:${m}`))
      return Promise.all([fetchTasks(m), fetchCompletions(m)]).then(([t, c]) => {
        const entry = { month: m, tasks: t, cmap: new Map(c.map(x => [x.task_id, x])), isForecast: false }
        cache.current.set(`h:${m}`, entry)
        return entry
      })
    }

    function fetchForecast(m) {
      if (cache.current.has(`f:${m}`)) return Promise.resolve(cache.current.get(`f:${m}`))
      return fetchTasks(m).then(t => {
        const entry = { month: m, tasks: t, cmap: new Map(), isForecast: true }
        cache.current.set(`f:${m}`, entry)
        return entry
      })
    }

    Promise.all([
      ...historyMonths.map(fetchHistory),
      ...forecastMonths.map(fetchForecast),
    ])
      .then(data => { setMonthData(data); setLoading(false) })
      .catch(e   => { setError(e.message); setLoading(false) })
  }, [month])

  // Merge fetched months with the currently-loaded month (avoid duplicate fetch)
  const allData = useMemo(() => [
    ...monthData.filter(d => !d.isForecast),
    { month, tasks, cmap: completionMap, isForecast: false, isCurrent: true },
    ...monthData.filter(d => d.isForecast),
  ], [monthData, month, tasks, completionMap])

  const chartData = useMemo(() =>
    allData.map(d => ({
      month:      d.month,
      label:      shortMonth(d.month),
      actual:     d.isForecast ? 0 : calcActual(d.tasks, d.cmap),
      expected:   calcExpected(d.tasks),
      isForecast: d.isForecast,
      isCurrent:  d.isCurrent ?? false,
    })),
    [allData]
  )

  const categoryData = useMemo(() => {
    const recent = allData.filter(d => !d.isForecast).slice(-3)
    const acc = { payment: 0, subscription: 0, bill: 0 }
    recent.forEach(d => {
      const by = calcByCategory(d.tasks, d.cmap)
      Object.keys(acc).forEach(k => { acc[k] += by[k] })
    })
    return Object.entries(acc)
      .filter(([, v]) => v > 0)
      .map(([type, amount]) => ({ type, amount }))
      .sort((a, b) => b.amount - a.amount)
  }, [allData])

  const stats = useMemo(() => {
    const history = chartData.filter(d => !d.isForecast)
    const [y, m]  = month.split('-').map(Number)
    const fyStartYear  = m >= fiscalYearStart ? y : y - 1
    const fyStartMonth = `${fyStartYear}-${String(fiscalYearStart).padStart(2, '0')}`
    const ytd = history
      .filter(d => d.month >= fyStartMonth && d.month <= month)
      .reduce((s, d) => s + d.actual, 0)
    const active = history.filter(d => d.actual > 0)
    const avg  = active.length
      ? active.reduce((s, d) => s + d.actual, 0) / active.length
      : 0
    const maxAmt = Math.max(...history.map(d => d.actual), 0)
    const peak   = history.find(d => d.actual === maxAmt && maxAmt > 0)
    const next   = chartData.find(d => d.isForecast)
    return {
      ytd,
      ytdLabel: fiscalYearStart === 1 ? 'Year to date' : 'FY to date',
      avg,
      peakAmt:   maxAmt,
      peakLabel: peak ? `${shortMonth(peak.month)} ${peak.month.slice(0, 4)}` : '—',
      nextExpected: next?.expected ?? 0,
    }
  }, [chartData, month, fiscalYearStart])

  if (loading) return <div className="loading" role="status" aria-live="polite" aria-busy="true">Loading report…</div>
  if (error)   return <div className="error-banner" role="alert">{error}</div>

  const hasData = chartData.some(d => d.actual > 0 || d.expected > 0)

  return (
    <div className="report-view">
      <h2>Reports</h2>

      {/* Summary stats */}
      <div className="report-stats">
        <div className="stat-card">
          <span className="stat-label">{stats.ytdLabel}</span>
          <span className="stat-value">{formatAmount(stats.ytd, currency, numberFormat)}</span>
        </div>
        <div className="stat-card">
          <span className="stat-label">Monthly avg</span>
          <span className="stat-value">{formatAmount(stats.avg, currency, numberFormat)}</span>
        </div>
        <div className="stat-card">
          <span className="stat-label">Peak month</span>
          <span className="stat-value">{formatAmount(stats.peakAmt, currency, numberFormat)}</span>
          <span className="stat-sub">{stats.peakLabel}</span>
        </div>
        <div className="stat-card">
          <span className="stat-label">Next month</span>
          <span className="stat-value">{formatAmount(stats.nextExpected, currency, numberFormat)}</span>
          <span className="stat-sub">forecast</span>
        </div>
      </div>

      {/* Spending history + forecast */}
      <section className="report-section">
        <h3 className="report-section-title">Monthly Spending</h3>
        {hasData ? (
          <SpendingChart data={chartData} currency={currency} numberFormat={numberFormat} />
        ) : (
          <p className="report-empty">
            No spending data yet. Complete tasks that have an amount set to see history here.
          </p>
        )}
      </section>

      {/* Category breakdown */}
      <section className="report-section">
        <div className="report-section-header">
          <h3 className="report-section-title">Category Breakdown</h3>
          <span className="report-section-sub">last 3 months</span>
        </div>
        <CategoryChart data={categoryData} currency={currency} numberFormat={numberFormat} />
      </section>

      {/* Export */}
      <ExportSection currentMonth={month} />
    </div>
  )
}

// ── ExportSection ─────────────────────────────────────────────────────────────

function currentYear() {
  return String(new Date().getFullYear())
}

function ExportSection({ currentMonth }) {
  const year = currentYear()
  const [from, setFrom] = useState(`${year}-01`)
  const [to,   setTo]   = useState(currentMonth)
  const [downloading, setDownloading] = useState(false)
  const [error, setError] = useState(null)

  async function handleExport(e) {
    e.preventDefault()
    setError(null)
    setDownloading(true)
    try {
      const res = await exportCompletionsCSV(from, to)
      if (!res.ok) {
        const data = await res.json().catch(() => ({}))
        throw new Error(data.error ?? `HTTP ${res.status}`)
      }
      const blob = await res.blob()
      const url  = URL.createObjectURL(blob)
      const a    = document.createElement('a')
      a.href     = url
      a.download = `montly-export-${from}-${to}.csv`
      a.click()
      URL.revokeObjectURL(url)
    } catch (err) {
      setError(err.message ?? 'Export failed')
    } finally {
      setDownloading(false)
    }
  }

  return (
    <section className="report-section report-export">
      <h3 className="report-section-title">Export</h3>
      {error && <p className="form-error" role="alert">{error}</p>}
      <form className="export-form" onSubmit={handleExport}>
        <label htmlFor="export-from" className="export-label">From</label>
        <input
          id="export-from"
          className="export-month-input"
          type="month"
          value={from}
          onChange={e => setFrom(e.target.value)}
          required
        />
        <label htmlFor="export-to" className="export-label">to</label>
        <input
          id="export-to"
          className="export-month-input"
          type="month"
          value={to}
          onChange={e => setTo(e.target.value)}
          required
        />
        <button type="submit" className="btn-secondary btn-sm" disabled={downloading}>
          {downloading ? 'Downloading…' : 'Download CSV'}
        </button>
      </form>
    </section>
  )
}

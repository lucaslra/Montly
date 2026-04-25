import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ReportView from '../components/ReportView.jsx'
import * as api from '../api.js'

vi.mock('../api.js', () => ({
  fetchReport:           vi.fn(),
  exportCompletionsCSV:  vi.fn(),
  importCompletionsCSV:  vi.fn(),
}))

beforeEach(() => {
  // History (6 months) + forecast (3 months) are all empty by default
  api.fetchReport.mockResolvedValue({ months: [] })

  // jsdom stubs for export download path
  URL.createObjectURL = vi.fn().mockReturnValue('blob:fake-url')
  URL.revokeObjectURL = vi.fn()
})

afterEach(() => vi.clearAllMocks())

// ── Helpers ───────────────────────────────────────────────────────────────────

const FIXED_MONTH = '2026-04'

function renderReport(overrides = {}) {
  const props = {
    month:           FIXED_MONTH,
    tasks:           [],
    completionMap:   new Map(),
    currency:        '$',
    numberFormat:    'en',
    fiscalYearStart: 1,
    ...overrides,
  }
  return render(<ReportView {...props} />)
}

function makeMoneyTask(id, amount) {
  return { id, title: `Task ${id}`, type: 'bill', metadata: { amount: String(amount) }, start_date: '', end_date: '', interval: 1 }
}

// ── Loading & error states ────────────────────────────────────────────────────

describe('ReportView loading and error states', () => {
  it('shows a loading indicator while history is being fetched', () => {
    api.fetchReport.mockReturnValue(new Promise(() => {})) // never resolves
    renderReport()
    expect(screen.getByText('Loading report…')).toBeInTheDocument()
  })

  it('shows an error banner when the history fetch fails', async () => {
    api.fetchReport.mockRejectedValue(new Error('network failure'))
    renderReport()
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('network failure')
    )
  })
})

// ── Stats cards ───────────────────────────────────────────────────────────────

describe('ReportView stats cards', () => {
  it('renders all four stat card labels after data loads', async () => {
    renderReport()
    await waitFor(() => screen.getByText('Year to date'))
    expect(screen.getByText('Monthly avg')).toBeInTheDocument()
    expect(screen.getByText('Peak month')).toBeInTheDocument()
    expect(screen.getByText('Next month')).toBeInTheDocument()
  })

  it('shows "Year to date" when fiscalYearStart is January (1)', async () => {
    renderReport({ fiscalYearStart: 1 })
    await waitFor(() => expect(screen.getByText('Year to date')).toBeInTheDocument())
  })

  it('shows "FY to date" when fiscalYearStart is not January', async () => {
    renderReport({ fiscalYearStart: 4 })
    await waitFor(() => expect(screen.getByText('FY to date')).toBeInTheDocument())
  })

  it('includes the current month paid amount in the YTD total', async () => {
    const task = makeMoneyTask(1, 50)
    const completionMap = new Map([[1, { task_id: 1, amount: '', note: '' }]])
    renderReport({ tasks: [task], completionMap, fiscalYearStart: 1 })
    await waitFor(() => screen.getByText('Year to date'))
    // $50.00 should appear somewhere in the stats
    expect(screen.getAllByText('$50.00').length).toBeGreaterThanOrEqual(1)
  })
})

// ── No-data message ───────────────────────────────────────────────────────────

describe('ReportView empty states', () => {
  it('shows an empty-data message when no spending history exists', async () => {
    renderReport()
    await waitFor(() =>
      expect(screen.getByText(/No spending data yet/)).toBeInTheDocument()
    )
  })

  it('shows the donut chart empty message when no recent category spending', async () => {
    renderReport()
    await waitFor(() =>
      expect(screen.getByText(/No completed spending tasks in the last 3 months/)).toBeInTheDocument()
    )
  })
})

// ── Export section ────────────────────────────────────────────────────────────

describe('ReportView ExportSection', () => {
  it('renders From and To date inputs and the Download CSV button', async () => {
    renderReport()
    await waitFor(() => screen.getByLabelText('From'))
    expect(screen.getByLabelText('to')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Download CSV' })).toBeInTheDocument()
  })

  it('defaults "to" to the current report month', async () => {
    renderReport({ month: '2026-04' })
    await waitFor(() => screen.getByLabelText('to'))
    expect(screen.getByLabelText('to')).toHaveValue('2026-04')
  })

  it('calls exportCompletionsCSV with the from/to values on submit', async () => {
    const fakeResponse = {
      ok: true,
      blob: vi.fn().mockResolvedValue(new Blob(['a,b'])),
    }
    api.exportCompletionsCSV.mockResolvedValue(fakeResponse)
    renderReport({ month: '2026-04' })
    await waitFor(() => screen.getByRole('button', { name: 'Download CSV' }))

    const fromInput = screen.getByLabelText('From')
    await userEvent.clear(fromInput)
    await userEvent.type(fromInput, '2026-01')
    await userEvent.click(screen.getByRole('button', { name: 'Download CSV' }))

    await waitFor(() =>
      expect(api.exportCompletionsCSV).toHaveBeenCalledWith('2026-01', '2026-04')
    )
  })

  it('disables the button while the download is in progress', async () => {
    let resolve
    api.exportCompletionsCSV.mockReturnValue(new Promise(r => { resolve = r }))
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Download CSV' }))
    await userEvent.click(screen.getByRole('button', { name: 'Download CSV' }))
    expect(screen.getByRole('button', { name: 'Downloading…' })).toBeDisabled()
    resolve({ ok: true, blob: vi.fn().mockResolvedValue(new Blob([])) })
  })

  it('shows an error when the export response is not ok', async () => {
    api.exportCompletionsCSV.mockResolvedValue({
      ok: false,
      status: 400,
      json: vi.fn().mockResolvedValue({ error: 'invalid date range' }),
    })
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Download CSV' }))
    await userEvent.click(screen.getByRole('button', { name: 'Download CSV' }))
    await waitFor(() =>
      expect(screen.getByRole('alert')).toHaveTextContent('invalid date range')
    )
  })
})

// ── ImportSection ─────────────────────────────────────────────────────────────

// jsdom does not fully implement the FileList API, so we simulate file selection
// by defining the files property and firing a change event.
function selectFile(input, file) {
  Object.defineProperty(input, 'files', { value: [file], configurable: true })
  fireEvent.change(input)
}

describe('ImportSection', () => {
  it('renders the Import button disabled before a file is chosen', async () => {
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Import CSV' }))
    expect(screen.getByRole('button', { name: 'Import CSV' })).toBeDisabled()
  })

  it('enables the button once a file is selected', async () => {
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Import CSV' }))
    const file = new File(['col\n'], 'test.csv', { type: 'text/csv' })
    const input = document.querySelector('input[type="file"]')
    act(() => selectFile(input, file))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Import CSV' })).not.toBeDisabled())
  })

  it('calls importCompletionsCSV and shows the result summary', async () => {
    api.importCompletionsCSV.mockResolvedValue({ tasks_created: 1, completions_created: 3, completions_updated: 0 })
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Import CSV' }))
    const file = new File(['col\n'], 'test.csv', { type: 'text/csv' })
    const input = document.querySelector('input[type="file"]')
    act(() => selectFile(input, file))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Import CSV' })).not.toBeDisabled())
    fireEvent.submit(input.closest('form'))
    await waitFor(() => expect(api.importCompletionsCSV).toHaveBeenCalled())
    expect(screen.getByRole('status')).toHaveTextContent('3 created')
    expect(screen.getByRole('status')).toHaveTextContent('1 new task')
  })

  it('shows an error when the import fails', async () => {
    api.importCompletionsCSV.mockRejectedValue(new Error('unexpected CSV header'))
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Import CSV' }))
    const file = new File(['bad header\n'], 'bad.csv', { type: 'text/csv' })
    const input = document.querySelector('input[type="file"]')
    act(() => selectFile(input, file))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Import CSV' })).not.toBeDisabled())
    fireEvent.submit(input.closest('form'))
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('unexpected CSV header'))
  })

  it('shows Importing… while in flight and disables the button', async () => {
    let resolve
    api.importCompletionsCSV.mockReturnValue(new Promise(r => { resolve = r }))
    renderReport()
    await waitFor(() => screen.getByRole('button', { name: 'Import CSV' }))
    const file = new File(['col\n'], 'f.csv', { type: 'text/csv' })
    const input = document.querySelector('input[type="file"]')
    act(() => selectFile(input, file))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Import CSV' })).not.toBeDisabled())
    fireEvent.submit(input.closest('form'))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Importing…' })).toBeDisabled())
    resolve({ tasks_created: 0, completions_created: 0, completions_updated: 0 })
  })
})

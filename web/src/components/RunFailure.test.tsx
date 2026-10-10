import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Diagnosis, Fix } from '../api/types'
import { RunFailure } from './RunFailure'

const fix = (o: Partial<Fix> & Pick<Fix, 'id' | 'op'>): Fix => ({ label: o.id, needs_person: false, desk_ok: false, ...o })

function diagnosis(o: Partial<Diagnosis> = {}): Diagnosis {
  return {
    id: 7, run_id: 10, kind: 'gate_failed', summary: 'The gate failed: make check exited 2', created: '2026-10-08T10:04:00Z',
    evidence: 'FAIL internal/x\nexit status 2', command: 'make check', exit_code: 2, files: ['internal/x.go'],
    fixes: [
      fix({ id: 'continue', op: 'continue', label: 'Continue the session', desk_ok: true }),
      fix({ id: 'widen', op: 'widen_scope', label: 'Widen the scope', needs_person: true, args: { paths: 'internal/x.go' } }),
      fix({ id: 'log', op: 'open_log', label: 'Open the log' }),
    ],
    ...o,
  }
}

const run = (d?: Diagnosis) => ({ id: 10, state: 'failed' as const, diagnosis: d })

let calls: { path: string; body?: unknown }[]
beforeEach(() => {
  calls = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }), { status: 200 })
    if (path === '/v1/runs/10/diagnoses') return new Response(JSON.stringify([diagnosis()]), { status: 200 })
    if (path === '/v1/runs/10/fix') {
      const { action } = JSON.parse(String(init?.body)) as { action: string }
      if (action === 'continue') return new Response(JSON.stringify({ fix: fix({ id: 'continue', op: 'continue' }), run: { id: 11 }, done: true, message: 'continuing run’s session' }), { status: 200 })
      return new Response(JSON.stringify({ error: 'run 10 has no suggested fix' }), { status: 409 })
    }
    return new Response('{}', { status: 404 })
  })
})
afterEach(() => vi.unstubAllGlobals())

describe('RunFailure', () => {
  it('renders nothing for a run with no diagnosis', () => {
    const { container } = render(<RunFailure run={run()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the reason, the evidence and the command', async () => {
    render(<RunFailure run={run(diagnosis())} />)
    expect(screen.getByText('The gate failed: make check exited 2')).toBeInTheDocument()
    expect(screen.getByText('make check')).toBeInTheDocument()
    expect(screen.getByText(/FAIL internal\/x/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open the log' })).toHaveAttribute('href', '#/runs/10/log')
  })

  it('takes a fix that needs no confirmation with one click', async () => {
    const fixed = vi.fn()
    render(<RunFailure run={run(diagnosis())} onFixed={fixed} />)
    await userEvent.click(screen.getByRole('button', { name: 'Continue the session' }))
    expect(await screen.findByRole('status')).toHaveTextContent('continuing run’s session')
    expect(calls.find((c) => c.path === '/v1/runs/10/fix')?.body).toEqual({ action: 'continue' })
    expect(fixed).toHaveBeenCalled()
  })

  it('asks the person to confirm a fix that needs them, saying what it does', async () => {
    render(<RunFailure run={run(diagnosis())} />)
    await userEvent.click(screen.getByRole('button', { name: /Widen the scope/ }))
    expect(calls.some((c) => c.path === '/v1/runs/10/fix')).toBe(false)
    const group = screen.getByRole('group', { name: /Confirm: Widen the scope/ })
    expect(group).toHaveTextContent('Adds internal/x.go to this lane’s scope')
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(screen.queryByRole('group', { name: /Confirm/ })).toBeNull()
  })

  it('shows the daemon’s refusal', async () => {
    render(<RunFailure run={run(diagnosis())} />)
    await userEvent.click(screen.getByRole('button', { name: /Widen the scope/ }))
    await userEvent.click(screen.getByRole('button', { name: /Confirm: Widen the scope/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('no suggested fix')
  })

  it('fetches the evidence a list left out when opened', async () => {
    render(<RunFailure run={run(diagnosis({ evidence: undefined, evidence_omitted: true }))} />)
    await userEvent.click(screen.getByText('Evidence'))
    await waitFor(() => expect(screen.getByText(/FAIL internal\/x/)).toBeInTheDocument())
    expect(calls.some((c) => c.path === '/v1/runs/10/diagnoses')).toBe(true)
  })

  it('compact form is a disclosure on the reason', () => {
    render(<RunFailure run={run(diagnosis())} compact />)
    expect(screen.getByText('The gate failed: make check exited 2').tagName).toBe('SUMMARY')
  })
})

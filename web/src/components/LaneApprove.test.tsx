import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { LaneApproval } from '../api/types'
import { LaneApprove } from './LaneApprove'

const base: LaneApproval = {
  supported: true, mr: 7, mr_url: 'https://forge/mr/7', mr_state: 'opened', sha: 'abcdef0123456789',
  approved: false, pipeline: 9, pipeline_status: 'success', require_approval: 'failed',
}

interface Call { path: string; body: unknown }

/** A daemon that answers the approval read with `view` and the approve with `approve`. */
function daemon(view: Partial<LaneApproval>, approve?: () => Response) {
  const calls: Call[] = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    calls.push({ path, body: init?.body ? JSON.parse(String(init.body)) : undefined })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
    if (path.endsWith('/approve')) return approve ? approve() : new Response('{}', { status: 500 })
    return new Response(JSON.stringify({ ...base, ...view }))
  })
  return calls
}

afterEach(() => vi.unstubAllGlobals())

describe('LaneApprove', () => {
  it('shows the approval and pipeline state', async () => {
    daemon({ approved: true, approved_by: ['ann'] })
    render(<LaneApprove laneId={1} />)
    expect(await screen.findByText('approved by ann')).toBeInTheDocument()
    expect(screen.getByText(/require-approval failed/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Approve/ })).not.toBeInTheDocument()
  })

  it('asks to confirm, then approves the head it showed', async () => {
    const calls = daemon({}, () => new Response(JSON.stringify({ ...base, approved: true, retried: true })))
    render(<LaneApprove laneId={1} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Approve !7…' }))
    // Nothing is sent before the confirm.
    expect(calls.some((c) => c.path.endsWith('/approve'))).toBe(false)
    const confirm = screen.getByRole('group', { name: 'Confirm approval' })
    expect(confirm).toHaveTextContent('abcdef01')
    await userEvent.click(screen.getByRole('button', { name: 'Approve at abcdef01' }))
    expect(await screen.findByRole('status')).toHaveTextContent('The require-approval job was retried.')
    expect(calls.find((c) => c.path === '/v1/lanes/1/approve')?.body).toEqual({ sha: 'abcdef0123456789' })
  })

  it('shows the daemon’s refusal when the head moved', async () => {
    daemon({}, () => new Response(JSON.stringify({ error: '!7 changed since you looked (now at 1234abcd): look at the diff again' }), { status: 409 }))
    render(<LaneApprove laneId={1} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Approve !7…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Approve at abcdef01' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('changed since you looked')
  })

  it('offers no button for a merged request', async () => {
    daemon({ mr_state: 'merged' })
    render(<LaneApprove laneId={1} />)
    expect(await screen.findByText('merged')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Approve/ })).not.toBeInTheDocument()
  })

  it('says so, with the link, on a forge that cannot approve', async () => {
    daemon({ supported: false, reason: 'Approval is not available for this lane’s forge: use the merge request’s link.' })
    render(<LaneApprove laneId={1} />)
    expect(await screen.findByText(/Approval is not available/)).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open the merge request' })).toHaveAttribute('href', 'https://forge/mr/7')
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

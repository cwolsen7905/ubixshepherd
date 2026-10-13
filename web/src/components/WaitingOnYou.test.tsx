import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { WaitingOnYou } from './WaitingOnYou'

const list: Waiting = {
  count: 4,
  counts: { decisions: 1, mrs: 1, ship: 1, next: 1 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 4, repo: 'app', lane: 'a', lane_id: 1, title: 'Which option?' },
    { kind: 'mr', action: 'approve', repo: 'app', lane: 'b', lane_id: 2, mr: 175, title: 'approve MR !175, then retry its approval job' },
    { kind: 'ship', action: 'ship', repo: 'app', lane: 'c', lane_id: 3, title: 'ship 2 commits' },
    { kind: 'next', action: 'review', next_id: 9, title: 'Idea' },
  ],
}

interface Call { path: string; method?: string }

function daemon(w: Waiting, ship?: () => Response) {
  const calls: Call[] = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    calls.push({ path, method: init?.method })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
    if (path === '/v1/waiting') return new Response(JSON.stringify(w))
    if (path.endsWith('/ship')) return ship ? ship() : new Response('{}', { status: 500 })
    if (path.endsWith('/approval')) {
      return new Response(JSON.stringify({ supported: true, mr: 175, mr_url: 'https://forge/175', mr_state: 'opened', sha: 'abcdef0123', approved: false }))
    }
    return new Response('{}')
  })
  return calls
}

afterEach(() => vi.unstubAllGlobals())

describe('WaitingOnYou', () => {
  it('shows nothing while nothing waits', async () => {
    const calls = daemon({ count: 0, counts: { decisions: 0, mrs: 0, ship: 0, next: 0 }, items: [] })
    const { container } = render(<WaitingOnYou />)
    await waitFor(() => expect(calls.some((c) => c.path === '/v1/waiting')).toBe(true))
    expect(container).toBeEmptyDOMElement()
  })

  it('counts in a badge and lists each item with its action', async () => {
    daemon(list)
    render(<WaitingOnYou />)
    await userEvent.click(await screen.findByRole('button', { name: '4 waiting on you' }))
    expect(screen.getByText('Which option?')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Answer' })).toHaveAttribute('href', '#/decisions')
    expect(screen.getByRole('link', { name: 'Review' })).toHaveAttribute('href', '#/next')
    expect(screen.getByRole('button', { name: 'Approve…' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ship' })).toBeInTheDocument()
  })

  it('opens the approval control, which reads the forge only then', async () => {
    const calls = daemon(list)
    render(<WaitingOnYou />)
    await userEvent.click(await screen.findByRole('button', { name: '4 waiting on you' }))
    expect(calls.some((c) => c.path.endsWith('/approval'))).toBe(false)
    await userEvent.click(screen.getByRole('button', { name: 'Approve…' }))
    expect(await screen.findByRole('button', { name: 'Approve !175…' })).toBeInTheDocument()
    expect(calls.some((c) => c.path === '/v1/lanes/2/approval')).toBe(true)
  })

  it('ships the lane on a click and says what happened or why not', async () => {
    const calls = daemon(list, () => new Response(JSON.stringify({ lane: 'c', commits: 2, pushed: true, message: 'pushed 2 commits' })))
    render(<WaitingOnYou />)
    await userEvent.click(await screen.findByRole('button', { name: '4 waiting on you' }))
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(await screen.findByText('pushed 2 commits')).toBeInTheDocument()
    expect(calls.some((c) => c.path === '/v1/lanes/3/ship' && c.method === 'POST')).toBe(true)
  })

  it('shows a refusal to ship', async () => {
    daemon(list, () => new Response(JSON.stringify({ error: 'the gate failed' }), { status: 409 }))
    render(<WaitingOnYou />)
    await userEvent.click(await screen.findByRole('button', { name: '4 waiting on you' }))
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not shipped')
  })
})

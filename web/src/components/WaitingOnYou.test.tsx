import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { refocus } from '../router'
import { WaitingOnYou } from './WaitingOnYou'
import * as step from '../model/waitingStep'

vi.mock('../router', async (orig) => ({ ...(await orig<typeof import('../router')>()), refocus: vi.fn() }))

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
    expect(screen.getByRole('link', { name: 'Answer' })).toHaveAttribute('href', '#/decisions?d=4')
    expect(screen.getByRole('link', { name: 'Review' })).toHaveAttribute('href', '#/next?item=9')
    expect(screen.getByRole('button', { name: 'Approve…' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ship' })).toBeInTheDocument()
  })

  it('asks the page to focus again when the link points at the hash already shown', async () => {
    daemon(list)
    render(<WaitingOnYou />)
    await userEvent.click(await screen.findByRole('button', { name: '4 waiting on you' }))
    const link = screen.getByRole('link', { name: 'Review' })
    vi.mocked(refocus).mockClear()
    await userEvent.click(link)
    expect(refocus).not.toHaveBeenCalled()
    window.location.hash = '#/next?item=9'
    await userEvent.click(link)
    expect(refocus).toHaveBeenCalledTimes(1)
    window.location.hash = ''
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

  it('steps through every waiting item with the arrows, wrapping at the ends', async () => {
    const withQuestion: Waiting = {
      count: 2,
      counts: { decisions: 1, mrs: 0, ship: 0, next: 0, questions: 1 },
      items: [
        { kind: 'question', action: 'reply', question_id: 5, turn: 11, title: 'Ship it?' },
        { kind: 'decision', action: 'answer', decision_id: 4, title: 'Which option?' },
      ],
    }
    daemon(withQuestion)
    const seen: string[] = []
    const on = () => void seen.push(step.takeStep()?.kind ?? 'none')
    window.addEventListener(step.STEP_EVENT, on)
    render(<WaitingOnYou />)
    await screen.findByRole('button', { name: '2 waiting on you' })
    const down = screen.getByRole('button', { name: 'Next item waiting on you' })
    const up = screen.getByRole('button', { name: 'Previous item waiting on you' })
    await userEvent.click(down)
    await userEvent.click(down)
    await userEvent.click(down)
    await userEvent.click(up)
    expect(seen).toEqual(['question', 'decision', 'question', 'decision'])
    window.removeEventListener(step.STEP_EVENT, on)
    window.location.hash = ''
  })
})

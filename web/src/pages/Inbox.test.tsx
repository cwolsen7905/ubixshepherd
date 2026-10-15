import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { decision, spend, status } from '../test/fixtures'
import type { Waiting } from '../api/types'
import { ApiError, waitingOnYou } from '../api/client'
import { InboxPage } from './Inbox'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const waiting: Waiting = {
  count: 6,
  counts: { decisions: 1, mrs: 1, ship: 0, next: 1, blocked: 1 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 7, lane: 'a', title: 'Which store?' },
    { kind: 'mr', action: 'approve', lane_id: 3, lane: 'b', title: 'Add retries', mr: 12 },
    { kind: 'next', action: 'review', next_id: 5, title: 'Proposed item' },
    { kind: 'ship', action: 'ship', lane_id: 6, lane: 'd', title: 'Ship lane d' },
    { kind: 'blocked', action: 'open', lane_id: 4, lane: 'c', title: 'Lane c is blocked' },
    { kind: 'request', action: 'route', request_id: 9, title: 'Needs a lane' },
  ],
}

async function show(kind?: string) {
  const api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    decisions: async (s: string) => (s === 'open' ? [decision({ id: 7 })] : []),
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after: number) => ({ items: [], events: [], last: after }),
    next: async () => [],
  } as unknown as Api
  const live = new Live(api, still, async () => waiting)
  await act(async () => {
    await live.refresh()
    await live.readWaiting()
  })
  render(
    <LiveProvider live={live}>
      <InboxPage kind={kind} />
    </LiveProvider>,
  )
}

describe('InboxPage', () => {
  beforeEach(() => {
    window.location.hash = '#/inbox'
  })

  it('groups what waits by what the person does', async () => {
    await show()
    expect(screen.getByRole('heading', { name: /Answer/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Approve/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Broken/ })).toBeInTheDocument()
  })

  it('filters to one kind', async () => {
    await show('decision')
    expect(screen.getByText('Which store?')).toBeInTheDocument()
    expect(screen.queryByText('Add retries')).toBeNull()
  })

  it('opens a decision row in the inspector', async () => {
    await show()
    const group = screen.getByRole('heading', { name: /Answer/ }).closest('section') as HTMLElement
    await userEvent.click(within(group).getByRole('button', { name: /Which store/ }))
    expect(decodeURIComponent(window.location.hash)).toContain('open=decision:7')
  })

  it('opens a merge request row at its lane', async () => {
    await show()
    await userEvent.click(screen.getByRole('button', { name: /Add retries/ }))
    expect(decodeURIComponent(window.location.hash)).toContain('open=lane:3')
  })

  it('shows how to route a request that has no lane', async () => {
    await show()
    expect(screen.getByText(/shepherd request route 9/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Needs a lane/ })).toBeNull()
  })

  it('scrolls a decision deep link to the middle and marks it', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    window.location.hash = '#/inbox?kind=decision&open=decision:7'
    await show('decision')
    const row = document.querySelector('.inbox-row.is-focus-target') as HTMLElement
    expect(row).not.toBeNull()
    expect(scroll).toHaveBeenCalledWith({ block: 'center' })
  })

  it('focuses the inspector when the open row is chosen again', async () => {
    window.location.hash = '#/inbox?open=lane:3'
    await show()
    const h = document.createElement('h2')
    h.id = 'inspector-title'
    h.tabIndex = -1
    document.body.append(h)
    await userEvent.click(screen.getByRole('button', { name: /Add retries/ }))
    expect(h).toHaveFocus()
    h.remove()
  })

  it('ships a lane in place and says what happened', async () => {
    const ship = vi.spyOn(waitingOnYou, 'ship').mockResolvedValue({ lane: 'd', commits: 1, pushed: true, message: 'Pushed 1 commit.' })
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(ship).toHaveBeenCalledWith(6)
    expect(await screen.findByRole('status')).toHaveTextContent('Pushed 1 commit.')
    ship.mockRestore()
  })

  it('shows a refused ship as an alert', async () => {
    const ship = vi.spyOn(waitingOnYou, 'ship').mockRejectedValue(new ApiError(409, 'gate failed'))
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not shipped: gate failed')
    ship.mockRestore()
  })

  it('moves the cursor with j', async () => {
    await show()
    const at = () => document.querySelector('.is-cursor .inbox-title')?.textContent
    expect(at()).toBeUndefined()
    await userEvent.keyboard('j')
    expect(at()).toBe('Which store?')
    await userEvent.keyboard('j')
    expect(at()).toBe('Add retries')
    await userEvent.keyboard('k')
    expect(at()).toBe('Which store?')
  })
})

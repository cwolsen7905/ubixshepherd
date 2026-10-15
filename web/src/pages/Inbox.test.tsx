import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { decision, spend, status } from '../test/fixtures'
import type { Waiting } from '../api/types'
import { InboxPage } from './Inbox'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const waiting: Waiting = {
  count: 4,
  counts: { decisions: 1, mrs: 1, ship: 0, next: 1, blocked: 1 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 7, lane: 'a', title: 'Which store?' },
    { kind: 'mr', action: 'approve', lane_id: 3, lane: 'b', title: 'Add retries', mr: 12 },
    { kind: 'next', action: 'review', next_id: 5, title: 'Proposed item' },
    { kind: 'blocked', action: 'open', lane_id: 4, lane: 'c', title: 'Lane c is blocked' },
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

  it('moves the cursor with j', async () => {
    await show()
    await userEvent.keyboard('j')
    expect(document.querySelector('.is-cursor')).not.toBeNull()
  })
})

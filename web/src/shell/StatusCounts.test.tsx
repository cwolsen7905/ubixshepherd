import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { popLists, cutWord, type PopItem } from './statusItems'
import { StatusCounts } from './StatusCounts'
import { lane } from '../test/fixtures'

const item = (n: number): PopItem => ({
  key: `k${n}`, glyph: '?', tone: 'warn', title: `Question ${n}`, full: `Question ${n} in full`, where: `lane-${n}`, age: `${n}m`, href: `#/decisions?d=${n}`,
})
const none = { running: [], waiting: [], broken: [], routing: [] }

describe('StatusCounts', () => {
  it('shows all four counts, and a zero is faint, never hidden', () => {
    render(<StatusCounts counts={{ running: 2, waiting: 0, broken: 0, routing: 1 }} lists={none} />)
    const group = screen.getByRole('group', { name: 'Status counts' })
    expect(within(group).getAllByRole('link')).toHaveLength(4)
    expect(screen.getByRole('link', { name: '2 running' })).not.toHaveClass('is-zero')
    expect(screen.getByRole('link', { name: '0 waiting on you' })).toHaveClass('is-zero')
    expect(screen.getByRole('link', { name: '0 broken' })).toHaveTextContent('0')
    expect(screen.getByRole('link', { name: '1 needing routing' })).not.toHaveClass('is-zero')
  })

  it('links running and broken to the Board and waiting and routing to the Inbox', () => {
    render(<StatusCounts counts={{ running: 1, waiting: 1, broken: 1, routing: 1 }} lists={none} />)
    expect(screen.getByRole('link', { name: '1 running' })).toHaveAttribute('href', '#/board')
    expect(screen.getByRole('link', { name: '1 broken' })).toHaveAttribute('href', '#/board')
    expect(screen.getByRole('link', { name: '1 waiting on you' })).toHaveAttribute('href', '#/inbox')
    expect(screen.getByRole('link', { name: '1 needing routing' })).toHaveAttribute('href', '#/inbox')
  })

  it('opens a popover on hover with at most five items and a link to the rest', async () => {
    const user = userEvent.setup()
    const waiting = Array.from({ length: 7 }, (_, i) => item(i + 1))
    render(<StatusCounts counts={{ running: 0, waiting: 7, broken: 0, routing: 0 }} lists={{ ...none, waiting }} />)
    expect(screen.queryByText('7 waiting on you')).toBeNull()
    await user.hover(screen.getByRole('link', { name: '7 waiting on you' }))
    expect(screen.getByText('7 waiting on you')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /Question/ })).toHaveLength(5)
    expect(screen.getByRole('link', { name: /Question 2/ })).toHaveAttribute('title', 'Question 2 in full')
    expect(screen.getByRole('link', { name: /Question 1/ })).toHaveAttribute('href', '#/decisions?d=1')
    expect(screen.getByRole('link', { name: 'See all 7 →' })).toHaveAttribute('href', '#/inbox')
  })

  it('has no "See all" link for five or fewer, and says so when nothing is there', async () => {
    const user = userEvent.setup()
    render(<StatusCounts counts={{ running: 0, waiting: 5, broken: 0, routing: 0 }} lists={{ ...none, waiting: [1, 2, 3, 4, 5].map(item) }} />)
    await user.hover(screen.getByRole('link', { name: '5 waiting on you' }))
    expect(screen.queryByText(/See all/)).toBeNull()
    await user.unhover(screen.getByRole('link', { name: '5 waiting on you' }))
    await user.hover(screen.getByRole('link', { name: '0 running' }))
    expect(screen.getByText('Nothing running')).toBeInTheDocument()
  })

  it('opens on Tab and closes on Escape', async () => {
    const user = userEvent.setup()
    render(<StatusCounts counts={{ running: 0, waiting: 1, broken: 0, routing: 0 }} lists={{ ...none, waiting: [item(1)] }} />)
    await user.tab()
    await user.tab()
    expect(screen.getByRole('link', { name: '1 waiting on you' })).toHaveFocus()
    expect(screen.getByText('1 waiting on you', { selector: '.sh-count-pop-h' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(screen.queryByText('1 waiting on you', { selector: '.sh-count-pop-h' })).toBeNull()
  })

  it('closes a moment after the pointer leaves', async () => {
    const user = userEvent.setup()
    render(<StatusCounts counts={{ running: 0, waiting: 1, broken: 0, routing: 0 }} lists={{ ...none, waiting: [item(1)] }} />)
    const link = screen.getByRole('link', { name: '1 waiting on you' })
    await user.hover(link)
    await user.unhover(link)
    await waitFor(() => expect(screen.queryByText('Question 1')).toBeNull())
  })

  it('hands the count to a sheet instead of a link at 390', async () => {
    const user = userEvent.setup()
    const onSheet = vi.fn()
    render(<StatusCounts counts={{ running: 0, waiting: 3, broken: 0, routing: 0 }} lists={none} sheet onSheet={onSheet} />)
    await user.click(screen.getByRole('link', { name: '3 waiting on you' }))
    expect(onSheet).toHaveBeenCalledWith('waiting')
  })
})

describe('popLists', () => {
  const waiting: Waiting = {
    count: 3,
    counts: { decisions: 1, mrs: 0, ship: 0, next: 0, requests: 1, blocked: 1 },
    items: [
      { kind: 'decision', action: 'answer', decision_id: 4, title: 'Which pagination style should the export use for very large result sets?', lane: 'feat/export', since: '2026-10-10T11:00:00Z' },
      { kind: 'request', action: 'route', request_id: 9, title: 'Needs a reviewer', lane: 'fix/login' },
      { kind: 'blocked', action: 'open', lane_id: 7, lane: 'feat/blocked', title: 'Blocked by lane feat/other' },
    ],
  }
  const snap = { lanes: [lane({ id: 7, name: 'feat/blocked' })], runs: [], decisions: [], requests: [], seen: { until: '2026-10-01T00:00:00Z', keys: new Set<string>() }, waiting }

  it('builds each list from the one waiting list, cut at a whole word', () => {
    const l = popLists(snap, Date.parse('2026-10-10T12:00:00Z'))
    expect(l.waiting).toHaveLength(1)
    expect(l.waiting[0]?.href).toBe('#/decisions?d=4')
    expect(l.waiting[0]?.age).toBe('1h')
    expect(l.waiting[0]?.title.endsWith('…')).toBe(true)
    expect(l.waiting[0]?.title.length).toBeLessThanOrEqual(45)
    expect(l.waiting.map((x) => x.full)).not.toContain('Needs a reviewer')
    expect(l.waiting.map((x) => x.full)).not.toContain('Blocked by lane feat/other')
    expect(l.routing.map((x) => x.full)).toEqual(['Needs a reviewer'])
    expect(l.broken.map((x) => x.full)).toContain('Blocked by lane feat/other')
  })

  it('shows a merged lane with undecided files, and never throws on a kind it does not know', () => {
    const items = [
      { kind: 'artifact', action: 'open', lane_id: 7, lane: 'feat/done', title: 'Undecided files hold feat/done open' },
      { kind: 'from-a-newer-daemon', action: 'open', title: 'Something new' },
    ] as unknown as Waiting['items']
    const s = { ...snap, waiting: { ...waiting, count: 2, items } }
    const l = popLists(s, Date.parse('2026-10-10T12:00:00Z'))
    expect(l.waiting.map((x) => [x.glyph, x.full])).toEqual([
      ['▤', 'Undecided files hold feat/done open'],
      ['○', 'Something new'],
    ])
  })

  it('cuts at a whole word', () => {
    expect(cutWord('short')).toBe('short')
    expect(cutWord('one two three four', 10)).toBe('one two…')
  })
})

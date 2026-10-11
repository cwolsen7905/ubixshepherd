import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RunHistory } from '../api/types'
import { busyDay, BUSY_NOW } from '../model/timeline.fixtures'
import type { TimelineRange } from '../router'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { FETCH_GAP } from '../timeline/data'
import { status } from '../test/fixtures'
import { TimelinePage } from './Timeline'

const busy = busyDay()
const { src } = busy

/** The daemon's reads, and what the page asked of the history. */
function daemon(o: { history?: Partial<RunHistory>; fail?: boolean; none?: boolean } = {}) {
  const seen: URL[] = []
  vi.stubGlobal('fetch', async (path: string) => {
    const url = new URL(path, 'http://x')
    seen.push(url)
    if (o.fail && url.pathname.startsWith('/v1/history')) return new Response(JSON.stringify({ error: 'boom' }), { status: 500 })
    if (url.pathname === '/v1/history/runs') {
      const since = Date.parse(url.searchParams.get('since') ?? '')
      const runs = (o.none ? [] : src.historyRuns ?? []).filter((r) => Date.parse(r.started) >= since)
      const body: RunHistory = { runs, count: runs.length, cost_usd: 1, credits: 0, agents: [], lanes: [], ...o.history }
      return new Response(JSON.stringify(body), { status: 200 })
    }
    if (url.pathname === '/v1/history/lanes') return new Response(JSON.stringify(o.none ? [] : src.closedLanes), { status: 200 })
    if (url.pathname === '/v1/decisions') return new Response(JSON.stringify(o.none ? [] : src.answered), { status: 200 })
    return new Response('{}', { status: 404 })
  })
  return seen
}

/** A clock whose timers the test runs by hand, so the feed moves when told to. */
function manual() {
  const timers: (() => void)[] = []
  const clock: Clock = { now: () => Date.now(), hidden: () => false, setTimeout: (fn) => timers.push(fn), clearTimeout: () => {} }
  return { clock, timers }
}

function liveWith(clock: Clock, feedLast = { n: 1 }, over: Partial<Api> = {}) {
  const api: Api = {
    status: async () => status(),
    lanes: async () => src.lanes,
    runs: async () => src.runs,
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => src.decisions,
    requests: async () => src.requests,
    spend: async () => src.spend as NonNullable<typeof src.spend>,
    feedLatest: async () => ({ items: [], events: [], last: feedLast.n }),
    feed: async (after) =>
      after >= feedLast.n
        ? { items: [], events: [], last: after }
        : {
            items: Array.from({ length: feedLast.n - after }, (_, i) => ({ id: after + i + 1, kind: 'commit', text: 'commit', created: new Date(BUSY_NOW).toISOString() })),
            events: Array.from({ length: feedLast.n - after }, () => 'commit'),
            last: feedLast.n,
          },
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
    ...over,
  }
  return new Live(api, clock)
}

async function show(props: { range?: TimelineRange; agent?: string } = {}, live?: Live) {
  const l = live ?? liveWith(manual().clock)
  await act(() => l.refresh())
  const view = render(
    <LiveProvider live={l}>
      <TimelinePage {...props} />
    </LiveProvider>,
  )
  return { ...view, live: l }
}

function narrow(on: boolean) {
  vi.stubGlobal('matchMedia', (query: string) => ({
    matches: on && query.includes('max-width'), media: query, addEventListener: () => {}, removeEventListener: () => {},
  }))
}

beforeEach(() => {
  window.localStorage.clear()
  daemon()
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(BUSY_NOW)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the Timeline page', () => {
  it('shows a heading and one line of what is going on', async () => {
    await show()
    expect(screen.getByRole('heading', { level: 1, name: 'Timeline' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('5 running, 2 waiting on you, 3 merged, $18.40 of $25.00 today')).toBeInTheDocument())
  })

  it('draws a track for every lane and a bar for every run once the history has loaded', async () => {
    const { container } = await show()
    await waitFor(() => expect(container.querySelectorAll('.tl-row')).toHaveLength(24))
    await waitFor(() => expect(container.querySelectorAll('a.tl-run, a.tl-density').length).toBeGreaterThan(150))
    // The straddling run is only in the history.
    expect(container.querySelector(`a[data-run="${busy.ids.straddling}"]`)).not.toBeNull()
  })

  it('links each range, keeping the agent filter, and marks the current one', async () => {
    await show({ range: '24h', agent: 'claude' })
    const nav = screen.getByRole('navigation', { name: 'Time range' })
    expect(within(nav).getByRole('link', { name: 'Today' })).toHaveAttribute('href', '#/timeline?range=today&agent=claude')
    expect(within(nav).getByRole('link', { name: 'Last 7 days' })).toHaveAttribute('href', '#/timeline?range=week&agent=claude')
    expect(within(nav).getByRole('link', { name: 'Last 24 hours' })).toHaveAttribute('aria-current', 'true')
  })

  it('asks the history for the range from midnight, or six hours back, with the daemon’s largest page', async () => {
    const seen = daemon()
    await show()
    await waitFor(() => expect(seen.some((u) => u.pathname === '/v1/history/runs')).toBe(true))
    const req = seen.find((u) => u.pathname === '/v1/history/runs') as URL
    expect(Date.parse(req.searchParams.get('since') as string)).toBe(new Date(2026, 9, 10, 0, 0).getTime() - 6 * 3600_000)
    expect(req.searchParams.get('limit')).toBe('1000')
    expect(req.searchParams.get('workspace_id')).toBe('1')
  })

  it('asks for a week as a week', async () => {
    const seen = daemon()
    await show({ range: 'week' })
    await waitFor(() => expect(seen.some((u) => u.pathname === '/v1/history/runs')).toBe(true))
    const req = seen.find((u) => u.pathname === '/v1/history/runs') as URL
    const since = Date.parse(req.searchParams.get('since') as string)
    expect(BUSY_NOW - since).toBeGreaterThanOrEqual(7 * 24 * 3600_000)
    expect(BUSY_NOW - since).toBeLessThan(8 * 24 * 3600_000)
  })

  it('dims the other agents for a filter and says which is chosen', async () => {
    const { container } = await show({ agent: 'copilot' })
    await waitFor(() => expect(container.querySelectorAll('a.tl-run').length).toBeGreaterThan(0))
    expect(container.querySelectorAll('a.tl-run.tl-agent-copilot.is-dim')).toHaveLength(0)
    expect(container.querySelectorAll('a.tl-run.tl-agent-claude.is-dim').length).toBeGreaterThan(0)
    expect(screen.getByRole('link', { name: 'copilot' })).toHaveAttribute('aria-current', 'true')
  })

  it('offers the idle lanes, and shows them when asked', async () => {
    const user = userEvent.setup()
    const { container } = await show()
    await waitFor(() => expect(container.querySelectorAll('.tl-row')).toHaveLength(24))
    await user.click(screen.getByRole('button', { name: 'Show idle lanes (1)' }))
    expect(container.querySelectorAll('.tl-row')).toHaveLength(25)
    expect(screen.getByRole('button', { name: 'Hide idle lanes (1)' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('holds every run in a table, sortable, so nothing is reachable only by hovering', async () => {
    const user = userEvent.setup()
    await show()
    await waitFor(() => expect(screen.getByText('View as table')).toBeInTheDocument())
    await user.click(screen.getByText('View as table'))
    const table = await screen.findByRole('table', { name: 'Runs in the timeline’s range' })
    // Plain DOM reads: a role query over hundreds of rows is slow in jsdom.
    const ids = () => [...table.querySelectorAll('tbody tr')].map((r) => Number(r.children[1]?.textContent))
    const newest = ids()
    expect(newest.length).toBeGreaterThan(100)
    await user.click(within(table).getByRole('button', { name: /^Run/ }))
    expect(ids()).toEqual([...ids()].sort((a, b) => b - a))
    await user.click(within(table).getByRole('button', { name: /^Run/ }))
    expect(ids()).toEqual([...ids()].sort((a, b) => a - b))
    expect(table.querySelector('tbody tr td:nth-child(2) a')).toHaveAttribute('href', expect.stringMatching(/^#\/runs\/\d+\/log$/))
  })

  it('says so when nothing ran, with what to do about it', async () => {
    daemon({ none: true })
    const live = liveWith(manual().clock, { n: 1 }, {
      lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [],
    })
    await show({}, live)
    expect(await screen.findByText(/No run started today\./)).toBeInTheDocument()
    expect(screen.getByText('shepherd lane run')).toBeInTheDocument()
  })

  it('names the cut when the daemon holds more runs than it listed', async () => {
    daemon({ history: { count: 1340 } })
    await show()
    expect(await screen.findByText(/Showing the newest \d+ runs of 1,340 today\./)).toBeInTheDocument()
  })

  it('says when the history could not be read, and keeps drawing the snapshot', async () => {
    daemon({ fail: true })
    const { container } = await show()
    expect(await screen.findByText(/Could not read the history: boom/)).toBeInTheDocument()
    expect(container.querySelectorAll('.tl-row').length).toBeGreaterThan(0)
  })

  it('turns the range into a select on a narrow screen', async () => {
    narrow(true)
    await show({ range: 'week' })
    expect(screen.getByRole('combobox', { name: 'Range' })).toHaveValue('week')
    expect(screen.queryByRole('navigation', { name: 'Time range' })).toBeNull()
  })
})

describe('reading the history again', () => {
  beforeEach(() => {
    vi.useRealTimers()
    vi.useFakeTimers({ toFake: ['Date', 'setTimeout', 'clearTimeout'] })
    vi.setSystemTime(BUSY_NOW)
  })

  const historyReads = (seen: URL[]) => seen.filter((u) => u.pathname === '/v1/history/runs').length

  it('reads once more when the feed moves, but not more than once in the gap', async () => {
    const seen = daemon()
    const { clock, timers } = manual()
    const feed = { n: 1 }
    const live = liveWith(clock, feed)
    live.start()
    await show({}, live)
    await vi.advanceTimersByTimeAsync(0)
    expect(historyReads(seen)).toBe(1)

    // The feed moves twice in quick succession: one more read, after the gap.
    feed.n = 2
    await act(async () => {
      await timers.shift()?.()
    })
    await vi.advanceTimersByTimeAsync(10)
    expect(historyReads(seen)).toBe(1)
    feed.n = 3
    await act(async () => {
      await timers.shift()?.()
    })
    await vi.advanceTimersByTimeAsync(FETCH_GAP - 100)
    expect(historyReads(seen)).toBe(1)
    await vi.advanceTimersByTimeAsync(200)
    expect(historyReads(seen)).toBe(2)
    live.stop()
  })
})

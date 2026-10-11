import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { BoardItem } from '../model/board'
import { COLLAPSED_KEY, VIEW_KEY } from '../model/projects'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, project, run, spend, status, unassigned } from '../test/fixtures'
import { BoardPage, type DismissedControl } from './Board'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const items: BoardItem[] = [
  { key: 'lane:1', group: 'working', name: 'feat/login', laneId: 1, repo: 'acme-api', lane: lane(), detail: 'run 10', at: '2026-10-08T10:00:00Z' },
  { key: 'lane:2', group: 'working', name: 'docs-pass', laneId: 2, repo: 'docs', lane: lane({ id: 2, name: 'docs-pass', repo: 'docs' }), detail: 'run 11', at: '2026-10-08T10:01:00Z' },
]

function daemon(projects = [project({ lanes: 2, spend_today: 1.5 }), unassigned({ spend_today: 0.25 })]) {
  vi.stubGlobal('fetch', async (path: string) => {
    if (path === '/v1/projects') return new Response(JSON.stringify({ projects }), { status: 200 })
    return new Response('[]', { status: 200 })
  })
}

async function show(shown: BoardItem[] = items, over: Partial<Api> = {}, dismissed?: DismissedControl) {
  const api: Api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
    ...over,
  }
  return await mount(api, shown, dismissed)
}

async function mount(api: Api, shown: BoardItem[], dismissed?: DismissedControl) {
  const live = new Live(api, still)
  await act(() => live.refresh())
  await act(async () => {
    render(
      <LiveProvider live={live}>
        <BoardPage items={shown} dismissed={dismissed} />
      </LiveProvider>,
    )
  })
}

beforeEach(() => window.localStorage.clear())
afterEach(() => vi.unstubAllGlobals())

describe('BoardPage failures', () => {
  it('shows a broken lane’s reason with its fixes beside the row', async () => {
    daemon([unassigned()])
    const failed = run({
      id: 12, state: 'failed',
      diagnosis: {
        id: 1, run_id: 12, kind: 'quota', summary: 'claude hit its usage limit', evidence_omitted: true, created: '2026-10-08T10:04:00Z',
        fixes: [{ id: 'other', label: 'Retry with another agent', op: 'retry_other', needs_person: true, desk_ok: false }],
      },
    })
    await show([{ key: 'lane:1', group: 'broken', name: 'feat/login', laneId: 1, repo: 'acme-api', lane: lane(), run: failed, detail: 'run 12 failed', at: '2026-10-08T10:00:00Z' }])
    expect(screen.getByText('claude hit its usage limit').tagName).toBe('SUMMARY')
    expect(screen.getByRole('button', { name: /Retry with another agent/ })).toBeInTheDocument()
  })
})

describe('BoardPage by project', () => {
  it('groups lanes under their project with a summary, Unassigned last', async () => {
    daemon()
    await show()
    const shop = screen.getByRole('region', { name: /shop/ })
    expect(within(shop).getByText('feat/login')).toBeInTheDocument()
    expect(within(shop).queryByText('docs-pass')).toBeNull()
    expect(within(shop).getByText('2 open · 1 running · $1.50 today')).toBeInTheDocument()
    const un = screen.getByRole('region', { name: /Unassigned/ })
    expect(within(un).getByText('docs-pass')).toBeInTheDocument()
    const order = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(order.findIndex((t) => t?.includes('shop'))).toBeLessThan(order.findIndex((t) => t?.includes('Unassigned')))
  })

  it('collapses a group and remembers it', async () => {
    daemon()
    await show()
    await userEvent.click(screen.getByRole('button', { name: /shop/ }))
    expect(screen.queryByText('feat/login')).toBeNull()
    expect(screen.getByText('docs-pass')).toBeInTheDocument()
    expect(JSON.parse(window.localStorage.getItem(COLLAPSED_KEY) ?? '[]')).toEqual(['shop'])
    expect(screen.getByRole('button', { name: /shop/ })).toHaveAttribute('aria-expanded', 'false')
  })

  it('flattens on request and remembers the choice', async () => {
    daemon()
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Flat' }))
    expect(window.localStorage.getItem(VIEW_KEY)).toBe('flat')
    expect(screen.queryByRole('region', { name: /shop/ })).toBeNull()
    expect(screen.getByText('feat/login')).toBeInTheDocument()
    expect(screen.getByText('docs-pass')).toBeInTheDocument()
  })

  it('opens flat when the stored choice is flat, and when no project exists', async () => {
    window.localStorage.setItem(VIEW_KEY, 'flat')
    daemon()
    await show()
    expect(screen.queryByRole('region', { name: /shop/ })).toBeNull()
    expect(screen.getByRole('button', { name: 'Flat' })).toHaveAttribute('aria-pressed', 'true')
  })

  it('stays flat, with no toggle, when only Unassigned exists', async () => {
    daemon([unassigned()])
    await show()
    expect(screen.queryByRole('button', { name: 'Flat' })).toBeNull()
    expect(screen.getByText('feat/login')).toBeInTheDocument()
  })
})

describe('BoardPage dismiss', () => {
  const failed = run({ state: 'failed' })
  const broken: BoardItem = {
    key: 'lane:1', group: 'broken', name: 'feat/login', laneId: 1, repo: 'acme-api', lane: lane(), run: failed,
    detail: 'run 10 failed', at: '2026-10-08T10:04:00Z',
  }

  it('dismisses a failed run from its card, then reloads the lists', async () => {
    const dismissRun = vi.fn(async (id: number) => ({ ...failed, id, dismissed: '2026-10-08T10:06:00Z' }))
    const runs = vi.fn(async () => [])
    await show([broken], { dismissRun, runs })
    await userEvent.click(screen.getByRole('button', { name: 'Dismiss run 10' }))
    expect(dismissRun).toHaveBeenCalledWith(10)
    expect(runs.mock.calls.length).toBeGreaterThan(1)
  })

  it('offers Undismiss on a dismissed run shown by the toggle', async () => {
    const undismissRun = vi.fn(async (id: number) => ({ ...failed, id }))
    const item = { ...broken, run: { ...failed, dismissed: '2026-10-08T10:06:00Z' }, detail: 'run 10 failed, dismissed' }
    await show([item], { undismissRun }, { count: 1, show: true, onShow: () => {} })
    expect(screen.getByRole('button', { name: 'Hide dismissed (1)' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Undismiss run 10' }))
    expect(undismissRun).toHaveBeenCalledWith(10)
  })

  it('offers the toggle only when something is dismissed', async () => {
    const onShow = vi.fn()
    await show([], {}, { count: 2, show: false, onShow })
    await userEvent.click(screen.getByRole('button', { name: 'Show dismissed (2)' }))
    expect(onShow).toHaveBeenCalledWith(true)
  })
})

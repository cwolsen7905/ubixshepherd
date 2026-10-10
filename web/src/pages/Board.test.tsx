import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { BoardItem } from '../model/board'
import { COLLAPSED_KEY, VIEW_KEY } from '../model/projects'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, project, spend, status, unassigned } from '../test/fixtures'
import { BoardPage } from './Board'

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

async function show() {
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
    answer: async () => { throw new Error('unused') },
  }
  const live = new Live(api, still)
  await act(() => live.refresh())
  await act(async () => {
    render(
      <LiveProvider live={live}>
        <BoardPage items={items} />
      </LiveProvider>,
    )
  })
}

beforeEach(() => window.localStorage.clear())
afterEach(() => vi.unstubAllGlobals())

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

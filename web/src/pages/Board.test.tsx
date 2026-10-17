import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { BoardItem } from '../model/board'
import { VIEW_KEY } from '../model/projects'
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
    expect(shop.compareDocumentPosition(un) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
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

describe('BoardPage closing', () => {
  it('marks a finished lane, and disables closing one with a run going', async () => {
    vi.stubGlobal('fetch', async (path: string) => {
      if (path === '/v1/fold/review') {
        return new Response(JSON.stringify([
          { branch: 'feat/login', worktree: '/tmp/a', lane: 'feat/login', verdict: 'finished', why: 'every change on it is in origin/dev', in_base: true },
          { branch: 'docs-pass', worktree: '/tmp/b', lane: 'docs-pass', verdict: 'finished', why: 'every change on it is in origin/dev', in_base: true },
        ]), { status: 200 })
      }
      return new Response(JSON.stringify(path === '/v1/projects' ? { projects: [unassigned()] } : []), { status: 200 })
    })
    const running = run({ id: 11, lane_id: 2, state: 'running' })
    await show([
      { key: 'lane:1', group: 'done', name: 'feat/login', laneId: 1, repo: 'acme-api', lane: lane(), run: run({ id: 10, state: 'succeeded' }), detail: 'run 10', at: '2026-10-08T10:00:00Z' },
      { key: 'lane:2', group: 'working', name: 'docs-pass', laneId: 2, repo: 'docs', lane: lane({ id: 2, name: 'docs-pass', repo: 'docs', repo_id: 1 }), run: running, detail: 'run 11', at: '2026-10-08T10:01:00Z' },
    ])
    expect(await screen.findAllByText(/finished: ready to close/)).toHaveLength(2)
    const row = (name: string) => within(screen.getByText(name).closest('li') as HTMLElement)
    await userEvent.click(row('feat/login').getByRole('button', { name: '…' }))
    expect(row('feat/login').getByRole('button', { name: 'Close lane' })).toBeEnabled()
    await userEvent.click(row('docs-pass').getByRole('button', { name: '…' }))
    expect(row('docs-pass').getByRole('button', { name: 'Close lane' })).toBeDisabled()
  })

  it('shows no overflow menu under a lane the review has not finished', async () => {
    daemon([unassigned()])
    await show([
      { key: 'lane:1', group: 'working', name: 'feat/login', laneId: 1, repo: 'acme-api', lane: lane(), run: run({ id: 10, state: 'running' }), detail: 'run 10', at: '2026-10-08T10:00:00Z' },
    ])
    const row = within(screen.getByText('feat/login').closest('li') as HTMLElement)
    // No review verdict arrives (the stub answers []), so neither the finished mark
    // nor the '…' overflow menu renders: the row ends at the lane row.
    expect(row.queryByRole('button', { name: '…' })).toBeNull()
    expect(row.queryByText(/finished: ready to close/)).toBeNull()
  })
})

describe('BoardPage rows', () => {
  const one = (r: ReturnType<typeof run>) => [
    { key: 'lane:1', group: 'working', name: 'feat/login', laneId: 1, repo: 'acme-api', agent: 'claude', lane: lane(), run: r, detail: 'run 10', at: '2026-10-08T10:00:00Z' } as BoardItem,
  ]
  const rowOf = () => screen.getByText('feat/login').closest('[data-component="ListRow"]') as HTMLElement

  it('has two lines: the lane over agent, model and status', async () => {
    daemon([unassigned()])
    await show(one(run({ model: 'claude-sonnet' })))
    expect(within(rowOf()).getByText('feat/login')).toBeInTheDocument()
    expect(within(rowOf()).getByText('claude · claude-sonnet · run 10')).toBeInTheDocument()
  })

  it('shortens a model id to the part after the last slash', async () => {
    daemon([unassigned()])
    await show(one(run({ model: 'openrouter/moonshotai/kimi-k2.7-code' })))
    expect(within(rowOf()).getByText('claude · kimi-k2.7-code · run 10')).toBeInTheDocument()
  })

  it('leaves the model out when the run has none', async () => {
    daemon([unassigned()])
    await show(one(run({ model: undefined })))
    expect(within(rowOf()).getByText('claude · run 10')).toBeInTheDocument()
  })

  it('shows the merge request and the age in their columns', async () => {
    daemon([unassigned()])
    await show([{ ...one(run())[0] as BoardItem, lane: lane({ mr: 41, mr_state: 'open', pipeline_status: 'passed' }) }])
    expect(within(rowOf()).getByText(/!41/)).toBeInTheDocument()
    expect(rowOf().querySelector('.sh-row-age')?.textContent).toMatch(/^\d+[smhd]$/)
  })
})

describe('BoardPage groups', () => {
  const at = '2026-10-08T10:00:00Z'
  const mk = (id: number, group: BoardItem['group'], name: string): BoardItem => ({
    key: `lane:${id}`, group, name, laneId: id, repo: 'acme-api', lane: lane({ id, name }), detail: 'd', at,
  })

  it('lists needs you, broken, to review and working in that order, each with a count', async () => {
    daemon([unassigned()])
    await show([mk(1, 'working', 'w'), mk(2, 'review', 'r'), mk(3, 'broken', 'b'), mk(4, 'needs', 'n')])
    const names = ['Needs you', 'Broken', 'To review', 'Working']
    const regions = names.map((n) => screen.getByRole('region', { name: new RegExp(`^${n}`) }))
    regions.slice(1).forEach((r, i) => {
      expect((regions[i] as HTMLElement).compareDocumentPosition(r) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    })
    expect(within(regions[0] as HTMLElement).getByText('1')).toBeInTheDocument()
  })

  it('adds held and idle groups for open lanes with nothing on the board, after working', async () => {
    daemon([unassigned()])
    const lanes = [lane({ id: 5, name: 'quiet' }), lane({ id: 6, name: 'waiting' })]
    const runs = [run({ id: 20, lane_id: 6, agent: 'claude', state: 'stopped', ended: '2026-10-08T09:00:00Z' })]
    const holds = [{ agent: 'claude', until: new Date(Date.now() + 3_600_000).toISOString() }]
    await show([mk(1, 'working', 'w')], {
      lanes: async () => lanes,
      runs: async () => runs,
      status: async () => status({ detail: { os: 'linux', arch: 'amd64', running: 0, open_lanes: 2, decisions: 0, quota_holds: holds, spend_usd: 0 } as never }),
    })
    const idle = screen.getByRole('region', { name: /^Idle/ })
    expect(within(idle).getByText('quiet')).toBeInTheDocument()
    const held = screen.getByRole('region', { name: /^Held/ })
    expect(within(held).getByText('waiting')).toBeInTheDocument()
    expect(within(held).getByText(/held: out of quota until/)).toBeInTheDocument()
    const working = screen.getByRole('region', { name: /^Working/ })
    expect(working.compareDocumentPosition(held) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(held.compareDocumentPosition(idle) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByText(/1 held · 1 idle/)).toBeInTheDocument()
  })

  it('ends with the Finished footer', async () => {
    daemon([unassigned()])
    await show([mk(1, 'working', 'w')])
    expect(screen.getByRole('region', { name: /^Finished/ })).toBeInTheDocument()
  })
})

describe('BoardPage quiet projects', () => {
  const quiet = [
    project({ name: 'attic', repos: ['old'], lanes: 0, spend_today: 0 }),
    project({ name: 'barn', repos: ['hay'], lanes: 0, spend_today: 0 }),
    project({ name: 'idle-lane', repos: ['idle'], lanes: 1, spend_today: 0 }),
  ]

  it('has one page head and sentence-case group names', async () => {
    daemon()
    await show()
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe('Board')
    expect(screen.getAllByRole('region', { name: /^Working/ }).length).toBeGreaterThan(0)
  })

  it('folds projects with no lanes and nothing on the board under one head, and opens it on request', async () => {
    daemon([project({ lanes: 2 }), ...quiet, unassigned()])
    await show()
    const fold = screen.getByRole('button', { name: '2 quiet projects' })
    expect(fold).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('region', { name: /attic/ })).toBeNull()
    expect(screen.queryByRole('link', { name: 'attic' })).toBeNull()
    // A project with an open lane keeps its group even with no row.
    expect(screen.getByRole('region', { name: /idle-lane/ })).toBeInTheDocument()
    await userEvent.click(fold)
    expect(fold).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('link', { name: 'attic' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'barn' })).toBeInTheDocument()
  })

  it('shows no fold when every project has something', async () => {
    daemon()
    await show()
    expect(screen.queryByRole('button', { name: /quiet project/ })).toBeNull()
  })
})

describe('BoardPage rows and the inspector', () => {
  afterEach(() => {
    window.location.hash = ''
  })
  it('marks the open row with aria-current and the keyboard cursor with data-selected, apart', async () => {
    window.location.hash = '#/?open=lane%3A2'
    await show()
    const rows = screen.getAllByRole('link').filter((a) => a.getAttribute('data-component') === 'ListRow')
    expect(rows[1]).toHaveAttribute('aria-current', 'true')
    expect(rows[0]).not.toHaveAttribute('aria-current')
    const wraps = rows.map((r) => r.closest('.board-row'))
    expect(wraps[0]).toHaveAttribute('data-selected', 'true')
    expect(wraps[1]).not.toHaveAttribute('data-selected')
  })
})

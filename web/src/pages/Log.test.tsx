import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import type { Diagnosis } from '../api/types'
import { decision, lane, run, spend, status } from '../test/fixtures'
import { runTab } from '../inspector/runPane'
import { LogPage } from './Log'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

describe('the run page', () => {
  const baseApi: Api = {
    status: async () => status(),
    lanes: async () => [lane()],
    runs: async () => [run()],
    run: async () => run(),
    runLog: async () => ({ offset: 0, data: '', size: 0, done: true }) as never,
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 1 }),
    feed: async () => ({ items: [], events: [], last: 1 }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }

  it('opens with a way back to the board', async () => {
    const live = new Live(baseApi, still)
    await act(async () => {
      await live.refresh()
    })
    vi.stubGlobal('fetch', async (u: string) =>
      new Response(JSON.stringify(
        u.includes('/conversation')
          ? { items: [], cursor: 0, more: false, running: false }
          : { offset: 0, data: '', size: 0, done: true }
      ), { status: 200 })
    )
    render(
      <LiveProvider live={live}>
        <LogPage run={1} />
      </LiveProvider>,
    )
    expect(screen.getByRole('link', { name: '← Board' })).toHaveAttribute('href', '#/board')
    vi.unstubAllGlobals()
  })

  it('renders the run pane tabs: Log, Changes, Artifacts', async () => {
    const live = new Live(baseApi, still)
    await act(async () => {
      await live.refresh()
    })
    const f = (p: string) => ({ path: p, status: 'modified', added: 1, removed: 0, in_scope: true })
    const diff = {
      lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'def456', mode: 'stat',
      read_at: '2026-10-08T10:04:00Z', commits_total: 1, out_of_scope: 0, commits: [{ sha: 'abc1234', subject: 'run commit', author: 'a', date: '', files: 1 }],
      committed: [f('docs/x.md')], uncommitted: [f('src/wip.ts')], untracked: [],
    }
    vi.stubGlobal('fetch', async (u: string) => {
      const body = u.includes('/conversation')
        ? { items: [], cursor: 0, more: false, running: false }
        : u.includes('/diff')
          ? diff
          : u.includes('/v1/runs?')
            ? [run()]
            : u.endsWith('/v1/runs/10')
              ? run({ outside: ['docs/x.md'] })
              : u.includes('/log')
                ? { offset: 0, data: '', size: 0, done: true }
                : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    render(
      <LiveProvider live={live}>
        <LogPage run={10} />
      </LiveProvider>,
    )
    expect(await screen.findByRole('tab', { name: 'Log' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Changes' })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Artifacts' })).toBeInTheDocument()
    expect(screen.getByText('Run 10 succeeded')).toBeInTheDocument()
    expect(screen.getByText('acme-api / feat/login')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Changes' }))
    expect(await screen.findByText('run commit')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: /Uncommitted/ })).toHaveTextContent("in the lane's worktree now")
    // The run's own file is marked outside scope by the design row: lantern title, no plain text.
    const row = screen.getByText('docs/x.md').closest('li') as HTMLElement
    expect(row).toHaveClass('is-outside')
    expect(row.title).toMatch(/outside scope/)
    vi.unstubAllGlobals()
  })

  const diagnosis: Diagnosis = {
    id: 7, run_id: 10, kind: 'gate_failed', summary: 'The gate failed: make check exited 2', created: '2026-10-08T10:04:00Z',
    evidence: 'FAIL internal/x', command: 'make check', exit_code: 2, files: [],
    fixes: [{ id: 'continue', op: 'continue', label: 'Continue the session', needs_person: false, desk_ok: true }],
  }

  async function open(over: Parameters<typeof run>[0], extra: Partial<Api> = {}, body: (u: string) => unknown = () => ({})) {
    const r = run({ id: 10, ...over })
    const live = new Live({ ...baseApi, runs: async () => [r], run: async () => r, ...extra }, still)
    await act(async () => {
      await live.refresh()
    })
    vi.stubGlobal('fetch', async (u: string) =>
      new Response(JSON.stringify(
        u.includes('/conversation') ? { items: [], cursor: 0, more: false, running: false }
          : u.includes('/log') ? { offset: 0, data: '', size: 0, done: true }
            : u.endsWith('/v1/runs/10') ? r
              : body(u),
      ), { status: 200 }))
    render(
      <LiveProvider live={live}>
        <LogPage run={10} />
      </LiveProvider>,
    )
    return live
  }
  beforeEach(() => {
    window.localStorage.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows the failure diagnosis and its fix actions', async () => {
    await open({ state: 'failed', diagnosis })
    expect(await screen.findByText('The gate failed: make check exited 2')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Continue the session' })).toBeInTheDocument()
  })

  it('shows no diagnosis for a run without one', async () => {
    await open({ state: 'succeeded' })
    await screen.findByRole('tab', { name: 'Log' })
    expect(screen.queryByRole('button', { name: 'Continue the session' })).toBeNull()
  })

  it('dismisses a failed run and offers Undismiss once it is dismissed', async () => {
    const dismissRun = vi.fn(async () => run({ id: 10, state: 'failed', dismissed: '2026-10-08T10:06:00Z' }))
    await open({ state: 'failed' }, { dismissRun })
    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))
    expect(dismissRun).toHaveBeenCalledWith(10)
  })

  it('shows why a dismiss failed', async () => {
    const dismissRun = vi.fn(async () => { throw new Error('daemon unreachable') })
    await open({ state: 'failed' }, { dismissRun })
    await userEvent.click(await screen.findByRole('button', { name: 'Dismiss' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not dismiss this run: daemon unreachable')
  })

  it('maps the old produced address onto the artifacts tab', () => {
    expect(runTab('produced')).toBe('artifacts')
    expect(runTab('artifacts')).toBe('artifacts')
    expect(runTab('nonsense')).toBe('log')
  })

  it('offers Undismiss for a dismissed run, and nothing for a run that succeeded', async () => {
    const undismissRun = vi.fn(async () => run({ id: 10, state: 'failed' }))
    await open({ state: 'failed', dismissed: '2026-10-08T10:01:00Z' }, { undismissRun })
    await userEvent.click(await screen.findByRole('button', { name: 'Undismiss' }))
    expect(undismissRun).toHaveBeenCalledWith(10)
    expect(screen.getByText(/dismissed from the board/)).toBeInTheDocument()
  })

  it('has no Dismiss for a run that succeeded', async () => {
    await open({ state: 'succeeded' })
    await screen.findByRole('tab', { name: 'Log' })
    expect(screen.queryByRole('button', { name: 'Dismiss' })).toBeNull()
  })

  it('lists the decisions the run asked under Asked', async () => {
    await open({}, {}, (u) => (u.endsWith('/events') ? { decisions: [decision({ id: 5, question: 'Should we use authentication?' })] } : {}))
    await userEvent.click(await screen.findByRole('tab', { name: 'Asked' }))
    expect(await screen.findByRole('link', { name: 'Should we use authentication?' })).toBeInTheDocument()
    expect(screen.getByText(/waiting on you/)).toBeInTheDocument()
  })

  it('has a Conversation tab', async () => {
    await open({})
    expect(await screen.findByRole('tab', { name: 'Conversation' })).toBeInTheDocument()
  })

  it('marks an ended run as seen, and a running one not', async () => {
    const live = await open({ state: 'succeeded' })
    await waitFor(() => expect(live.getSnapshot().seen.keys.has('run:10')).toBe(true))
  })

  it('does not mark a running run as seen', async () => {
    const live = await open({ state: 'running', ended: undefined })
    await screen.findByRole('tab', { name: 'Log' })
    expect(live.getSnapshot().seen.keys.has('run:10')).toBe(false)
  })
})

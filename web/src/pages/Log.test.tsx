import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, run, spend, status } from '../test/fixtures'
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
    expect(screen.getByText('outside scope')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })
})

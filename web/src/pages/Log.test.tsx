import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, feedItem, lane, run, spend, status } from '../test/fixtures'
import { RunInspector } from '../inspector/runPane'
import { LogPage } from './Log'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

describe('the run page', () => {
  it('opens with a way back to the board', async () => {
    const api: Api = {
      status: async () => status(),
      lanes: async () => [lane()],
      runs: async () => [run()],
      run: async () => run(),
      runLog: async () => ({ offset: 0, data: '', size: 0, done: true }) as never,
      runEvents: async () => { throw new Error('unused') },
      decisions: async () => [decision()],
      requests: async () => [],
      next: async () => ({ items: [] }),
      spend: async () => spend(),
      feedLatest: async () => ({ items: [], events: [], last: 1 }),
      feed: async () => ({ items: [feedItem({ id: 1 })], events: [], last: 1 }),
      dismissRun: async () => { throw new Error('unused') },
      undismissRun: async () => { throw new Error('unused') },
      answer: async () => { throw new Error('unused') },
    }
    const live = new Live(api, still)
    await act(async () => {
      await live.refresh()
    })
    vi.stubGlobal('fetch', async (u: string) => new Response(JSON.stringify(u.includes('/conversation') ? { items: [], cursor: 0, more: false, running: false } : { offset: 0, data: '', size: 0, done: true }), { status: 200 }))
    render(
      <LiveProvider live={live}>
        <LogPage run={1} />
      </LiveProvider>,
    )
    expect(screen.getByRole('link', { name: '← Board' })).toHaveAttribute('href', '#/board')
    vi.unstubAllGlobals()
  })

  it('narrows its changes to the run and shows the worktree only for the lane\'s latest run', async () => {
    const api: Api = {
      status: async () => status(),
      lanes: async () => [lane()],
      runs: async () => [run()],
      run: async () => run({ outside: ['docs/x.md'] }),
      runLog: async () => ({ offset: 0, data: '', size: 0, done: true }) as never,
      runEvents: async () => { throw new Error('unused') },
      decisions: async () => [],
      requests: async () => [],
      spend: async () => spend(),
      feedLatest: async () => ({ items: [], events: [], last: 1 }),
      feed: async () => ({ items: [feedItem({ id: 1 })], events: [], last: 1 }),
      dismissRun: async () => { throw new Error('unused') },
      undismissRun: async () => { throw new Error('unused') },
      answer: async () => { throw new Error('unused') },
    }
    const live = new Live(api, still)
    await act(async () => {
      await live.refresh()
    })
    const f = (p: string) => ({ path: p, status: 'modified', added: 1, removed: 0, in_scope: true })
    const diff = {
      lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'def456', mode: 'stat',
      read_at: '2026-10-08T10:04:00Z', commits_total: 1, out_of_scope: 0, commits: [{ sha: 'abc1234', subject: 'run commit', author: 'a', date: '', files: 1 }],
      committed: [f('docs/x.md')], uncommitted: [f('src/wip.ts')], untracked: [],
    }
    const urls: string[] = []
    vi.stubGlobal('fetch', async (u: string) => {
      urls.push(u)
      const body = u.includes('/conversation') ? { items: [], cursor: 0, more: false, running: false } : u.includes('/diff') ? diff : u.includes('/v1/runs?') ? [run()] : u.endsWith('/v1/runs/10') ? run({ outside: ['docs/x.md'] }) : u.includes('/log') ? { offset: 0, data: '', size: 0, done: true } : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    render(
      <LiveProvider live={live}>
        <LogPage run={10} />
      </LiveProvider>,
    )
    await userEvent.click(await screen.findByRole('tab', { name: 'Changes' }))
    expect(await screen.findByText('run commit')).toBeInTheDocument()
    expect(urls).toContain('/v1/lanes/1/diff?mode=stat&run=10')
    expect(screen.getByRole('region', { name: /Uncommitted/ })).toHaveTextContent("in the lane's worktree now")
    expect(screen.getByText('outside scope')).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('shows one run in the inspector: its part of the thread, then its log and what it asked', async () => {
    vi.stubGlobal('fetch', async (u: string) => {
      const body = u.includes('/conversation')
        ? {
            items: [
              { seq: 1, kind: 'agent', run: 10, text: 'from this run' },
              { seq: 2, kind: 'agent', run: 11, text: 'from another run' },
            ],
            cursor: 2, more: false, running: false,
          }
        : u.includes('/events')
          ? { events: [], decisions: [{ id: 5, run_id: 10, question: 'Ship it?', state: 'open', created: '2026-10-08T10:00:00Z' }] }
          : u.includes('/log')
            ? { offset: 5, data: 'hello log', size: 5, done: true }
            : u.endsWith('/v1/runs/10')
              ? run()
              : {}
      return new Response(JSON.stringify(body), { status: 200 })
    })
    render(<RunInspector id={10} />)
    expect(await screen.findByText('from this run')).toBeInTheDocument()
    expect(screen.queryByText('from another run')).toBeNull()
    await userEvent.click(screen.getByRole('tab', { name: 'Asked' }))
    expect(await screen.findByRole('link', { name: 'Ship it?' })).toBeInTheDocument()
    vi.unstubAllGlobals()
  })
})

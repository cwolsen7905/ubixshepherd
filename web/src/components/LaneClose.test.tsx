import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, spend, status } from '../test/fixtures'
import type { LaneReview, LaneView } from '../api/types'
import { FinishedMark, LaneCloseButton, useFinishedLanes } from './LaneClose'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function liveFake() {
  const api: Api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    next: async () => ({ items: [] }),
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }
  return new Live(api, still)
}

interface Call { path: string; body: unknown }

/** A daemon that answers `close` by its force flag, and review with `reviews`. */
function daemon(close: (force: boolean) => Response, reviews: LaneReview[] = []) {
  const calls: Call[] = []
  vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ path, body })
    if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }), { status: 200 })
    if (path.endsWith('/close')) return close(Boolean((body as { force?: boolean }).force))
    if (path === '/v1/fold/review') return new Response(JSON.stringify(reviews), { status: 200 })
    return new Response('[]', { status: 200 })
  })
  return calls
}

const refused = (msg: string) => new Response(JSON.stringify({ error: msg }), { status: 409 })
const closed = () => new Response(JSON.stringify({ lane: lane({ state: 'closed' }), branch_deleted: true }), { status: 200 })

async function mountButton(l: LaneView, running = false) {
  const live = liveFake()
  await act(() => live.refresh())
  const onClosed = vi.fn()
  render(
    <LiveProvider live={live}>
      <LaneCloseButton lane={l} running={running} onClosed={onClosed} />
    </LiveProvider>,
  )
  return onClosed
}

afterEach(() => vi.unstubAllGlobals())

describe('LaneCloseButton', () => {
  it('closes the lane through the daemon without force', async () => {
    const calls = daemon(() => closed())
    const onClosed = await mountButton(lane())
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    expect(await screen.findByText('Lane closed')).toBeInTheDocument()
    const post = calls.find((c) => c.path === '/v1/lanes/1/close')
    expect(post?.body).toEqual({ force: false })
    expect(onClosed).toHaveBeenCalled()
  })

  it('shows the daemon’s reason when it refuses, and does not close', async () => {
    const calls = daemon(() => refused('/tmp/acme-api has uncommitted changes:\n M a.go\ncommit or discard them'))
    await mountButton(lane())
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('uncommitted changes')
    expect(screen.queryByText('Lane closed.')).not.toBeInTheDocument()
    expect(calls.filter((c) => c.path.endsWith('/close'))).toHaveLength(1)
  })

  it('offers close anyway only behind a confirm that says what it discards and keeps', async () => {
    const calls = daemon((force) => (force ? closed() : refused('branch feat/login is not in origin/dev as far as git can tell')))
    await mountButton(lane())
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Close anyway…' }))
    // Nothing forced yet: the confirm comes first.
    expect(calls.filter((c) => c.path.endsWith('/close'))).toHaveLength(1)
    const confirm = screen.getByRole('group', { name: 'Confirm closing anyway' })
    expect(confirm).toHaveTextContent('discards any uncommitted changes')
    expect(confirm).toHaveTextContent('/tmp/acme-api')
    expect(confirm).toHaveTextContent('branch feat/login is kept unless git can see it merged')
    await userEvent.click(screen.getByRole('button', { name: 'Discard changes and close' }))
    expect(await screen.findByText('Lane closed')).toBeInTheDocument()
    expect(calls.filter((c) => c.path.endsWith('/close')).map((c) => c.body)).toEqual([{ force: false }, { force: true }])
  })

  it('can back out of the confirm', async () => {
    daemon(() => refused('not merged'))
    await mountButton(lane())
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Close anyway…' }))
    await userEvent.click(screen.getByRole('button', { name: 'Keep the lane' }))
    expect(screen.queryByRole('group', { name: 'Confirm closing anyway' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Close anyway…' })).toBeInTheDocument()
  })

  it('is disabled while a run is going', async () => {
    const calls = daemon(() => closed())
    await mountButton(lane(), true)
    const b = screen.getByRole('button', { name: 'Close lane' })
    expect(b).toBeDisabled()
    await userEvent.click(b)
    expect(calls.filter((c) => c.path.endsWith('/close'))).toHaveLength(0)
  })

  it('does not offer force for a failure that is not a refusal', async () => {
    daemon(() => new Response(JSON.stringify({ error: 'database is locked' }), { status: 500 }))
    await mountButton(lane())
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('database is locked')
    expect(screen.queryByRole('button', { name: 'Close anyway…' })).not.toBeInTheDocument()
  })
})

describe('FinishedMark', () => {
  const review: LaneReview = {
    branch: 'feat/login', worktree: '/tmp/acme-api', lane: 'feat/login', verdict: 'finished',
    why: 'every change on it is in origin/dev', in_base: true,
  }

  it('says ready to close, with its evidence', () => {
    render(<FinishedMark review={review} lastRun="succeeded" detail />)
    expect(screen.getByText(/finished: ready to close/)).toBeInTheDocument()
    expect(screen.getByText(/every change on it is in origin\/dev; last run succeeded/)).toBeInTheDocument()
  })
})

describe('useFinishedLanes', () => {
  function Probe({ lanes }: { lanes: LaneView[] }) {
    const found = useFinishedLanes(lanes, 0)
    return <ul>{lanes.map((l) => <li key={l.id}>{l.name}: {found.get(l.id)?.why ?? 'not finished'}</li>)}</ul>
  }

  it('marks only the lanes the review calls finished, by repo', async () => {
    const calls = daemon(() => closed(), [
      { branch: 'a', worktree: '/a', lane: 'done-lane', verdict: 'finished', why: 'every change on it is in origin/dev' },
      { branch: 'b', worktree: '/b', lane: 'busy-lane', verdict: 'live', why: 'it has uncommitted changes' },
    ])
    render(<Probe lanes={[lane({ id: 1, name: 'done-lane' }), lane({ id: 2, name: 'busy-lane' })]} />)
    await waitFor(() => expect(screen.getByText('done-lane: every change on it is in origin/dev')).toBeInTheDocument())
    expect(screen.getByText('busy-lane: not finished')).toBeInTheDocument()
    // One review per repo, not per lane.
    expect(calls.filter((c) => c.path === '/v1/fold/review')).toHaveLength(1)
  })
})

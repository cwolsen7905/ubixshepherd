import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, spend, status } from '../test/fixtures'
import { LaneCloseButton } from './LaneClose'

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

describe('LaneCloseButton artifact choices', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('lists the uncommitted artifacts of a refused close and keeps one', async () => {
    const calls: { path: string; method?: string; body?: string }[] = []
    let kept = false
    vi.stubGlobal('fetch', async (path: string, init?: RequestInit) => {
      calls.push({ path, method: init?.method, body: init?.body as string | undefined })
      if (path === '/v1/web/session') return new Response(JSON.stringify({ csrf: 'c' }))
      if (path.endsWith('/close')) return new Response(JSON.stringify({ error: '1 file(s) to keep or discard: docs/plan.md' }), { status: 409 })
      if (path.startsWith('/v1/artifacts?'))
        return new Response(JSON.stringify(kept ? [] : [{ id: 12, workspace_id: 1, lane_id: 1, path: 'docs/plan.md', intent: 'undecided', state: 'live' }]))
      if (path === '/v1/artifacts/12') {
        kept = true
        return new Response(JSON.stringify({ id: 12 }))
      }
      return new Response('[]')
    })
    const api = {
      status: async () => status(), lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [],
      spend: async () => spend(), feedLatest: async () => ({ items: [], events: [], last: 0 }),
      feed: async (a: number) => ({ items: [], events: [], last: a }), next: async () => [],
    } as unknown as Api
    const live = new Live(api, still)
    await act(() => live.refresh())
    render(
      <LiveProvider live={live}>
        <LaneCloseButton lane={lane()} running={false} />
      </LiveProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Close lane' }))
    const group = await screen.findByRole('group', { name: 'Files to decide before closing' })
    expect(group).toHaveTextContent('docs/plan.md')
    await userEvent.click(screen.getByRole('button', { name: 'Keep' }))
    const patch = calls.find((c) => c.method === 'PATCH')
    expect(patch?.path).toBe('/v1/artifacts/12')
    expect(JSON.parse(patch?.body ?? '{}')).toEqual({ intent: 'keep' })
    await act(async () => {})
    expect(screen.queryByRole('group', { name: 'Files to decide before closing' })).toBeNull()
  })
})

import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, spend, status } from '../test/fixtures'
import { ArtifactsPage } from './Artifacts'

const still: Clock = { now: () => Date.parse('2026-10-15T12:00:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const art = (o: object) => ({
  workspace_id: 1, repo_id: 1, lane_id: 1, run_id: 5, kind: 'markdown', source: 'declared', intent: 'keep', intent_by: 'agent',
  state: 'live', size: 100, created: '2026-10-15T10:00:00Z', updated: '2026-10-15T11:00:00Z', ...o,
})

async function show(props: Parameters<typeof ArtifactsPage>[0], calls: string[]) {
  vi.stubGlobal('fetch', async (path: string) => {
    calls.push(path)
    if (path.startsWith('/v1/artifacts'))
      return new Response(JSON.stringify([art({ id: 1, path: 'docs/a.md' }), art({ id: 2, path: 'docs/b.md', repo_id: 2, lane_id: 2, intent: 'undecided' })]))
    if (path.startsWith('/v1/topics')) return new Response(JSON.stringify({ topics: [{ id: 3, title: 'CSV export' }] }))
    return new Response('{}')
  })
  const api = {
    status: async () => status(),
    lanes: async () => [lane({ id: 1, repo_id: 1, repo: 'acme-api' }), lane({ id: 2, repo_id: 2, repo: 'acme-web', name: 'feat/ui' })],
    runs: async () => [],
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after: number) => ({ items: [], events: [], last: after }),
    next: async () => [],
  } as unknown as Api
  const live = new Live(api, still)
  await act(() => live.refresh())
  render(
    <LiveProvider live={live}>
      <ArtifactsPage {...props} />
    </LiveProvider>,
  )
}

afterEach(() => vi.unstubAllGlobals())

describe('ArtifactsPage', () => {
  it('lists every artifact with its lane and intent and opens one in the inspector', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    expect(await screen.findByText('docs/a.md')).toBeInTheDocument()
    expect(screen.getByText('docs/b.md')).toBeInTheDocument()
    expect(screen.getByText('undecided')).toBeInTheDocument()
    await userEvent.click(screen.getByText('docs/a.md'))
    expect(decodeURIComponent(window.location.hash)).toContain('open=artifact:1')
  })

  it('asks the daemon for a lane, run and topic, and filters a repo itself', async () => {
    const calls: string[] = []
    window.location.hash = '#/artifacts'
    await show({ repo: 2, lane: 2, run: 5, topic: 3 }, calls)
    expect(await screen.findByText('docs/b.md')).toBeInTheDocument()
    expect(screen.queryByText('docs/a.md')).toBeNull()
    const q = calls.find((c) => c.startsWith('/v1/artifacts?')) ?? ''
    expect(q).toContain('lane_id=2')
    expect(q).toContain('run_id=5')
    expect(q).toContain('topic_id=3')
  })
})

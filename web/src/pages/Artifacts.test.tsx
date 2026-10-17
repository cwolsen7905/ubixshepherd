import { act, fireEvent, render, screen, within } from '@testing-library/react'
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
    if (/^\/v1\/artifacts\/\d+\/content/.test(path)) return new Response('# The body\n\n![shot](img/a.png)\n')
    if (/^\/v1\/artifacts\/\d+$/.test(path)) return new Response(JSON.stringify(art({ id: 1, path: 'docs/a.md', versions: [] })))
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
  it('lists every artifact with its lane and intent and opens one full width', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    expect(await screen.findByText('docs/a.md')).toBeInTheDocument()
    expect(screen.getByText('docs/b.md')).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'undecided' })).toBeInTheDocument()
    await act(async () => { await userEvent.click(screen.getByRole('link', { name: 'docs/a.md' })) })
    expect(window.location.hash).toBe('#/artifacts?view=1')
    expect(screen.getByRole('button', { name: /← Artifacts/ })).toBeInTheDocument()
  })

  it('draws an opened file under the shared page header, with its actions in it', async () => {
    window.location.hash = '#/artifacts?view=1'
    await show({}, [])
    const head = (await screen.findByRole('heading', { level: 1 })).closest('[data-component="PageHeader"]') as HTMLElement
    expect(head).not.toBeNull()
    const h = within(head)
    expect(h.getByRole('link', { name: '← Artifacts' })).toBeInTheDocument()
    expect(h.getByRole('button', { name: /^Close\s*Esc$/ })).toBeInTheDocument()
    expect(h.getByRole('button', { name: 'Open beside the artifacts' })).toBeInTheDocument()
    expect(h.getAllByText('docs/a.md').length).toBeGreaterThan(0)
    expect(h.getByRole('group', { name: 'What to do with this file' })).toBeInTheDocument()
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

  it('filters by intent', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    expect(await screen.findByText('docs/a.md')).toBeInTheDocument()
    expect(screen.getByText('docs/b.md')).toBeInTheDocument()
    const intent = screen.getByLabelText('Intent')
    await userEvent.selectOptions(intent, 'undecided')
    expect(screen.queryByText('docs/a.md')).toBeNull()
    expect(screen.getByText('docs/b.md')).toBeInTheDocument()
  })

  it('clears the intent filter with Clear filters', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    expect(await screen.findByText('docs/a.md')).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByLabelText('Intent'), 'undecided')
    expect(screen.queryByText('docs/a.md')).toBeNull()
    await userEvent.click(screen.getByRole('link', { name: 'Clear filters' }))
    expect(screen.getByLabelText('Intent')).toHaveValue('all')
    expect(screen.getByText('docs/a.md')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Clear filters' })).toBeNull()
  })

  it('keeps the intent filter in the address and reads it back', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    expect(await screen.findByText('docs/a.md')).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByLabelText('Intent'), 'undecided')
    expect(window.location.hash).toBe('#/artifacts?intent=undecided')
  })

  it('applies an intent from the address on load', async () => {
    window.location.hash = '#/artifacts?intent=undecided'
    await show({}, [])
    expect(await screen.findByText('docs/b.md')).toBeInTheDocument()
    expect(screen.queryByText('docs/a.md')).toBeNull()
    expect(screen.getByLabelText('Intent')).toHaveValue('undecided')
  })

  it('leaves Enter on a lane link to the link and ignores a modified click on a row', async () => {
    window.location.hash = '#/artifacts'
    await show({}, [])
    await screen.findByText('docs/a.md')
    const lane = screen.getAllByRole('link').find((l) => l.getAttribute('href')?.startsWith('#/lanes'))
    if (!lane) throw new Error('no lane link')
    lane.focus()
    await userEvent.keyboard('{Enter}')
    expect(window.location.hash).not.toContain('view=')
    window.location.hash = '#/artifacts'
    const cell = screen.getAllByRole('cell', { name: /none|\d+/ })[0]
    if (!cell) throw new Error('no cell')
    await act(async () => { fireEvent.click(cell, { metaKey: true }) })
    expect(window.location.hash).not.toContain('view=')
  })

  it('opens a deep link to a file full width, in place of the list, with its image from the lane', async () => {
    window.location.hash = '#/artifacts?view=1&lane=1'
    await show({ lane: 1 }, [])
    expect(await screen.findByText('The body')).toBeInTheDocument()
    const img = await screen.findByRole('img', { name: 'shot' })
    expect(img.getAttribute('src')).toBe('/v1/lanes/1/artifacts/content?path=docs%2Fimg%2Fa.png')
    expect(screen.queryByRole('table')).toBeNull()
  })

  it('returns to the list with its filters on the back arrow and on Escape', async () => {
    window.location.hash = '#/artifacts?lane=1&view=1'
    await show({ lane: 1 }, [])
    await screen.findByText('The body')
    await userEvent.click(screen.getByRole('link', { name: /← Artifacts/ }))
    expect(window.location.hash).toBe('#/artifacts?lane=1')
    expect(screen.queryByText('The body')).toBeNull()
    expect(await screen.findByText('docs/a.md')).toBeVisible()

    window.location.hash = '#/artifacts?lane=1&view=1'
    await screen.findByText('The body')
    await userEvent.keyboard('{Escape}')
    await act(async () => {})
    expect(window.location.hash).toBe('#/artifacts?lane=1')
  })
})

import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { feedItem, lane, project, run, status } from '../test/fixtures'
import { HistoryPage, mentions } from './History'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

async function show(props: Parameters<typeof HistoryPage>[0] = {}, older: ReturnType<typeof feedItem>[] = []) {
  vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ projects: [project({ name: 'shop', repos: ['acme-api'] }), project({ name: 'blog', repos: ['blog'] })] }), { status: 200 }))
  const items = [
    feedItem({ id: 1, kind: 'run_passed', text: 'run 10: claude in lane feat/login succeeded', ref: 10, created: '2026-10-10T12:30:00Z' }),
    feedItem({ id: 2, kind: 'decision_answered', text: 'decision 3 answered', ref: 3, created: '2026-10-10T13:00:00Z' }),
    feedItem({ id: 3, kind: 'mr', text: 'blog: opened !7', ref: 8, created: '2026-10-09T09:05:00Z' }),
    ...older,
  ]
  const api = {
    status: async () => status(), lanes: async () => [lane()], runs: async () => [run({ id: 10, lane_id: 1 })], decisions: async () => [], requests: async () => [],
    spend: async () => null,
    feedLatest: async () => ({ items: [], events: [], last: items.length }),
    feed: async (after: number) => (after === 0 ? { items, events: ['run_passed', 'decision_answered', 'mr', ...older.map((o) => o.kind)], last: items.length } : { items: [], events: [], last: after }),
  } as unknown as Api
  const live = new Live(api, still)
  await act(() => live.refresh())
  render(
    <LiveProvider live={live}>
      <HistoryPage {...props} />
    </LiveProvider>,
  )
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  window.location.hash = ''
})

describe('the History page', () => {
  it('groups the log by day, newest first, with each entry linking to its run or lane', async () => {
    await show()
    const today = await screen.findByRole('region', { name: 'Today' })
    const rows = within(today).getAllByRole('listitem')
    expect(rows[0]).toHaveTextContent('decision 3 answered')
    expect(rows[1]).toHaveTextContent('run 10')
    expect((rows[1] as HTMLElement).querySelector('a.sh-row-title')).toHaveAttribute('href', '#/runs/10/log')
    expect(screen.getByRole('region', { name: 'Yesterday' })).toHaveTextContent('blog: opened !7')
  })

  it('opens an entry on Enter or a click on its row, and the lane link still works', async () => {
    await show()
    const today = await screen.findByRole('region', { name: 'Today' })
    const row = within(today).getAllByRole('listitem')[1] as HTMLElement
    const line = row.querySelector('.history-row') as HTMLElement
    line.focus()
    await userEvent.keyboard('{Enter}')
    expect(window.location.hash).toBe('#/runs/10/log')
    window.location.hash = ''
    await userEvent.click(line)
    expect(window.location.hash).toBe('#/runs/10/log')
    window.location.hash = ''
    await userEvent.click(within(row).getByRole('link', { name: 'feat/login' }))
    expect(window.location.hash).toBe('#/lanes/1')
  })

  it('hides entries older than the default 7 days until the range is widened', async () => {
    await show({}, [feedItem({ id: 4, kind: 'run_passed', text: 'run 2: old work', ref: 2, created: '2026-09-20T09:00:00Z' })])
    await screen.findByRole('region', { name: 'Today' })
    expect(screen.queryByText('run 2: old work')).toBeNull()
    expect(screen.getByText('3 of 3 in the last 7 days')).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Date range' }), 'all')
    expect(await screen.findByText('run 2: old work')).toBeInTheDocument()
    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Date range' }), 'today')
    expect(screen.queryByText('run 2: old work')).toBeNull()
    expect(screen.queryByText('blog: opened !7')).toBeNull()
  })

  it('says when only older entries exist', async () => {
    await show({ q: 'old work' }, [feedItem({ id: 4, kind: 'run_passed', text: 'run 2: old work', ref: 2, created: '2026-09-20T09:00:00Z' })])
    expect(await screen.findByText('Nothing matches.')).toBeInTheDocument()
    expect(screen.getByText('Clear a filter to see more.')).toBeInTheDocument()
  })

  it('links the lane from the entry\'s ids, never from its text', async () => {
    await show({}, [
      feedItem({ id: 4, kind: 'lane_opened', text: 'lane opened: feat/login in acme-api', ref: 1, created: '2026-10-09T14:00:00Z' }),
      feedItem({ id: 5, kind: 'lane_opened', text: 'lane opened: feat/login in acme-api', ref: 99, created: '2026-10-09T14:05:00Z' }),
      feedItem({ id: 6, kind: 'lane_closed', text: 'lane closed: something', created: '2026-10-09T14:10:00Z' }),
    ])
    const day = await screen.findByRole('region', { name: 'Yesterday' })
    const rows = within(day).getAllByRole('listitem')
    // Newest first: no ref, then an unknown lane id (its text names a real lane), then a known id.
    expect(within(rows[0] as HTMLElement).queryByRole('link', { name: 'feat/login' })).toBeNull()
    expect(within(rows[1] as HTMLElement).queryByRole('link', { name: 'feat/login' })).toBeNull()
    expect(within(rows[2] as HTMLElement).getByRole('link', { name: 'feat/login' })).toHaveAttribute('href', '#/lanes/1')
  })

  it('filters by kind, project and search text', async () => {
    await show({ kind: 'answer' })
    expect(await screen.findAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByRole('button', { name: /^Answers/ })).toHaveAttribute('aria-pressed', 'true')
  })

  it('narrows to a project through its repos and lanes', async () => {
    await show({ project: 'shop' })
    const rows = await screen.findAllByRole('listitem')
    expect(rows).toHaveLength(1)
    expect(rows[0]).toHaveTextContent('feat/login')
  })

  it('searches the text, and says when nothing matches', async () => {
    await show({ q: 'nothing like this' })
    expect(await screen.findByText('Nothing matches.')).toBeInTheDocument()
  })

  it('puts a kind choice in the address', async () => {
    await show()
    await userEvent.click(await screen.findByRole('button', { name: /^Merges/ }))
    expect(window.location.hash).toBe('#/history?kind=merge')
  })
})

describe('mentions', () => {
  it('matches whole names only', () => {
    expect(mentions('api: opened !7', 'api')).toBe(true)
    expect(mentions('run 3 in lane feat/login', 'feat/login')).toBe(true)
    expect(mentions('api-gateway: opened !7', 'api')).toBe(false)
    expect(mentions('a rapid run', 'api')).toBe(false)
    expect(mentions('x (api) y', 'api')).toBe(true)
  })
})

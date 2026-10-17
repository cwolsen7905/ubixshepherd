import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { feedItem, lane, project, status } from '../test/fixtures'
import { HistoryPage } from './History'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

async function show(props: Parameters<typeof HistoryPage>[0] = {}) {
  vi.stubGlobal('fetch', async () => new Response(JSON.stringify({ projects: [project({ name: 'shop', repos: ['acme-api'] }), project({ name: 'blog', repos: ['blog'] })] }), { status: 200 }))
  const items = [
    feedItem({ id: 1, kind: 'run_passed', text: 'run 10: claude in lane feat/login succeeded', ref: 10, created: '2026-10-10T12:30:00Z' }),
    feedItem({ id: 2, kind: 'decision_answered', text: 'decision 3 answered', ref: 3, created: '2026-10-10T13:00:00Z' }),
    feedItem({ id: 3, kind: 'mr', text: 'blog: opened !7', ref: 8, created: '2026-10-09T09:05:00Z' }),
  ]
  const api = {
    status: async () => status(), lanes: async () => [lane()], runs: async () => [], decisions: async () => [], requests: async () => [],
    spend: async () => null,
    feedLatest: async () => ({ items: [], events: [], last: 3 }),
    feed: async (after: number) => (after === 0 ? { items, events: ['run_passed', 'decision_answered', 'mr'], last: 3 } : { items: [], events: [], last: after }),
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
    expect(within(rows[1] as HTMLElement).getByRole('link')).toHaveAttribute('href', '#/runs/10/log')
    expect(screen.getByRole('region', { name: 'Yesterday' })).toHaveTextContent('blog: opened !7')
  })

  it('filters by kind, project and search text', async () => {
    await show({ kind: 'answer' })
    expect(await screen.findAllByRole('listitem')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'Answers' })).toHaveAttribute('aria-pressed', 'true')
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
    await userEvent.click(await screen.findByRole('button', { name: 'Merges' }))
    expect(window.location.hash).toBe('#/history?kind=merge')
  })
})

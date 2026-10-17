import { readFileSync } from 'node:fs'
import { act, render, screen, within } from '@testing-library/react'
import type { RunHistory, SpendToday, Status, UsageStats } from '../api/types'
import { agentRows, budgetUse, chartDays, chartModel, costliestRuns, heroSub, modelRows, projectOfRepo, projectRows, shortModel, usageWindow } from '../model/spend'
import { CostliestList, SpendHero, dayNum, dayShort } from '../components/UsageView'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { spend, status } from '../test/fixtures'
import { UsagePage } from './Usage'

const NOW = new Date(2026, 9, 10, 15, 0).getTime()
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function show(spendToday: SpendToday = spend(), statusToday: Status = status(), runs: Api['runs'] = async () => []) {
  const api: Api = {
    status: async () => statusToday,
    lanes: async () => [],
    runs,
    run: async () => { throw new Error('unused') },
    runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') },
    decisions: async () => [],
    requests: async () => [],
    spend: async () => spendToday,
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    dismissRun: async () => { throw new Error('unused') },
    undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }
  const live = new Live(api, still)
  return { live, api }
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('the usage spend model', () => {
  it('names the last 14 days ending today, oldest first', () => {
    expect(chartDays(NOW)).toHaveLength(14)
    expect(chartDays(NOW)[0]).toBe('2026-09-27')
    expect(chartDays(NOW)[13]).toBe('2026-10-10')
  })

  it('windows the usage reads on the chart days', () => {
    expect(usageWindow(NOW)).toEqual({ from: '2026-09-27', to: '2026-10-10' })
  })

  it('says near budget past 80% of the cap and over at it', () => {
    const at = (usd: number): SpendToday => ({ day: '2026-10-10', usd, budget: 20, credit_usd: 0, by_source: {} })
    const low = budgetUse(at(4.1))
    expect(low?.fraction).toBeCloseTo(0.205, 10)
    expect(low).toMatchObject({ percent: 21, near: false, over: false })
    expect(budgetUse(at(16))).toMatchObject({ near: true, over: false })
    expect(budgetUse(at(20))).toMatchObject({ near: false, over: true })
    expect(budgetUse({ ...at(4.1), budget: 0 })).toBeNull()
    expect(heroSub(at(16))).toEqual({ of: 'of $20.00 today', note: 'near budget' })
    expect(heroSub(at(21))).toEqual({ of: 'of $20.00 today', note: 'over budget' })
    expect(heroSub(at(4.1))).toEqual({ of: 'of $20.00 today', note: '' })
  })

  it('groups the cards biggest spend first, short model ids with the full id for the hover', () => {
    const stats = {
      by_model: [
        { key: 'claude a', agent: 'claude', model: 'a', cost_usd: 1.2 },
        { key: 'opencode openrouter/moonshotai/kimi-k2', agent: 'opencode', model: 'openrouter/moonshotai/kimi-k2', cost_usd: 3 },
      ],
      by_lane: [
        { key: 'acme/feat-x', repo: 'acme', lane: 'feat-x', cost_usd: 4.2 },
        { key: 'docs/pass', repo: 'docs', lane: 'pass', cost_usd: 0.5 },
      ],
      desk_by_model: [{ key: 'sonnet', agent: 'claude', model: 'sonnet', cost_usd: 10 }],
    } as unknown as UsageStats
    expect(agentRows(stats).map((r) => [r.label, r.usd])).toEqual([['claude', 11.2], ['opencode', 3]])
    expect(shortModel('openrouter/moonshotai/kimi-k2')).toBe('kimi-k2')
    expect(shortModel('sonnet')).toBe('sonnet')
    const models = modelRows(stats)
    expect(models[0]).toMatchObject({ label: 'sonnet', usd: 10 })
    expect(models.find((r) => r.key === 'openrouter/moonshotai/kimi-k2')).toMatchObject({
      label: 'kimi-k2', title: 'openrouter/moonshotai/kimi-k2', usd: 3,
    })
    const ofRepo = projectOfRepo([{ name: 'shop', repos: ['acme'], lanes: 0, running: 0, decisions: 0, requests: 0, waiting: 0, spend_today: 0 }])
    expect(projectRows(stats, ofRepo).map((r) => [r.label, r.usd])).toEqual([['shop', 4.2], ['Unassigned', 0.5]])
  })

  it('paints the chart lichen under 80% of the cap, lantern at it, broken at it', () => {
    const days = ['2026-10-09', '2026-10-10']
    const { bars, max } = chartModel(days, { '2026-10-09': 4, '2026-10-10': 20 }, 20)
    expect(bars.map((b) => b.state)).toEqual(['under', 'over'])
    expect(chartModel(days, { '2026-10-09': 16, '2026-10-10': 4 }, 20).bars.map((b) => b.state)).toEqual(['near', 'under'])
    expect(max).toBe(20)
    expect(chartModel(days, { '2026-10-09': 4 }, 0).bars[0]?.state).toBe('under')
  })

  it('names the most expensive runs first', () => {
    const runs = [
      { id: 1, cost_usd: 0.5 }, { id: 2, cost_usd: undefined }, { id: 3, cost_usd: 4.2 },
    ] as RunHistory['runs']
    expect(costliestRuns(runs, 2).map((r) => r.id)).toEqual([3, 1])
  })

  it('shortens a chart day to "Oct 3", and to its number alone when narrow', () => {
    expect(dayShort('2026-10-03')).toBe('Oct 3')
    expect(dayNum('2026-10-03')).toBe('3')
  })
})

describe('the usage parts', () => {
  const run = (id: number, state: string) => ({ id, lane_id: 1, repo: 'r', lane: 'l', agent: 'claude', state, commits: 0, started: '2026-10-10T10:00:00Z', task: '' })

  it('draws each run state with a glyph from the status vocabulary and its word', () => {
    const { container } = render(<CostliestList runs={['running', 'succeeded', 'failed', 'interrupted', 'stopped'].map((s, i) => run(i + 1, s))} />)
    const glyphs = [...container.querySelectorAll('.sh-glyph [aria-hidden="true"]')].map((g) => g.textContent)
    expect(glyphs).toEqual(['●', '✓', '✗', '↯', '○'])
    expect(container).toHaveTextContent('interrupted')
  })

  it('shows the budget note lantern near the cap and broken at it', () => {
    const { container, rerender } = render(<SpendHero usdToday={16} budget={20} sub={{ of: 'of $20.00 today', note: 'near budget' }} offline={false} />)
    expect(container.querySelector('.sh-spend-note .is-near')).not.toBeNull()
    expect(container.querySelector('.sh-spend-note .is-over')).toBeNull()
    rerender(<SpendHero usdToday={21} budget={20} sub={{ of: 'of $20.00 today', note: 'over budget' }} offline={false} />)
    expect(container.querySelector('.sh-spend-note .is-over')).not.toBeNull()
    expect(readFileSync('src/pages/Usage.css', 'utf8')).toMatch(/\.sh-spend-note \.is-over \{ color: var\(--sh-broken\)/)
  })
})

describe('the Usage page', () => {
  async function mount(o: { spendToday?: SpendToday; runs?: Api['runs'] } = {}) {
    vi.stubGlobal('fetch', async (path: string) => {
      const url = new URL(path, 'http://x')
      if (url.pathname === '/v1/usage') {
        const stats: UsageStats = {
          from: url.searchParams.get('from') ?? '', to: url.searchParams.get('to') ?? '',
          total: { count: 1, measured: 0, input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 0, peak_context: 0, over_200k: 0, compactions: 0, cost_usd: 2 },
          runs: { count: 1, measured: 0, input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 0, peak_context: 0, over_200k: 0, compactions: 0, cost_usd: 2 },
          desk: { count: 0, measured: 0, input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 0, peak_context: 0, over_200k: 0, compactions: 0, cost_usd: 0 },
          by_model: [{ key: 'claude sonnet', agent: 'claude', model: 'sonnet', count: 1, measured: 0, input_tokens: 0, cache_read_tokens: 0, cache_creation_tokens: 0, output_tokens: 0, peak_context: 0, over_200k: 0, compactions: 0, cost_usd: 2 }],
          by_lane: [],
          desk_by_model: [],
        }
        return new Response(JSON.stringify(stats), { status: 200 })
      }
      if (url.pathname === '/v1/status') return new Response(JSON.stringify({ ...status(), detail: { quota_holds: [{ agent: 'copilot', until: '2026-10-10T16:00:00Z' }] } }), { status: 200 })
      if (url.pathname === '/v1/projects') return new Response(JSON.stringify({ projects: [] }), { status: 200 })
      if (url.pathname === '/v1/history/runs') {
        return new Response(JSON.stringify({ runs: [], count: 0, cost_usd: 0, credits: 0, agents: [], lanes: [] }), { status: 200 })
      }
      return new Response('{}', { status: 404 })
    })
    const { live } = show(o.spendToday, status(), o.runs)
    await act(() => live.refresh())
    render(
      <LiveProvider live={live}>
        <UsagePage now={NOW} />
      </LiveProvider>,
    )
  }

  it('shows the big figure with "of $X today", the meter and the reset line', async () => {
    await mount({ spendToday: spend({ usd: 16.4, budget: 20 }) })
    const hero = await screen.findByRole('region', { name: 'Spent today' })
    expect(hero).toHaveTextContent('$16.40')
    expect(hero).toHaveTextContent('of $20.00 today')
    expect(hero).toHaveTextContent('near budget')
    expect(within(hero).getByRole('meter', { name: 'Spent today' })).toHaveAttribute('aria-valuenow', '16.4')
    expect(screen.getByText('Resets at midnight, local time.')).toBeInTheDocument()
  })

  it('shows the cards, the 14-day chart, the holds and the expensive runs', async () => {
    await mount()
    expect(await screen.findByText('claude')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: 'By model' })).toHaveTextContent('sonnet')
    expect(screen.getByRole('region', { name: 'Spend, last 14 days' })).toBeInTheDocument()
    expect(await screen.findByText('copilot')).toBeInTheDocument()
  })
})

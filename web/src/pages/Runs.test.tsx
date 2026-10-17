import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { RunHistory, RunRecord } from '../api/types'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { spend, status } from '../test/fixtures'
import { RunsPage } from './Runs'

const NOW = Date.parse('2026-10-10T15:00:00Z')
const still: Clock = { now: () => NOW, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

function makeRun(o: Partial<RunRecord> = {}): RunRecord {
  return {
    id: 10, lane_id: 1, repo: 'acme-api', lane: 'feat/login', lane_state: 'open', agent: 'claude', state: 'succeeded',
    commits: 2, cost_usd: 0.5, started: '2026-10-10T12:00:00Z', ended: '2026-10-10T12:04:12Z', task: 'Fix the login redirect', ...o,
  }
}

function makeHistory(runs: RunRecord[]): RunHistory {
  return { runs, count: runs.length, cost_usd: 0.5, credits: 0, agents: ['claude'], lanes: [{ id: 1, repo: 'acme-api', name: 'feat/login' }] }
}

async function show(props: Parameters<typeof RunsPage>[0], body: RunHistory) {
  vi.stubGlobal('fetch', async () => new Response(JSON.stringify(body), { status: 200 }))
  const api: Api = {
    status: async () => status(), lanes: async () => [], runs: async () => [],
    run: async () => { throw new Error('unused') }, runLog: async () => { throw new Error('unused') },
    runEvents: async () => { throw new Error('unused') }, decisions: async () => [], requests: async () => [],
    spend: async () => spend(), feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after) => ({ items: [], events: [], last: after }),
    dismissRun: async () => { throw new Error('unused') }, undismissRun: async () => { throw new Error('unused') },
    answer: async () => { throw new Error('unused') },
  }
  const live = new Live(api, still)
  await act(() => live.refresh())
  render(
    <LiveProvider live={live}>
      <RunsPage {...props} />
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

describe('the Runs page', () => {
  it('shows the design list: glyph, run, lane over agent/model/started, result over duration, cost', async () => {
    await show({}, makeHistory([
      makeRun({ id: 12, state: 'failed' }),
      makeRun({ id: 11 }),
    ]))
    const table = await screen.findByRole('table')
    expect(table).toHaveAttribute('data-component', 'RunsTable')
    expect(table).toHaveAttribute('data-variant', 'list')
    const rows = within(table).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2)
    const laneName = rows[0]?.querySelector('.sh-runs-lane-name') as HTMLElement
    expect(laneName).toHaveTextContent('acme-api / feat/login')
    const laneMeta = rows[0]?.querySelector('.sh-runs-lane-meta') as HTMLElement
    expect(laneMeta).toHaveTextContent('claude · · started')
    const result = rows[0]?.querySelector('.sh-runs-result') as HTMLElement
    expect(result).toHaveTextContent('failed')
  })

  it('has chips for range, agent and lane', async () => {
    await show({}, makeHistory([makeRun()]))
    await screen.findByRole('table')
    expect(screen.getByRole('button', { name: 'Today' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Week' })).toHaveAttribute('aria-pressed', 'false')
    expect(screen.getByRole('button', { name: 'claude' })).toBeInTheDocument()
  })

  it('switches the range chip and clears unselected states', async () => {
    await show({}, makeHistory([makeRun()]))
    await screen.findByRole('table')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Week' }))
    await waitFor(() => expect(window.location.hash).toBe('#/runs?range=week'))
  })

  it('says what is empty with the kit empty state', async () => {
    await show({}, makeHistory([]))
    expect(await screen.findByText('No run today.')).toBeInTheDocument()
  })
})

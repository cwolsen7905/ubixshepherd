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

const PROMPT = [
  'You are working for uBixShepherd in lane feat/login of repo acme-api.',
  'Work only inside this directory: /w (branch feat/login, cut from dev).',
  '',
  'Task:',
  'Fix the login redirect',
  'and cover it with a test',
].join('\n')

function run(o: Partial<RunRecord>): RunRecord {
  return {
    id: 10, lane_id: 1, repo: 'acme-api', lane: 'feat/login', lane_state: 'open', agent: 'claude', state: 'succeeded',
    commits: 2, cost_usd: 0.5, started: '2026-10-10T12:00:00Z', ended: '2026-10-10T12:04:12Z', task: PROMPT, ...o,
  }
}

function history(runs: RunRecord[]): RunHistory {
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

describe('the Runs page task column', () => {
  it('shows the task, not the prompt’s preamble, and keeps the line in the title', async () => {
    await show({}, history([run({}), run({ id: 11, task: 'Bump the modules' })]))
    const table = await screen.findByRole('table')
    const rows = within(table).getAllByRole('row').slice(1)
    const cell = rows[0]?.querySelector('.run-task') as HTMLElement
    expect(cell).toHaveTextContent('Fix the login redirect')
    expect(cell).not.toHaveTextContent('You are working for')
    expect(cell).toHaveAttribute('title', 'Fix the login redirect\nand cover it with a test')
    expect(rows[1]?.querySelector('.run-task')).toHaveTextContent('Bump the modules')
  })

  it('labels its columns in sentence case', async () => {
    await show({}, history([run({})]))
    const table = await screen.findByRole('table')
    const heads = within(table).getAllByRole('columnheader').map((h) => h.textContent)
    expect(heads).toEqual(['State', 'Run', 'Agent', 'Lane', 'Outcome', 'Commits', 'Cost', 'Started', 'Took', 'Task'])
  })
})

describe('the Runs page filters', () => {
  it('clears the filters with a button that goes back to the range', async () => {
    await show({ range: 'week', agent: 'claude' }, history([run({})]))
    await screen.findByRole('table')
    await userEvent.setup().click(screen.getByRole('button', { name: 'Clear filters' }))
    await waitFor(() => expect(window.location.hash).toBe('#/runs?range=week'))
  })

  it('says what is empty with the kit’s empty state', async () => {
    await show({ agent: 'copilot' }, history([]))
    expect(await screen.findByText('No run matches these filters in today.')).toBeInTheDocument()
  })
})

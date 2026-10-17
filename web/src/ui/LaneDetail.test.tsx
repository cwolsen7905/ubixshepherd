import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import type { NextItem } from '../api/types'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, lane, nextItem, request, run, spend, status } from '../test/fixtures'
import { defaultLaneDetailTab, laneStateAction, LaneDetail, type LaneDetailTab } from './LaneDetail'
import { rememberedTab } from './LaneDetailMemory'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
const NOW = Date.parse('2026-10-08T10:05:00Z')

const diff = {
  lane_id: 1, lane: 'feat/login', branch: 'feat/login', base: 'dev', base_ref: 'origin/dev', head: 'abcdef1234567', mode: 'stat', read_at: '2026-10-08T10:04:00Z',
  commits: [], commits_total: 0, committed: [{ path: 'a.go', status: 'modified', added: 1, removed: 0, in_scope: true }], uncommitted: [], untracked: [], out_of_scope: 0,
}

beforeEach(() => {
  window.sessionStorage.clear()
  vi.stubGlobal('fetch', async (path: string) => {
    if (path.includes('/diff')) return new Response(JSON.stringify(diff))
    if (path.includes('/files')) return new Response(JSON.stringify({ path: '', name: 'app', kind: 'dir', children: [{ path: 'README.md', name: 'README.md', kind: 'file', size: 4 }] }))
    if (path.includes('/artifacts')) return new Response(JSON.stringify({ artifacts: [], globs: [] }))
    if (path.includes('/conversation')) return new Response(JSON.stringify({ items: [], cursor: 0, more: false, running: false }))
    return new Response('[]')
  })
})
afterEach(() => vi.unstubAllGlobals())

async function mount(layout: 'panel' | 'page', first: LaneDetailTab = 'overview', l = lane()) {
  const api = {
    status: async () => status(), lanes: async () => [l], runs: async () => [run()], decisions: async () => [], requests: async () => [],
    spend: async () => spend(), feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
  } as unknown as Api
  const live = new Live(api, still)
  await act(() => live.refresh())
  function Host() {
    const [tab, setTab] = useState<LaneDetailTab>(first)
    return <LaneDetail lane={l} runs={[run({ id: 12 }), run({ id: 11 })]} now={NOW} tab={tab} onTab={setTab} feedLast={0} full={layout === 'page'} />
  }
  return render(<LiveProvider live={live}><Host /></LiveProvider>)
}

describe('LaneDetail', () => {
  it.each(['panel', 'page'] as const)('has the same six tabs as a %s', async (layout) => {
    await mount(layout)
    const el = document.querySelector('[data-component="LaneDetail"]')
    expect(el).toHaveAttribute('data-layout', layout)
    const names = screen.getAllByRole('tab').map((t) => t.textContent?.replace(/\s*\d+$/, ''))
    expect(names).toEqual(['Overview', 'Changes', 'Artifacts', 'Conversation', 'Runs', 'Files'])
    expect(screen.getByRole('tab', { name: /^Runs 2/ })).toBeInTheDocument()
    // Only the page repeats the lane's name as a heading.
    expect(screen.queryByRole('heading', { level: 1 }) !== null).toBe(layout === 'page')
  })

  it.each(['panel', 'page'] as const)('shows the same FileBrowser, ChangesList and RunsTable in the %s', async (layout) => {
    await mount(layout)
    await userEvent.click(screen.getByRole('tab', { name: /^Files/ }))
    expect(await screen.findByRole('button', { name: /README\.md/ })).toBeInTheDocument()
    expect(document.querySelectorAll('[data-component="FileBrowser"]')).toHaveLength(1)
    await userEvent.click(screen.getByRole('tab', { name: /^Changes/ }))
    expect(await screen.findByText('a.go')).toBeInTheDocument()
    expect(document.querySelectorAll('[data-component="ChangesList"]')).toHaveLength(1)
    await userEvent.click(screen.getByRole('tab', { name: /^Runs/ }))
    expect(document.querySelector('[data-component="RunsTable"]')).toHaveAttribute('data-variant', 'full')
  })

  it('keeps the tab when the same lane opens as a page (MAINT-C-04)', async () => {
    const panel = await mount('panel')
    await userEvent.click(screen.getByRole('tab', { name: /^Files/ }))
    expect(rememberedTab(1)).toBe('files')
    panel.unmount()
    // The page's caller starts from what was remembered.
    await mount('page', rememberedTab(1))
    expect(screen.getByRole('tab', { name: /^Files/ })).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('button', { name: /README\.md/ })).toBeInTheDocument()
  })

  it('leaves the Conversation tab to the thread, with a placeholder reader meanwhile', async () => {
    await mount('panel')
    await userEvent.click(screen.getByRole('tab', { name: 'Conversation' }))
    expect(await screen.findByRole('region', { name: 'Conversation' })).toBeInTheDocument()
  })

  it('has the state\'s primary action on the Overview', async () => {
    await mount('panel')
    const panel = screen.getByRole('tabpanel')
    // No decision, request, run failure or approved item waits: no primary, only the
    // ghost "Open as page" and Close lane at the right.
    expect(within(panel).queryByRole('button', { name: 'Answer decision' })).toBeNull()
    expect(within(panel).getByRole('button', { name: 'Open as page' })).toBeInTheDocument()
    expect(within(panel).getByRole('button', { name: 'Close lane' })).toBeInTheDocument()
  })

  it('opens a lane waiting for a merge review on Changes, others on Overview', () => {
    expect(defaultLaneDetailTab(lane())).toBe('overview')
    expect(defaultLaneDetailTab(lane({ mr: 7, mr_state: 'open', pipeline_status: 'passed' }))).toBe('changes')
    expect(defaultLaneDetailTab(lane({ mr: 7, mr_state: 'open', pipeline_status: 'running' }))).toBe('overview')
    expect(defaultLaneDetailTab(lane({ mr: 7, mr_state: 'open', pipeline_status: 'failed' }))).toBe('overview')
  })

  it('says a closed lane has no worktree to read', async () => {
    await mount('page', 'files', lane({ state: 'closed', closed: '2026-10-08T09:00:00Z' }))
    expect(screen.getByText(/no worktree to read/)).toBeInTheDocument()
  })
})

describe('laneStateAction', () => {
  const l = lane()
  const base = { lane: l, runs: [] as ReturnType<typeof run>[], latest: undefined, review: undefined, decisions: [], requests: [], next: [], waiting: [] }

  it('answers an open decision of the lane first, in lantern', () => {
    const d = decision()
    expect(laneStateAction({ ...base, decisions: [d] })?.kind).toBe('answer')
  })

  it('routes a request to route next, in lantern', () => {
    const r = request()
    expect(laneStateAction({ ...base, requests: [r] })?.kind).toBe('route')
  })

  it('follows a running run', () => {
    const running = run({ state: 'running' })
    expect(laneStateAction({ ...base, runs: [running], latest: running })?.kind).toBe('follow')
  })

  it('opens the log of a failed run', () => {
    const failed = run({ state: 'failed' })
    expect(laneStateAction({ ...base, runs: [failed], latest: failed })?.kind).toBe('open')
  })

  it('shows no primary when nothing waits', () => {
    expect(laneStateAction(base)).toBeUndefined()
  })

  it('opens next up when an approved item of the repo waits', () => {
    const item = nextItem()
    expect(laneStateAction({ ...base, next: [item] })?.kind).toBe('next')
  })

  it('shows no primary on a closed lane', () => {
    expect(laneStateAction({ ...base, lane: lane({ state: 'closed' }), decisions: [decision()] })).toBeUndefined()
  })
})

import { render as rtlRender, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, run, spend, status } from '../test/fixtures'
import { InspectorFrame, isWide } from './Inspector'
import { laneTab } from './lane'
import { mrTab } from './mr'

vi.mock('../api/client', async (orig) => ({
  ...(await orig<typeof import('../api/client')>()),
  laneChanges: { diff: async () => ({ committed: [{}, {}], uncommitted: [{}], untracked: [], commits: [], out_of_scope: 0 }) },
  laneApprove: {
    read: async () => ({ supported: true, mr: 7, mr_state: 'opened', mr_url: 'https://git.example/mr/7', sha: 'abcdef1234567', approved: false, require_approval: 'manual' }),
    approve: vi.fn(async () => ({ retried: true })),
  },
}))
vi.mock('../api/artifacts', async (orig) => ({
  ...(await orig<typeof import('../api/artifacts')>()),
  laneArtifacts: async () => ({ artifacts: [{ path: 'a.md' }, { path: 'b.md' }, { path: 'c.md' }], globs: [] }),
}))

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
const api = {
  status: async () => status(),
  lanes: async () => [lane({ id: 4, branch: 'feat/export-csv', mr: 7, mr_state: 'open' })],
  runs: async () => [run({ id: 11, lane_id: 4 }), run({ id: 12, lane_id: 4 })],
  decisions: async () => [], requests: async () => [],
  spend: async () => spend(), feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
} as unknown as Api
const render = (ui: ReactNode) => rtlRender(<LiveProvider live={new Live(api, still)}>{ui}</LiveProvider>)

describe('laneTab', () => {
  it('reads the tab from the address and defaults to the overview', () => {
    expect(laneTab('changes')).toBe('changes')
    expect(laneTab('runs')).toBe('runs')
    expect(laneTab(undefined)).toBe('overview')
    expect(laneTab('bogus')).toBe('overview')
    expect(mrTab('artifacts')).toBe('artifacts')
    expect(mrTab(undefined)).toBe('approval')
  })
})

describe('isWide', () => {
  it('audits which views are cramped in the split pane', () => {
    const wide = [
      { kind: 'lane', id: 1, tab: 'changes' }, { kind: 'lane', id: 1, tab: 'artifacts' },
      { kind: 'mr', id: 1, tab: 'changes' }, { kind: 'mr', id: 1, tab: 'artifacts' },
      { kind: 'run', id: 1, tab: 'log' }, { kind: 'run', id: 1, tab: 'changes' }, { kind: 'artifact', id: 1 },
    ]
    const narrow = [
      { kind: 'lane', id: 1 }, { kind: 'lane', id: 1, tab: 'conversation' }, { kind: 'lane', id: 1, tab: 'runs' },
      { kind: 'mr', id: 1 }, { kind: 'run', id: 1 }, { kind: 'run', id: 1, tab: 'asked' }, { kind: 'decision', id: 1 }, { kind: 'question', id: 1 },
    ]
    for (const o of wide) expect(isWide(o), JSON.stringify(o)).toBe(true)
    for (const o of narrow) expect(isWide(o), JSON.stringify(o)).toBe(false)
  })
})

describe('the lane inspector tabs', () => {
  beforeEach(() => {
    window.location.hash = '#/?open=lane:4'
  })
  it('uses the kit tabs with counts, and a Runs tab', async () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4 }} mode="split">page</InspectorFrame>)
    const tabs = await screen.findByRole('tablist', { name: 'Lane views' })
    expect(tabs).toHaveClass('ui-tabs-list')
    expect(await within(tabs).findByRole('tab', { name: 'Changes 3' })).toBeInTheDocument()
    expect(within(tabs).getByRole('tab', { name: 'Artifacts 3' })).toBeInTheDocument()
    expect(within(tabs).getByRole('tab', { name: 'Runs 2' })).toBeInTheDocument()
    expect(within(tabs).getByRole('tab', { name: 'Overview' })).toHaveAttribute('aria-selected', 'true')
  })
  it('lists the lane runs on the Runs tab', async () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'runs' }} mode="split">page</InspectorFrame>)
    expect(await screen.findByRole('link', { name: 'Run 12' })).toHaveAttribute('href', '#/?open=run%3A12')
  })
})

describe('the merge request inspector', () => {
  beforeEach(() => {
    window.location.hash = '#/?open=mr:4'
  })
  it('names the merge request, has its five tabs and approves from the Approval tab', async () => {
    const user = userEvent.setup()
    render(<InspectorFrame open={{ kind: 'mr', id: 4 }} mode="split">page</InspectorFrame>)
    const tabs = await screen.findByRole('tablist', { name: 'Merge request views' })
    expect(within(tabs).getAllByRole('tab').map((t) => t.textContent?.replace(/\s*\d+$/, ''))).toEqual(['Approval', 'Changes', 'Artifacts', 'Conversation', 'Runs'])
    expect(await screen.findByRole('heading', { name: 'MR !7' })).toBeInTheDocument()
    await user.click(await screen.findByRole('button', { name: /^Approve !7/ }))
    await user.click(screen.getByRole('button', { name: /^Approve at / }))
    expect(await screen.findByText(/Approved\./)).toBeInTheDocument()
    const { laneApprove } = await import('../api/client')
    expect(laneApprove.approve).toHaveBeenCalledWith(4, 'abcdef1234567')
  })
  it('switches tab through the address, keeping the kind', async () => {
    const user = userEvent.setup()
    render(<InspectorFrame open={{ kind: 'mr', id: 4 }} mode="split">page</InspectorFrame>)
    await user.click(await screen.findByRole('tab', { name: /^Runs/ }))
    expect(window.location.hash).toBe('#/?open=mr%3A4%2Fruns')
  })
})

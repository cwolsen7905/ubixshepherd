import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Live, type Api, type Clock } from '../state/live'
import { LiveProvider } from '../state/context'
import { decision, lane, spend, status } from '../test/fixtures'
import type { Waiting, WaitingItem } from '../api/types'
import { ApiError, history, laneApprove, laneChanges, nextUp, waitingOnYou } from '../api/client'
import { STEP_EVENT } from '../model/waitingStep'
import { MrInspector } from '../inspector/mr'
import { InspectorFrame } from '../inspector/Inspector'
import { InboxPage } from './Inbox'

const still: Clock = { now: () => Date.parse('2026-10-08T10:05:00Z'), hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }

const waiting: Waiting = {
  count: 6,
  counts: { decisions: 1, mrs: 1, ship: 0, next: 1, blocked: 1 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 7, lane: 'a', title: 'Which store?' },
    { kind: 'mr', action: 'approve', lane_id: 3, lane: 'b', title: 'Add retries', mr: 12 },
    { kind: 'next', action: 'review', next_id: 5, title: 'Proposed item' },
    { kind: 'ship', action: 'ship', lane_id: 6, lane: 'd', title: 'Ship lane d' },
    { kind: 'blocked', action: 'open', lane_id: 4, lane: 'c', title: 'Lane c is blocked' },
    { kind: 'request', action: 'route', request_id: 9, title: 'Needs a lane' },
    { kind: 'next', action: 'decide' as WaitingItem['action'], next_id: 8, title: 'Old item', detail: 'done or drop?: its lane closed unmerged' },
  ],
}

async function show(kind?: string, w: Waiting = waiting) {
  const api = {
    status: async () => status(),
    lanes: async () => [],
    runs: async () => [],
    decisions: async (s: string) => (s === 'open' ? [decision({ id: 7 })] : []),
    requests: async () => [],
    spend: async () => spend(),
    feedLatest: async () => ({ items: [], events: [], last: 0 }),
    feed: async (after: number) => ({ items: [], events: [], last: after }),
    next: async () => [],
  } as unknown as Api
  const live = new Live(api, still, async () => w)
  await act(async () => {
    await live.refresh()
    await live.readWaiting()
  })
  render(
    <LiveProvider live={live}>
      <InboxPage kind={kind} />
    </LiveProvider>,
  )
}

describe('InboxPage', () => {
  beforeEach(() => {
    window.location.hash = '#/inbox'
  })

  it('groups what waits by what the person does', async () => {
    await show()
    expect(screen.getByRole('heading', { name: /Answer/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Approve/ })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /Broken/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Waiting on you/ })).toBeInTheDocument()
    // Each waiting question, decision and request carries its typed-ID chip.
    expect(screen.getByText('decision 7')).toHaveAttribute('data-component', 'IdChip')
    expect(screen.getByText('request 9')).toHaveAttribute('data-component', 'IdChip')
    expect(screen.getByText(/move/)).toBeInTheDocument()
  })

  it('lists a merged lane with undecided files, and a kind it does not know, under To tidy', async () => {
    const items = [
      ...waiting.items,
      { kind: 'artifact', action: 'open', lane_id: 11, lane: 'e', title: 'Undecided files hold lane e open' },
      { kind: 'from-a-newer-daemon', action: 'open', title: 'Something new' },
    ] as unknown as Waiting['items']
    await show(undefined, { ...waiting, count: waiting.count + 2, items })
    expect(screen.getByText('Undecided files hold lane e open')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: /To tidy/ })).toBeInTheDocument()
    expect(screen.getByText('Something new')).toBeInTheDocument()
  })

  it('filters to one kind', async () => {
    await show('decision')
    expect(screen.getByText('Which store?')).toBeInTheDocument()
    expect(screen.queryByText('Add retries')).toBeNull()
  })

  it('opens a decision row in the inspector', async () => {
    await show()
    const group = screen.getByRole('heading', { name: /Answer/ }).closest('section') as HTMLElement
    await userEvent.click(within(group).getByRole('button', { name: (_, el) => el instanceof HTMLElement && el.querySelector('.sh-row-title')?.textContent === 'Which store?' }))
    expect(decodeURIComponent(window.location.hash)).toContain('open=decision:7')
  })

  it('opens a merge request row as the merge request', async () => {
    await show()
    await userEvent.click(screen.getByRole('button', { name: /Add retries/ }))
    expect(decodeURIComponent(window.location.hash)).toContain('open=mr:3')
  })

  it('opens a request to route in the side panel', async () => {
    await show()
    expect(screen.queryByText(/shepherd request route 9/)).toBeNull()
    await userEvent.click(screen.getByText('Needs a lane'))
    expect(window.location.hash).toContain('open=request%3A9')
  })

  it('scrolls a decision deep link to the middle and marks it', async () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    window.location.hash = '#/inbox?kind=decision&open=decision:7'
    await show('decision')
    const row = document.querySelector('.inbox-item.is-focus-target .sh-row') as HTMLElement
    expect(row).not.toBeNull()
    expect(scroll).toHaveBeenCalledWith({ block: 'center' })
  })

  it('focuses the inspector when the open row is chosen again', async () => {
    window.location.hash = '#/inbox?open=mr:3'
    await show()
    const h = document.createElement('h2')
    h.id = 'inspector-title'
    h.tabIndex = -1
    document.body.append(h)
    await userEvent.click(screen.getByRole('button', { name: /Add retries/ }))
    expect(h).toHaveFocus()
    h.remove()
  })

  it('ships a lane in place and says what happened', async () => {
    const ship = vi.spyOn(waitingOnYou, 'ship').mockResolvedValue({ lane: 'd', commits: 1, pushed: true, message: 'Pushed 1 commit.' })
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(ship).toHaveBeenCalledWith(6)
    expect(await screen.findByRole('status')).toHaveTextContent('Pushed 1 commit.')
    ship.mockRestore()
  })

  it('shows a refused ship as an alert', async () => {
    const ship = vi.spyOn(waitingOnYou, 'ship').mockRejectedValue(new ApiError(409, 'gate failed'))
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Ship' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not shipped: gate failed')
    ship.mockRestore()
  })

  it('offers done, drop and still needed on a done-or-drop suggestion', async () => {
    const update = vi.spyOn(nextUp, 'update').mockResolvedValue({} as never)
    await show()
    expect(screen.getByText(/done or drop\?: its lane closed unmerged/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Drop' }))
    expect(update).toHaveBeenLastCalledWith(8, { state: 'dropped' })
    await userEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(update).toHaveBeenLastCalledWith(8, { state: 'done' })
    await userEvent.click(screen.getByRole('button', { name: 'Still needed' }))
    expect(update).toHaveBeenLastCalledWith(8, { suggestion: '' })
    // A proposal waiting for approval has no such buttons.
    expect(screen.getAllByRole('button', { name: 'Done' })).toHaveLength(1)
    update.mockRestore()
  })

  it('shows a refused suggestion answer as an alert', async () => {
    const update = vi.spyOn(nextUp, 'update').mockRejectedValue(new ApiError(403, 'only the person'))
    await show()
    await userEvent.click(screen.getByRole('button', { name: 'Done' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not changed: only the person')
    update.mockRestore()
  })

  it('moves the cursor with j', async () => {
    await show()
    const at = () => document.querySelector('.sh-row.is-selected .sh-row-title')?.textContent
    expect(at()).toBeUndefined()
    await userEvent.keyboard('j')
    expect(at()).toBe('Which store?')
    await userEvent.keyboard('j')
    expect(at()).toBe('Add retries')
    await userEvent.keyboard('k')
    expect(at()).toBe('Which store?')
  })

  it('jumps to the next waiting item with ]', async () => {
    await show()
    await userEvent.keyboard('j')
    await userEvent.keyboard(']')
    expect(decodeURIComponent(window.location.hash)).toContain('open=mr:3')
  })

  it('marks the row of the open merge request as selected', async () => {
    window.location.hash = '#/inbox?open=mr:3'
    await show()
    const row = screen.getByRole('button', { name: /Add retries/ })
    expect(row).toHaveClass('is-selected')
    expect(row).toHaveAttribute('aria-pressed', 'true')
  })

  it('filters the groups with the chips', async () => {
    await show()
    await userEvent.click(screen.getByRole('button', { name: /^Broken/ }))
    expect(screen.getByText('Lane c is blocked')).toBeInTheDocument()
    expect(screen.queryByText('Which store?')).toBeNull()
  })

  it('lists merge requests merged today under Done today, without a rule name', async () => {
    const lanes = vi.spyOn(history, 'lanes').mockResolvedValue([
      { id: 9, branch: 'fix/x', repo: 'acme', mr: 40, outcome: 'merged', closed: new Date().toISOString() } as never,
      { id: 10, branch: 'fix/old', repo: 'acme', mr: 30, outcome: 'merged', closed: '2026-10-07T09:00:00Z' } as never,
    ])
    await show()
    expect(await screen.findByText('!40 fix/x')).toBeInTheDocument()
    expect(screen.queryByText('!30 fix/old')).toBeNull()
    expect(screen.queryByText(/Merged by rule/)).toBeNull()
    lanes.mockRestore()
  })

  it('reads Done today as soon as the workspace list arrives, not after the throttle', async () => {
    const lanes = vi.spyOn(history, 'lanes').mockResolvedValue([
      { id: 9, branch: 'fix/x', repo: 'acme', mr: 40, outcome: 'merged', closed: new Date().toISOString() } as never,
    ])
    const api = {
      status: async () => status(), lanes: async () => [], runs: async () => [], decisions: async () => [], requests: async () => [], spend: async () => spend(),
      feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }), next: async () => [],
    } as unknown as Api
    const live = new Live(api, still, async () => waiting)
    // The page mounts before the first status read, so its first Done today read has no workspace to ask.
    render(
      <LiveProvider live={live}>
        <InboxPage />
      </LiveProvider>,
    )
    expect(lanes).not.toHaveBeenCalled()
    await act(async () => {
      await live.refresh()
    })
    expect(await screen.findByText('!40 fix/x')).toBeInTheDocument()
    lanes.mockRestore()
  })
})

let liveOf: Live | undefined

describe('MR approval panel', () => {
  const diff = {
    lane_id: 3, lane: 'b', branch: 'feat/x', base: 'dev', base_ref: 'dev', head: '4be7d19c0000', mode: 'stat', read_at: '2026-10-08T10:00:00Z',
    commits: [{ sha: 'a'.repeat(40), subject: 'feat: add retries', files: 2 }], commits_total: 1,
    committed: [{ path: 'src/a.go', status: 'modified', added: 1, removed: 0, in_scope: true }, { path: 'docs/x.md', status: 'added', added: 1, removed: 0, in_scope: false }],
    uncommitted: [], untracked: [], out_of_scope: 1,
  }
  const view = { supported: true, mr: 12, mr_state: 'opened', sha: '4be7d19c0000', approved: false, pipeline: 9001, pipeline_status: 'success', require_approval: 'manual' }

  async function panel() {
    const api = {
      status: async () => status(), lanes: async () => [lane({ id: 3, branch: 'feat/x', mr: 12, mr_state: 'open' }), lane({ id: 4, branch: 'feat/other', mr: 43, mr_state: 'open' })], runs: async () => [], decisions: async () => [], requests: async () => [], spend: async () => spend(),
      feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }), next: async () => [],
    } as unknown as Api
    const live = new Live(api, still, async () => waiting)
    await act(async () => {
      await live.refresh()
      await live.readWaiting()
    })
    window.location.hash = '#/inbox?open=mr:3'
    liveOf = live
    render(
      <LiveProvider live={live}>
        <MrInspector open={{ kind: 'mr', id: 3 }} />
      </LiveProvider>,
    )
  }

  it('names the selected merge request in the crumbs and the title, and its row is selected', async () => {
    vi.spyOn(laneApprove, 'read').mockResolvedValue(view)
    vi.spyOn(laneChanges, 'diff').mockResolvedValue(diff as never)
    await panel()
    if (!liveOf) throw new Error('no live')
    // The panel and the list share the address: open=mr:3 is lane 3's merge request, !12.
    const { unmount } = render(
      <LiveProvider live={liveOf}>
        <InspectorFrame open={{ kind: 'mr', id: 3 }} mode="split">page</InspectorFrame>
      </LiveProvider>,
    )
    const aside = await screen.findByRole('complementary')
    expect(within(aside).getByText('feat/x')).toBeInTheDocument()
    expect(within(aside).getByText('MR !12')).toBeInTheDocument()
    expect(within(aside).queryByText('feat/other')).toBeNull()
    unmount()
  })

  it('asks the question with the head, the commits and what is outside scope', async () => {
    vi.spyOn(laneApprove, 'read').mockResolvedValue(view)
    vi.spyOn(laneChanges, 'diff').mockResolvedValue(diff as never)
    await panel()
    expect(await screen.findByRole('heading', { name: 'Approve merge request !12?' })).toBeInTheDocument()
    expect(screen.getByText(/You are approving head 4be7d19c; a newer push asks again/)).toBeInTheDocument()
    expect(await screen.findByText('feat: add retries')).toBeInTheDocument()
    expect(screen.getByText('1 commit, 2 files, 1 outside scope')).toBeInTheDocument()
    expect(screen.getByText(/docs\/x\.md/)).toBeInTheDocument()
    expect(screen.getByText('unreviewed')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Request AI review' })).toBeInTheDocument()
  })

  it('approves the head it read and moves on', async () => {
    vi.spyOn(laneApprove, 'read').mockResolvedValue(view)
    vi.spyOn(laneChanges, 'diff').mockResolvedValue(diff as never)
    const approve = vi.spyOn(laneApprove, 'approve').mockResolvedValue({ ...view, approved: true, retried: false })
    const stepped = vi.fn()
    window.addEventListener(STEP_EVENT, stepped)
    await panel()
    await userEvent.click(await screen.findByRole('button', { name: 'Approve !12' }))
    await userEvent.click(screen.getByRole('button', { name: /^Approve at / }))
    expect(approve).toHaveBeenCalledWith(3, '4be7d19c0000')
    // The item after it is a next-up proposal, which has its own pane.
    await waitFor(() => expect(window.location.hash).toContain('open=next%3A5'))
    expect(stepped).not.toHaveBeenCalled()
    window.removeEventListener(STEP_EVENT, stepped)
  })

  it('shows a refused approval as an alert', async () => {
    vi.spyOn(laneApprove, 'read').mockResolvedValue(view)
    vi.spyOn(laneChanges, 'diff').mockResolvedValue(diff as never)
    vi.spyOn(laneApprove, 'approve').mockRejectedValue(new ApiError(409, 'head moved'))
    await panel()
    await userEvent.click(await screen.findByRole('button', { name: 'Approve !12' }))
    await userEvent.click(screen.getByRole('button', { name: /^Approve at / }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Not approved: head moved')
  })

  it('shows the approved state and drops the Approve button after approving', async () => {
    const read = vi.spyOn(laneApprove, 'read').mockResolvedValueOnce(view).mockResolvedValue({ ...view, approved: true })
    vi.spyOn(laneChanges, 'diff').mockResolvedValue(diff as never)
    vi.spyOn(laneApprove, 'approve').mockResolvedValue({ ...view, approved: true, retried: false })
    await panel()
    await userEvent.click(await screen.findByRole('button', { name: 'Approve !12' }))
    await userEvent.click(screen.getByRole('button', { name: /^Approve at / }))
    expect(await screen.findByRole('heading', { name: 'Merge request !12 is approved' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Approve !12' })).toBeNull()
    expect(read).toHaveBeenCalledTimes(2)
  })
})

import { render as rtlRender, screen, within, type RenderOptions } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement, ReactNode } from 'react'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { decision, FakeSource, fakeDeskApi, lane, spend, status } from '../test/fixtures'
import { DeskThread } from '../state/desk'
import { act, waitFor } from '@testing-library/react'
import { InspectorFrame } from './Inspector'
import { storedArtifacts } from '../artifacts/api'
import { modeFor } from './useMode'

const open = { kind: 'topic', id: 5 }

const still: Clock = { now: () => 0, hidden: () => false, setTimeout: () => 0, clearTimeout: () => {} }
const api = {
  status: async () => status(), lanes: async () => [lane({ id: 4, branch: 'feat/export-csv' })], runs: async () => [], decisions: async () => [], requests: async () => [],
  spend: async () => spend(), feedLatest: async () => ({ items: [], events: [], last: 0 }), feed: async (after: number) => ({ items: [], events: [], last: after }),
} as unknown as Api
// The inspector sits inside the app's live provider, as in production.
function Wrap({ children }: { children: ReactNode }) {
  return <LiveProvider live={new Live(api, still)}>{children}</LiveProvider>
}
const render = (ui: ReactElement, o?: RenderOptions) => rtlRender(ui, { wrapper: Wrap, ...o })

describe('modeFor', () => {
  it('switches at 1440 and 760', () => {
    expect(modeFor(1440)).toBe('split')
    expect(modeFor(1439)).toBe('overlay')
    expect(modeFor(760)).toBe('overlay')
    expect(modeFor(759)).toBe('sheet')
  })
})

describe('InspectorFrame', () => {
  beforeEach(() => {
    window.location.hash = '#/board?open=run:5'
  })

  it('shows nothing extra when nothing is open', () => {
    render(<InspectorFrame>page</InspectorFrame>)
    expect(screen.getByText('page')).toBeInTheDocument()
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('names what is open and focuses the heading', () => {
    render(<InspectorFrame open={open} mode="split">page</InspectorFrame>)
    expect(screen.getByText('Open: topic 5')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'topic 5' })).toHaveFocus()
  })

  it('shows a breadcrumb head with Open as page and Close Esc', () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4 }} mode="split">page</InspectorFrame>)
    // The lane pane's body carries its own link too, so look in the head only.
    const head = within(document.querySelector('.inspector-head') as HTMLElement)
    expect(head.getByRole('link', { name: 'Open as page' })).toHaveAttribute('href', '#/lanes/4')
    expect(screen.getByRole('button', { name: 'Close inspector' })).toHaveTextContent('Close Esc')
  })

  it('links a run to its log and a decision to the Conversation, where it fills the main column', () => {
    const { unmount } = render(<InspectorFrame open={{ kind: 'run', id: 5 }} mode="split">page</InspectorFrame>)
    const head = within(document.querySelector('.inspector-head') as HTMLElement)
    expect(head.getByRole('link', { name: 'Open as page' })).toHaveAttribute('href', '#/runs/5/log')
    unmount()
    render(<InspectorFrame open={{ kind: 'decision', id: 5 }} mode="split">page</InspectorFrame>)
    expect(within(document.querySelector('.inspector-head') as HTMLElement).getByRole('link', { name: 'Open as page' })).toHaveAttribute('href', '#/?open=decision%3A5')
  })

  it('has one header: the lane pane does not repeat the lane name or Open as page', async () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4 }} mode="split">page</InspectorFrame>)
    await screen.findByRole('tablist', { name: 'Lane views' })
    expect(screen.getAllByRole('link', { name: 'Open as page' })).toHaveLength(1)
    expect(document.querySelector('.lane-sub')).toBeNull()
    expect(screen.getAllByText('feat/export-csv').length).toBeGreaterThan(0)
  })

  it('offers Open full width on a cramped tab, and a back arrow once there', async () => {
    const user = userEvent.setup()
    window.location.hash = '#/?open=lane:4/changes'
    const { rerender } = render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'changes' }} mode="split">page</InspectorFrame>)
    await user.click(screen.getByRole('button', { name: 'Open full width' }))
    expect(window.location.hash).toBe('#/?open=lane%3A4%2Fchanges%2Ffull')
    rerender(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'changes', full: true }} mode="split">page</InspectorFrame>)
    expect(screen.queryByRole('button', { name: 'Open full width' })).toBeNull()
    expect(screen.getByText('page').closest('.inspector-page')).toHaveAttribute('hidden')
    // The page header's way back to the side panel, and the one way out of the page.
    await screen.findByRole('tablist', { name: 'Lane views' })
    await user.click(screen.getByRole('button', { name: /^Open beside the chat/ }))
    expect(window.location.hash).toBe('#/?open=lane%3A4%2Fchanges')
  })

  it('draws a full-width merge request, run or lane under the page header, with its tabs in it', async () => {
    window.location.hash = '#/inbox?open=mr:4/full'
    render(<InspectorFrame open={{ kind: 'mr', id: 4, full: true }} mode="split">page</InspectorFrame>)
    const tabs = await screen.findByRole('tablist', { name: 'Merge request views' })
    const head = tabs.closest('[data-component="PageHeader"]') as HTMLElement
    expect(head).not.toBeNull()
    expect(within(head).getByRole('heading', { level: 1 })).toHaveTextContent('Merge request')
    expect(within(head).getByRole('button', { name: /^Close$/ })).toBeInTheDocument()
    expect(within(head).getByRole('button', { name: /^Open beside the inbox\s*Esc$/ })).toBeInTheDocument()
    expect(document.querySelector('.inspector-head')).toBeNull()
    // The pane is still labelled by, and focus can still be sent to, the title.
    expect(document.getElementById('inspector-title')).toBe(within(head).getByRole('heading', { level: 1 }))
  })

  it('shows the approval glyph on a merge request only while it waits for the person to approve', async () => {
    const withWaiting = (items: unknown[]) => {
      const live = new Live(api, still, async () => ({ items, count: items.length, counts: { decisions: 0, mrs: 0, ship: 0, next: 0 } }) as never)
      return ({ children }: { children: ReactNode }) => <LiveProvider live={live}>{children}</LiveProvider>
    }
    const mr = <InspectorFrame open={{ kind: 'mr', id: 4, full: true }} mode="split">page</InspectorFrame>
    window.location.hash = '#/inbox?open=mr:4/full'
    const none = rtlRender(mr, { wrapper: withWaiting([]) })
    const tabs = await screen.findByRole('tablist', { name: 'Merge request views' })
    expect(within(tabs.closest('[data-component="PageHeader"]') as HTMLElement).queryByText('waiting for your approval')).toBeNull()
    none.unmount()
    rtlRender(mr, { wrapper: withWaiting([{ kind: 'mr', action: 'approve', lane_id: 4, mr: 41, title: '' }]) })
    expect(await within(document.querySelector('[data-component="PageHeader"]') as HTMLElement).findByText('waiting for your approval')).toBeInTheDocument()
  })

  it('gives a full-width decision the page header too, with the way back and where it lives', () => {
    window.location.hash = '#/inbox?open=decision:5/full'
    render(<InspectorFrame open={{ kind: 'decision', id: 5, full: true }} mode="split">page</InspectorFrame>)
    const head = document.querySelector('[data-component="PageHeader"]') as HTMLElement
    expect(within(head).getByRole('heading', { level: 1, name: 'decision 5' })).toHaveFocus()
    expect(within(head).getByRole('button', { name: '← Inbox' })).toBeInTheDocument()
    expect(within(head).getByRole('link', { name: 'Open as page' })).toBeInTheDocument()
  })

  it('moves focus to the swapped-in button when entering and leaving full width', async () => {
    const user = userEvent.setup()
    window.location.hash = '#/?open=lane:4/changes'
    const { rerender } = render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'changes' }} mode="split">page</InspectorFrame>)
    await user.click(screen.getByRole('button', { name: 'Open full width' }))
    rerender(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'changes', full: true }} mode="split">page</InspectorFrame>)
    expect(screen.getByRole('button', { name: '← Chat' })).toHaveFocus()
    await user.click(screen.getByRole('button', { name: /^Open beside the chat/ }))
    rerender(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'changes' }} mode="split">page</InspectorFrame>)
    expect(screen.getByRole('button', { name: 'Open full width' })).toHaveFocus()
  })

  it('leaves a full-width view on Esc instead of closing', async () => {
    const user = userEvent.setup()
    window.location.hash = '#/?open=lane:4/artifacts/full'
    render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'artifacts', full: true }} mode="split">page</InspectorFrame>)
    await user.keyboard('{Escape}')
    expect(window.location.hash).toBe('#/?open=lane%3A4%2Fartifacts')
  })

  it('names an open artifact by its title in the header chip', async () => {
    const one = vi.spyOn(storedArtifacts, 'one').mockResolvedValue({ id: 1, title: 'Export orders as CSV', path: 'docs/plans/export-csv.md', versions: [] } as never)
    window.location.hash = '#/artifacts?open=artifact:1'
    render(<InspectorFrame open={{ kind: 'artifact', id: 1 }} mode="split">page</InspectorFrame>)
    expect(await screen.findByRole('heading', { name: 'Export orders as CSV', level: 2 })).toBeInTheDocument()
    one.mockRestore()
  })

  it('does not offer full width on a view that fits', () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'runs' }} mode="split">page</InspectorFrame>)
    expect(screen.queryByRole('button', { name: 'Open full width' })).toBeNull()
  })

  it('returns focus on close and closes on Esc', async () => {
    const user = userEvent.setup()
    const { rerender } = render(
      <InspectorFrame mode="split">
        <button>row</button>
      </InspectorFrame>,
    )
    screen.getByText('row').focus()
    rerender(
      <InspectorFrame open={open} mode="split">
        <button>row</button>
      </InspectorFrame>,
    )
    expect(screen.getByRole('heading')).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(window.location.hash).toBe('#/board')
    rerender(
      <InspectorFrame mode="split">
        <button>row</button>
      </InspectorFrame>,
    )
    expect(screen.getByText('row')).toHaveFocus()
  })

  it('traps Tab and makes the page inert in overlay and sheet', async () => {
    const user = userEvent.setup()
    for (const mode of ['overlay', 'sheet'] as const) {
      const { unmount } = render(
        <InspectorFrame open={{ kind: 'decision', id: 5 }} mode={mode}>
          <button>row</button>
        </InspectorFrame>,
      )
      const close = screen.getByRole('button', { name: 'Close inspector' })
      expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
      expect(screen.getByText('row').closest('.inspector-page')).toHaveAttribute('inert')
      // The head's Open as page link is the first stop, the composer's box the last (Send is
      // disabled while it is empty); the gone-decision note's own link sits between.
      const box = screen.getByRole('textbox')
      const page = screen.getByRole('link', { name: 'Open as page' })
      await user.tab()
      expect(page).toHaveFocus()
      await user.tab()
      expect(close).toHaveFocus()
      await user.tab()
      expect(screen.getByRole('link', { name: 'Open it on the Decisions page' })).toHaveFocus()
      await user.tab()
      expect(box).toHaveFocus()
      await user.tab()
      expect(page).toHaveFocus()
      await user.tab({ shift: true })
      expect(box).toHaveFocus()
      unmount()
    }
  })

  it('returns focus to a shell element that was inert while the sheet was open', () => {
    const tree = (o?: typeof open) => (
      <div>
        <nav>
          <button>nav</button>
        </nav>
        <InspectorFrame open={o} mode="sheet">
          <button>row</button>
        </InspectorFrame>
      </div>
    )
    const { rerender } = render(tree())
    screen.getByText('nav').focus()
    rerender(tree(open))
    expect(screen.getByText('nav').closest('nav')).toHaveAttribute('inert')
    rerender(tree())
    expect(screen.getByText('nav')).toHaveFocus()
  })

  it('makes the shell inert around a modal inspector, and restores it', () => {
    const { rerender } = render(
      <div>
        <nav>shell</nav>
        <InspectorFrame open={open} mode="sheet">
          <button>row</button>
        </InspectorFrame>
      </div>,
    )
    expect(screen.getByText('shell')).toHaveAttribute('inert')
    expect(document.querySelector('.inspector-scrim')).not.toHaveAttribute('inert')
    rerender(
      <div>
        <nav>shell</nav>
        <InspectorFrame mode="sheet">
          <button>row</button>
        </InspectorFrame>
      </div>,
    )
    expect(screen.getByText('shell')).not.toHaveAttribute('inert')
  })

  it('leaves the page live in split mode', () => {
    render(
      <InspectorFrame open={open} mode="split">
        <button>row</button>
      </InspectorFrame>,
    )
    expect(screen.getByText('row').closest('.inspector-page')).not.toHaveAttribute('inert')
  })
})

describe('the context composer in the inspector', () => {
  it('names a lane by its branch', async () => {
    render(<InspectorFrame open={{ kind: 'lane', id: 4 }} mode="split">page</InspectorFrame>)
    expect(await screen.findByText('Message the front desk about feat/export-csv (lane 4)')).toBeInTheDocument()
  })
  it('is not shown for a kind it cannot speak about', () => {
    render(<InspectorFrame open={{ kind: 'constructor', id: 4 }} mode="split">page</InspectorFrame>)
    expect(screen.queryByRole('textbox')).toBeNull()
  })
})

describe('the swapped layout (WEB-012)', () => {
  const swapApi = { ...api, decisions: async (state?: string) => (state === 'open' ? [decision()] : []) } as unknown as Api
  const make = (id: number) => new DeskThread(id, { api: fakeDeskApi({}), open: (u) => new FakeSource(u) })
  async function swapped(hash: string, mode: 'split' | 'overlay' = 'split') {
    window.location.hash = hash
    const live = new Live(swapApi, still)
    await act(async () => {
      await live.refresh()
    })
    const view = rtlRender(
      <LiveProvider live={live}>
        <InspectorFrame open={{ kind: 'decision', id: 5 }} mode={mode} thread={make}>
          <p>the thread page</p>
        </InspectorFrame>
      </LiveProvider>,
    )
    return view
  }

  it('puts the decision in the main column and the filtered thread with its composer in the side', async () => {
    await swapped('#/?open=decision%3A5')
    expect(screen.queryByText('the thread page')).toBeNull()
    const main = document.querySelector('.sh-swap-main') as HTMLElement
    expect(within(main).getByRole('heading', { level: 1, name: 'Should we use authentication?' })).toBeInTheDocument()
    expect(within(main).getByRole('button', { name: '← Conversation' })).toBeInTheDocument()
    const side = await screen.findByRole('complementary', { name: 'The conversation about this decision' })
    await waitFor(() => expect(within(side).getByRole('textbox', { name: 'Message to the front desk' })).toBeInTheDocument())
  })

  it('swaps back on Esc', async () => {
    await swapped('#/?open=decision%3A5')
    await userEvent.keyboard('{Escape}')
    expect(window.location.hash).toBe('#/')
  })

  it('is not swapped for an item answered in a panel, or off the Conversation', async () => {
    window.location.hash = '#/?open=mr:1'
    rtlRender(<InspectorFrame open={{ kind: 'mr', id: 1 }} mode="split">the thread page</InspectorFrame>, { wrapper: Wrap })
    expect(screen.getByText('the thread page')).toBeInTheDocument()
    expect(document.querySelector('.sh-swap-main')).toBeNull()
  })

  it('has no thread beside the decision below the split width', async () => {
    await swapped('#/?open=decision%3A5', 'overlay')
    expect(document.querySelector('.sh-swap-side')).toBeNull()
    expect(screen.getByRole('radiogroup', { name: /Options for/ })).toBeInTheDocument()
  })
})

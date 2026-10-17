import { render as rtlRender, screen, within, type RenderOptions } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement, ReactNode } from 'react'
import { LiveProvider } from '../state/context'
import { Live, type Api, type Clock } from '../state/live'
import { lane, spend, status } from '../test/fixtures'
import { InspectorFrame } from './Inspector'
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
    window.location.hash = '#/?open=run:5'
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

  it('links a run to its log and a decision to the Decisions page', () => {
    const { unmount } = render(<InspectorFrame open={{ kind: 'run', id: 5 }} mode="split">page</InspectorFrame>)
    const head = within(document.querySelector('.inspector-head') as HTMLElement)
    expect(head.getByRole('link', { name: 'Open as page' })).toHaveAttribute('href', '#/runs/5/log')
    unmount()
    render(<InspectorFrame open={{ kind: 'decision', id: 5 }} mode="split">page</InspectorFrame>)
    expect(within(document.querySelector('.inspector-head') as HTMLElement).getByRole('link', { name: 'Open as page' })).toHaveAttribute('href', '#/decisions?d=5')
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
    await user.click(screen.getByRole('button', { name: 'Back to the inspector' }))
    expect(window.location.hash).toBe('#/?open=lane%3A4%2Fchanges')
  })

  it('leaves a full-width view on Esc instead of closing', async () => {
    const user = userEvent.setup()
    window.location.hash = '#/?open=lane:4/artifacts/full'
    render(<InspectorFrame open={{ kind: 'lane', id: 4, tab: 'artifacts', full: true }} mode="split">page</InspectorFrame>)
    await user.keyboard('{Escape}')
    expect(window.location.hash).toBe('#/?open=lane%3A4%2Fartifacts')
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
    expect(window.location.hash).toBe('#/')
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

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { InspectorFrame } from './Inspector'
import { modeFor } from './useMode'

const open = { kind: 'run', id: 5 }

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
    expect(screen.getByText('Open: run 5')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'run 5' })).toHaveFocus()
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
        <InspectorFrame open={open} mode={mode}>
          <button>row</button>
        </InspectorFrame>,
      )
      const close = screen.getByRole('button', { name: 'Close inspector' })
      expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
      expect(screen.getByText('row').closest('.inspector-page')).toHaveAttribute('inert')
      // The composer's box is the last stop (Send is disabled while it is empty).
      const box = screen.getByRole('textbox')
      await user.tab()
      expect(close).toHaveFocus()
      await user.tab()
      expect(box).toHaveFocus()
      await user.tab()
      expect(close).toHaveFocus()
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

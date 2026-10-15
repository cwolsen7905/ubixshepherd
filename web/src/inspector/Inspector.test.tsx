import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { InspectorFrame } from './Inspector'
import { modeFor } from './useMode'

const open = { kind: 'decision', id: 5 }

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
    window.location.hash = '#/?open=decision:5'
  })

  it('shows nothing extra when nothing is open', () => {
    render(<InspectorFrame>page</InspectorFrame>)
    expect(screen.getByText('page')).toBeInTheDocument()
    expect(screen.queryByRole('complementary')).toBeNull()
  })

  it('names what is open and focuses the heading', () => {
    render(<InspectorFrame open={open} mode="split">page</InspectorFrame>)
    expect(screen.getByText('Open: decision 5')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'decision 5' })).toHaveFocus()
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
      await user.tab()
      expect(close).toHaveFocus()
      await user.tab()
      expect(close).toHaveFocus()
      await user.tab({ shift: true })
      expect(close).toHaveFocus()
      unmount()
    }
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

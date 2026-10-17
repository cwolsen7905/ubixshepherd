import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { OfflineBanner, OfflineProvider } from '../shell/Offline'
import { ContextComposer, GroupHeader, ListRow, OFFLINE_SEND, OfflineContext, offlineWords, retryIn } from './index'

describe('ListRow', () => {
  it('is a two-line row: title over sub-line, with the age', () => {
    render(<ListRow glyph="?" tone="warn" glyphLabel="decision" title="Which index?" sub="decision · feat/export" age="8m" />)
    const row = document.querySelector('.sh-row') as HTMLElement
    expect(row).toHaveClass('sh-row--inbox')
    expect(row).toHaveAttribute('data-component', 'ListRow')
    expect(row.querySelector('.sh-row-main > .sh-row-title')).toHaveTextContent('Which index?')
    expect(row.querySelector('.sh-row-main > .sh-row-sub')).toHaveTextContent('decision · feat/export')
    expect(screen.getByText('decision', { selector: '.visually-hidden' })).toBeInTheDocument()
    expect(row.querySelector('.sh-glyph')).toHaveClass('sh-glyph--warn')
  })

  it('has a lane, an item and a file variant, each its own grid', () => {
    const { rerender } = render(<ListRow variant="lane" glyph="●" title="feat/login" right="!41 ✓" rightTone="ok" age="3h" />)
    expect(document.querySelector('.sh-row')).toHaveClass('sh-row--lane')
    expect(screen.getByText('!41 ✓')).toHaveClass('is-ok')
    rerender(<ListRow variant="file" glyph="▤" title="plan.md" sub="docs/plan.md" right={<span className="sh-tag">kept local</span>} />)
    expect(document.querySelector('.sh-row')).toHaveClass('sh-row--file')
    rerender(<ListRow glyph="?" title="t" />)
    expect(document.querySelector('.sh-row')).toHaveClass('sh-row--inbox')
  })

  it('marks the selected row, selects on click, Enter and Space, and shows an unseen dot', async () => {
    const user = userEvent.setup()
    const onSelect = vi.fn()
    render(<ListRow glyph="?" title="Row" selected unseen age="1m" onSelect={onSelect} />)
    const row = screen.getByRole('button')
    expect(row).toHaveClass('is-selected')
    expect(row).toHaveAttribute('aria-pressed', 'true')
    expect(row.querySelector('.sh-unseen')).toBeInTheDocument()
    await user.click(row)
    row.focus()
    await user.keyboard('{Enter}')
    await user.keyboard(' ')
    expect(onSelect).toHaveBeenCalledTimes(3)
  })

  it('can be a link that navigates', () => {
    render(<ListRow glyph="●" title="Lane" href="#/lanes/3" />)
    expect(screen.getByRole('link')).toHaveAttribute('href', '#/lanes/3')
  })
})

describe('GroupHeader', () => {
  it('has section, status and project variants', () => {
    const { rerender, container } = render(<GroupHeader label="Answer" hint="4 waiting" warn />)
    expect(container.firstChild).toHaveClass('sh-group-h', 'sh-group-h--warn')
    expect(screen.getByText('4 waiting')).toHaveClass('sh-group-h-hint')
    rerender(<GroupHeader variant="status" glyph="✗" glyphTone="bad" label="broken" count={2} />)
    expect(container.querySelector('.sh-glyph--bad')).toHaveTextContent('✗')
    expect(container.firstChild).toHaveTextContent('broken2')
    rerender(<GroupHeader variant="project" label="acme-api" meta="3 lanes" spend="$4.20 today" />)
    expect(container.firstChild).toHaveClass('sh-project-h')
    expect(container.querySelector('b')).toHaveTextContent('acme-api')
    expect(screen.getByText('$4.20 today')).toHaveClass('sh-project-h-spend')
  })
})

function Draft({ onSend = vi.fn(), joined = false }: { onSend?: (v: string) => void; joined?: boolean }) {
  const [v, setV] = useState('')
  return <ContextComposer variant="composer" joined={joined} placeholder="Add to next up…" value={v} onChange={setV} onSend={onSend} />
}

describe('ContextComposer', () => {
  it('has a footer variant with an about line and a composer variant that can sit joined under the pending bar', () => {
    const { container, rerender } = render(<ContextComposer about="About lane feat/login" placeholder="Ask about it" />)
    expect(container.firstChild).toHaveClass('sh-ctx')
    expect(screen.getByText('About lane feat/login')).toHaveClass('sh-ctx-about')
    rerender(<ContextComposer variant="composer" joined placeholder="Message" />)
    expect(container.firstChild).toHaveClass('sh-composer', 'is-joined')
  })

  it('opens on click when it is a static placeholder', async () => {
    const user = userEvent.setup()
    const onOpen = vi.fn()
    render(<ContextComposer variant="composer" placeholder="Message" onOpen={onOpen} />)
    await user.click(screen.getByText('Message'))
    expect(onOpen).toHaveBeenCalled()
  })

  it('sends the draft with the button or Cmd+Enter, and not an empty one', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn()
    render(<Draft onSend={onSend} />)
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
    await user.type(screen.getByRole('textbox'), 'go ahead')
    await user.click(screen.getByRole('button', { name: 'Send' }))
    expect(onSend).toHaveBeenCalledWith('go ahead')
    await user.click(screen.getByRole('textbox'))
    await user.keyboard('{Control>}{Enter}{/Control}')
    expect(onSend).toHaveBeenCalledTimes(2)
  })

  it('offline: Send is disabled, says why, and the draft is kept', async () => {
    const user = userEvent.setup()
    const onSend = vi.fn()
    function Host({ offline }: { offline: boolean }) {
      return (
        <OfflineContext.Provider value={{ offline, retryIn: offline ? 8 : null }}>
          <Draft onSend={onSend} />
        </OfflineContext.Provider>
      )
    }
    const { rerender } = render(<Host offline={false} />)
    await user.type(screen.getByRole('textbox'), 'my long answer')
    rerender(<Host offline />)
    const send = screen.getByRole('button', { name: 'Send' })
    expect(send).toBeDisabled()
    expect(send).toHaveAttribute('title', OFFLINE_SEND)
    expect(screen.getByRole('textbox')).toHaveValue('my long answer')
    await user.click(send)
    await user.click(screen.getByRole('textbox'))
    await user.keyboard('{Control>}{Enter}{/Control}')
    expect(onSend).not.toHaveBeenCalled()
    // The draft is still there, and can still be edited, while offline.
    await user.type(screen.getByRole('textbox'), '!')
    expect(screen.getByRole('textbox')).toHaveValue('my long answer!')
    rerender(<Host offline={false} />)
    expect(screen.getByRole('textbox')).toHaveValue('my long answer!')
    expect(screen.getByRole('button', { name: 'Send' })).toBeEnabled()
  })
})

describe('offline', () => {
  it('walks the retry schedule from the moment the daemon went down', () => {
    const t = 1_000_000
    expect(retryIn(null, t)).toBeNull()
    expect(retryIn(t, t)).toBe(1)
    expect(retryIn(t - 1_500, t)).toBe(2)
    expect(retryIn(t - 5_000, t)).toBe(2)
    expect(retryIn(t - 8_000, t)).toBe(7)
    expect(retryIn(t - 100_000, t)).toBeGreaterThanOrEqual(1)
    expect(retryIn(t - 100_000, t)).toBeLessThanOrEqual(15)
  })

  it('words it as the footer does', () => {
    expect(offlineWords({ offline: true, retryIn: 8 })).toBe('Offline, retrying in 8s')
    expect(offlineWords({ offline: true, retryIn: null })).toBe('Offline, retrying')
  })

  it('shows a banner that says why, and when it retries; nothing while online', () => {
    const { rerender } = render(
      <OfflineProvider error={null} downSince={null}>
        <OfflineBanner error={null} />
      </OfflineProvider>,
    )
    expect(screen.queryByText(/did not answer/)).toBeNull()
    rerender(
      <OfflineProvider error="the daemon could not be reached" downSince={Date.now() - 5_000}>
        <OfflineBanner error="the daemon could not be reached" />
      </OfflineProvider>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('The daemon did not answer: the daemon could not be reached.')
    expect(document.querySelector('.sh-banner')).toHaveTextContent(/retrying in \d+s/)
  })
})

import { fireEvent, render, screen } from '@testing-library/react'
import { Conversation } from './Conversation'
import type { ConversationSource } from './types'

const source: ConversationSource = {
  read: async () => ({ items: [{ seq: 1, kind: 'agent', text: 'hello', run: 1 }], cursor: 1, more: false, running: true }),
  expand: async (seq: number) => ({ seq, kind: 'agent', text: 'hello', run: 1 }),
} as unknown as ConversationSource

let scrollTo: ReturnType<typeof vi.fn>
const saved = window.scrollTo

beforeEach(() => {
  scrollTo = vi.fn()
  window.scrollTo = scrollTo as unknown as typeof window.scrollTo
  // jsdom does not lay out: give every element a height to scroll to.
  Object.defineProperty(HTMLElement.prototype, 'scrollHeight', { configurable: true, get: () => 900 })
})
afterEach(() => {
  window.scrollTo = saved
  delete (HTMLElement.prototype as unknown as Record<string, unknown>).scrollHeight
})

describe('Conversation scrolling', () => {
  it('scrolls only its own scroll container, leaving the Board column alone', async () => {
    render(
      <div data-testid="board" style={{ overflowY: 'auto' }}>
        <div data-testid="panel" style={{ overflowY: 'auto' }}>
          <Conversation source={source} now={0} />
        </div>
      </div>,
    )
    await screen.findByText('hello')
    expect(screen.getByTestId('panel').scrollTop).toBe(900)
    expect(screen.getByTestId('board').scrollTop).toBe(0)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('follows the bottom of the page when nothing around it scrolls', async () => {
    render(<Conversation source={source} now={0} />)
    await screen.findByText('hello')
    expect(scrollTo).toHaveBeenLastCalledWith({ top: document.documentElement.scrollHeight })
  })

  it('skips a wrapper that does not overflow and scrolls the one that does', async () => {
    render(
      <div data-testid="panel" style={{ overflowY: 'auto' }}>
        <div data-testid="wrapper" style={{ overflowY: 'auto' }}>
          <Conversation source={source} now={0} />
        </div>
      </div>,
    )
    // The wrapper grows with its content: nothing of its own to scroll.
    Object.defineProperty(screen.getByTestId('wrapper'), 'scrollHeight', { configurable: true, get: () => 0 })
    await screen.findByText('hello')
    expect(screen.getByTestId('panel').scrollTop).toBe(900)
  })

  it('stops following when the reader scrolls the panel up, and Jump to latest returns', async () => {
    render(
      <div data-testid="panel" style={{ overflowY: 'auto' }}>
        <Conversation source={source} now={0} />
      </div>,
    )
    await screen.findByText('hello')
    const panel = screen.getByTestId('panel')
    expect(screen.queryByRole('button', { name: 'Jump to latest' })).not.toBeInTheDocument()
    panel.scrollTop = 100
    fireEvent.scroll(panel)
    const jump = await screen.findByRole('button', { name: 'Jump to latest' })
    jump.click()
    expect(panel.scrollTop).toBe(900)
  })
})

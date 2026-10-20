import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { Badge, Button, Chip, Dialog, EmptyState, ErrorState, IdChip, Kbd, LoadingRows, LOADING_DELAY_MS, Popover, Skeleton, Tabs, TOAST_MS, ToastProvider, useToast } from './index'
import { KitPage, kitEnabled } from './KitPage'

describe('Button', () => {
  it('is type=button by default and carries its variant', () => {
    render(<Button variant="attention" size="small">Answer</Button>)
    const b = screen.getByRole('button', { name: 'Answer' })
    expect(b).toHaveAttribute('type', 'button')
    expect(b).toHaveClass('sh-btn', 'sh-btn--warn', 'sh-btn--sm')
    // The old names stay as hooks for pages and tests not yet moved.
    expect(b).toHaveClass('ui-btn-attention', 'ui-btn-sm')
  })
  it('does not fire when disabled', async () => {
    const fn = vi.fn()
    render(<Button disabled onClick={fn}>Go</Button>)
    await userEvent.click(screen.getByRole('button'))
    expect(fn).not.toHaveBeenCalled()
  })
})

describe('Badge', () => {
  it('hides zero, caps large counts and names what it counts', () => {
    const { container, rerender } = render(<Badge count={0} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<Badge count={120} />)
    expect(screen.getByText('99+')).toBeInTheDocument()
    rerender(<Badge count={3} label="waiting on you" />)
    expect(screen.getByLabelText('3 waiting on you')).toBeInTheDocument()
  })
})

describe('Chip', () => {
  it('toggles as a pressed button and can be removed', async () => {
    const click = vi.fn()
    const remove = vi.fn()
    render(<><Chip kind="filter" selected onClick={click}>Open</Chip><Chip onRemove={remove}>topic</Chip></>)
    const f = screen.getByRole('button', { name: 'Open' })
    expect(f).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(f)
    expect(click).toHaveBeenCalled()
    await userEvent.click(screen.getByRole('button', { name: 'Remove' }))
    expect(remove).toHaveBeenCalled()
  })
})

describe('IdChip', () => {
  it('shows the kind and id, and links with href', () => {
    const { rerender } = render(<IdChip kind="decision" id={275} />)
    const chip = screen.getByText('decision 275')
    expect(chip.tagName).toBe('SPAN')
    expect(chip).toHaveClass('ui-idchip')
    expect(chip).toHaveAttribute('data-component', 'IdChip')
    expect(chip).toHaveAttribute('title', 'decision 275')
    rerender(<IdChip kind="item" id={82} href="#/inbox" />)
    const link = screen.getByRole('link', { name: 'item 82' })
    expect(link).toHaveAttribute('href', '#/inbox')
    expect(link).toHaveAttribute('title', 'item 82')
  })
  it('shows only the number bare, with the full label readable by assistive tech', () => {
    render(<IdChip kind="run" id={311} bare />)
    expect(screen.getByText('311').closest('[data-component="IdChip"]')).toHaveAttribute('title', 'run 311')
    expect(screen.getByText('run 311', { selector: '.visually-hidden' })).toBeInTheDocument()
  })
})

describe('Kbd', () => {
  it('renders one kbd per key', () => {
    render(<Kbd keys={['g', 'i']} />)
    expect(document.querySelectorAll('kbd')).toHaveLength(2)
  })
})

describe('Tabs', () => {
  function Host() {
    const [v, setV] = useState('a')
    return <Tabs label="T" value={v} onChange={setV} items={[
      { id: 'a', label: 'A', content: 'panel a' },
      { id: 'b', label: 'B', content: 'panel b' },
      { id: 'c', label: 'C', content: 'panel c' },
    ]} />
  }
  it('moves with arrow keys, wraps, and keeps one tab stop', async () => {
    const user = userEvent.setup()
    render(<Host />)
    expect(screen.getByRole('tab', { name: 'A' })).toHaveAttribute('tabindex', '0')
    expect(screen.getByRole('tab', { name: 'B' })).toHaveAttribute('tabindex', '-1')
    screen.getByRole('tab', { name: 'A' }).focus()
    await user.keyboard('{ArrowRight}')
    expect(screen.getByRole('tab', { name: 'B' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tabpanel')).toHaveTextContent('panel b')
    await user.keyboard('{End}{ArrowRight}')
    expect(screen.getByRole('tab', { name: 'A' })).toHaveFocus()
  })
})

describe('Dialog', () => {
  it('renders nothing closed, a titled dialog open, and Esc asks to close', () => {
    const onClose = vi.fn()
    const { rerender } = render(<Dialog open={false} onClose={onClose} title="Sure?">body</Dialog>)
    expect(document.querySelector('dialog')).toBeNull()
    rerender(<Dialog open onClose={onClose} title="Sure?">body</Dialog>)
    const d = document.querySelector('dialog') as HTMLDialogElement
    expect(d).toHaveAttribute('aria-labelledby')
    expect(screen.getByText('Sure?')).toBeInTheDocument()
    act(() => { d.dispatchEvent(new Event('cancel', { cancelable: true })) })
    expect(onClose).toHaveBeenCalled()
  })
})

describe('Popover', () => {
  it('opens from its trigger and closes on Esc, returning focus', async () => {
    const user = userEvent.setup()
    render(<Popover label="More">inside</Popover>)
    const t = screen.getByRole('button', { name: 'More' })
    await user.click(t)
    expect(t).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('inside')).toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(screen.queryByText('inside')).toBeNull()
    expect(t).toHaveFocus()
  })
})

describe('Toast', () => {
  it('announces in a polite region and clears itself', () => {
    vi.useFakeTimers()
    function Fire() {
      const toast = useToast()
      return <button onClick={() => toast('Answer sent', 'ok')}>fire</button>
    }
    render(<ToastProvider><Fire /></ToastProvider>)
    act(() => { screen.getByText('fire').click() })
    expect(screen.getByRole('status')).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByText('Answer sent')).toBeInTheDocument()
    act(() => { vi.advanceTimersByTime(TOAST_MS + 1) })
    expect(screen.queryByText('Answer sent')).toBeNull()
    vi.useRealTimers()
  })
})

describe('EmptyState and Skeleton', () => {
  it('shows title, body and action; the skeleton is one busy status', () => {
    render(<><EmptyState title="Nothing here" action={<button>Add</button>}>Try later.</EmptyState><Skeleton lines={3} /></>)
    expect(screen.getByText('Nothing here')).toBeInTheDocument()
    expect(screen.getByText('Try later.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Add' })).toBeInTheDocument()
    expect(screen.getByRole('status', { name: 'Loading' })).toHaveAttribute('aria-busy', 'true')
  })
})

describe('KitPage', () => {
  it('serves only #/kit and renders the gallery', () => {
    expect(kitEnabled('/kit')).toBe(import.meta.env.DEV)
    expect(kitEnabled('/other')).toBe(false)
    render(<KitPage />)
    expect(screen.getByRole('heading', { name: 'Kit' })).toBeInTheDocument()
  })
})

describe('ErrorState and LoadingRows', () => {
  it('says what failed, offers Retry and the status page', () => {
    const retry = vi.fn()
    render(<ErrorState what="Runs" error="The daemon answered 502." at="20:51" request="GET /v1/runs" onRetry={retry} />)
    expect(screen.getByRole('alert')).toHaveTextContent('Runs did not load: The daemon answered 502 at 20:51.')
    expect(screen.getByText('GET /v1/runs')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Open status' })).toHaveAttribute('href', '#/status')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(retry).toHaveBeenCalled()
  })

  it('shows no Retry when the page cannot ask again', () => {
    render(<ErrorState what="Runs" />)
    expect(screen.queryByRole('button', { name: 'Retry' })).toBeNull()
  })

  it('shows its rows only after the delay', () => {
    vi.useFakeTimers()
    render(<LoadingRows rows={2} />)
    expect(screen.queryByRole('status')).toBeNull()
    act(() => { vi.advanceTimersByTime(LOADING_DELAY_MS) })
    expect(screen.getByRole('status', { name: 'Loading' })).toBeInTheDocument()
    vi.useRealTimers()
  })
})

describe('ErrorState back link', () => {
  it('offers the way back beside Open status', () => {
    render(<ErrorState what="Project x" back={{ href: '#/projects', label: 'All projects' }} />)
    expect(screen.getByRole('link', { name: 'All projects' })).toHaveAttribute('href', '#/projects')
  })
})

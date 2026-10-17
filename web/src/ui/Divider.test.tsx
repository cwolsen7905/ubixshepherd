import { act, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useRef } from 'react'
import { parse } from '../router'
import { Shell } from '../shell/Shell'
import { useSideWidth } from '../shell/useSideWidth'
import { Divider, openFull } from './Divider'
import { clampSide, defaultSide, isSnap, readSide, sideLimits, writeSide } from './sideWidth'

const REM = 16
const CH = 8

describe('side width limits', () => {
  it('runs from 20rem to what leaves the main column 40 characters', () => {
    const l = sideLimits({ win: 1600, sidebar: 216, rem: REM, ch: CH })
    expect(l.min).toBe(20 * REM)
    expect(l.max).toBe(1600 - 216 - 40 * CH)
    // A window too narrow for both keeps the minimum.
    expect(sideLimits({ win: 700, sidebar: 216, rem: REM, ch: CH }).max).toBe(20 * REM)
  })
  it('defaults to 38% of the window between 20rem and 36rem', () => {
    expect(defaultSide(800, REM)).toBe(20 * REM)
    expect(defaultSide(1440, REM)).toBeCloseTo(0.38 * 1440)
    expect(defaultSide(2560, REM)).toBe(36 * REM)
  })
  it('clamps, and snaps past 60% of the window', () => {
    const l = { min: 320, max: 900 }
    expect([clampSide(100, l), clampSide(500, l), clampSide(5000, l)]).toEqual([320, 500, 900])
    expect(isSnap(0.6 * 1600, 1600)).toBe(false)
    expect(isSnap(0.6 * 1600 + 1, 1600)).toBe(true)
  })
  it('remembers a width per view and survives a blocked store', () => {
    const store = new Map<string, string>()
    const fake = { getItem: (k: string) => store.get(k) ?? null, setItem: (k: string, v: string) => void store.set(k, v), removeItem: (k: string) => void store.delete(k) }
    writeSide(fake, 'board', 480.4)
    expect(readSide(fake, 'board')).toBe(480)
    expect(readSide(fake, 'inbox')).toBeNull()
    writeSide(fake, 'board', null)
    expect(readSide(fake, 'board')).toBeNull()
    const broken = { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') }, removeItem: () => { throw new Error('blocked') } }
    expect(readSide(broken, 'board')).toBeNull()
    expect(() => writeSide(broken, 'board', 400)).not.toThrow()
  })
})

function setup(width = 500) {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1600 })
  const p = {
    onResize: vi.fn(),
    onCommit: vi.fn(),
    onReset: vi.fn(),
    onSwap: vi.fn(),
  }
  render(<Divider width={width} limits={() => ({ min: 320, max: 900 })} {...p} />)
  return { ...p, sep: screen.getByRole('separator', { name: 'Resize the side column' }) }
}

describe('Divider', () => {
  it('is a vertical separator with its limits and value, and a keyboard stop', () => {
    const { sep } = setup(500)
    expect(sep).toHaveAttribute('aria-orientation', 'vertical')
    expect(sep).toHaveAttribute('aria-valuenow', '500')
    expect(sep).toHaveAttribute('aria-valuemin', '320')
    expect(sep).toHaveAttribute('aria-valuemax', '900')
    expect(sep).toHaveAttribute('tabindex', '0')
    expect(sep).toHaveAttribute('data-component', 'Divider')
  })

  it('moves 16px an arrow key and 64px with Shift; left makes the panel wider', async () => {
    const user = userEvent.setup()
    const { sep, onResize, onCommit } = setup(500)
    sep.focus()
    await user.keyboard('{ArrowLeft}')
    expect(onResize).toHaveBeenLastCalledWith(516)
    expect(onCommit).toHaveBeenLastCalledWith(516)
    await user.keyboard('{ArrowRight}')
    expect(onResize).toHaveBeenLastCalledWith(484)
    await user.keyboard('{Shift>}{ArrowLeft}{/Shift}')
    expect(onResize).toHaveBeenLastCalledWith(564)
  })

  it('stops at the limits', async () => {
    const user = userEvent.setup()
    const a = setup(890)
    a.sep.focus()
    await user.keyboard('{Shift>}{ArrowLeft}{/Shift}')
    expect(a.onResize).toHaveBeenLastCalledWith(900)
  })

  it('resets on a double-click', () => {
    const { sep, onReset } = setup()
    fireEvent.doubleClick(sep)
    expect(onReset).toHaveBeenCalledTimes(1)
  })

  it('resizes while dragging, clamped, and remembers the width on release', () => {
    const { sep, onResize, onCommit, onSwap } = setup(500)
    fireEvent.pointerDown(sep, { button: 0, clientX: 1100 })
    fireEvent.pointerMove(window, { clientX: 1000 })
    expect(onResize).toHaveBeenLastCalledWith(600)
    fireEvent.pointerMove(window, { clientX: 1500 })
    expect(onResize).toHaveBeenLastCalledWith(320)
    fireEvent.pointerMove(window, { clientX: 800 })
    expect(onResize).toHaveBeenLastCalledWith(800)
    fireEvent.pointerUp(window)
    expect(onCommit).toHaveBeenCalledWith(800)
    expect(onSwap).not.toHaveBeenCalled()
  })

  it('past 60% of the window shows the wash and swaps on release, leaving the width alone', () => {
    const { sep, onResize, onCommit, onSwap } = setup(500)
    fireEvent.pointerDown(sep, { button: 0, clientX: 1100 })
    fireEvent.pointerMove(window, { clientX: 500 })
    expect(screen.getByText('Release to open as page')).toBeInTheDocument()
    expect(onResize).toHaveBeenLastCalledWith(500)
    fireEvent.pointerUp(window)
    expect(onSwap).toHaveBeenCalledTimes(1)
    expect(onCommit).not.toHaveBeenCalled()
    expect(screen.queryByText('Release to open as page')).toBeNull()
  })

  it('dragging back before release cancels the swap', () => {
    const { sep, onCommit, onSwap } = setup(500)
    fireEvent.pointerDown(sep, { button: 0, clientX: 1100 })
    fireEvent.pointerMove(window, { clientX: 500 })
    expect(screen.getByText('Release to open as page')).toBeInTheDocument()
    fireEvent.pointerMove(window, { clientX: 900 })
    expect(screen.queryByText('Release to open as page')).toBeNull()
    fireEvent.pointerUp(window)
    expect(onSwap).not.toHaveBeenCalled()
    expect(onCommit).toHaveBeenCalledWith(700)
  })
})

describe('useSideWidth', () => {
  function Probe({ view }: { view: string }) {
    const ref = useRef<HTMLElement>(null)
    const w = useSideWidth(view, ref)
    return (
      <>
        <output>{w.width}</output>
        <button onClick={() => w.commit(w.width + 40)}>widen</button>
        <button onClick={w.reset}>reset</button>
      </>
    )
  }
  beforeEach(() => {
    window.localStorage.clear()
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1600 })
  })

  it('remembers the width per view in localStorage and restores it', async () => {
    const user = userEvent.setup()
    const { rerender } = render(<Probe view="board" />)
    const base = Number(screen.getByRole('status').textContent)
    await user.click(screen.getByText('widen'))
    expect(Number(screen.getByRole('status').textContent)).toBe(base + 40)
    expect(window.localStorage.getItem('shepherd.sidewidth.board')).toBe(String(base + 40))
    rerender(<Probe view="inbox" />)
    expect(Number(screen.getByRole('status').textContent)).toBe(base)
    rerender(<Probe view="board" />)
    expect(Number(screen.getByRole('status').textContent)).toBe(base + 40)
  })

  it('reads a width remembered by an earlier visit, and forgets it on reset', async () => {
    const user = userEvent.setup()
    window.localStorage.setItem('shepherd.sidewidth.board', '640')
    render(<Probe view="board" />)
    expect(Number(screen.getByRole('status').textContent)).toBe(640)
    await user.click(screen.getByText('reset'))
    expect(window.localStorage.getItem('shepherd.sidewidth.board')).toBeNull()
    expect(Number(screen.getByRole('status').textContent)).not.toBe(640)
  })
})

describe('Shell divider', () => {
  const props = { waiting: null, workspaces: [], footer: null }
  function matchWidth(width: number) {
    window.matchMedia = ((q: string) => {
      const max = Number(/max-width:\s*(\d+)px/.exec(q)?.[1] ?? Infinity)
      return { matches: width <= max, media: q, addEventListener() {}, removeEventListener() {} }
    }) as unknown as typeof window.matchMedia
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  }
  afterEach(() => {
    // @ts-expect-error restore jsdom's lack of matchMedia
    delete window.matchMedia
  })

  it('shows beside a side panel, and not without one', () => {
    matchWidth(1600)
    const { rerender } = render(<Shell route={parse('#/board')} {...props}>page</Shell>)
    expect(screen.queryByRole('separator')).toBeNull()
    rerender(<Shell route={parse('#/board')} {...props} side={<p>panel</p>}>page</Shell>)
    expect(screen.getByRole('separator')).toBeInTheDocument()
    expect(screen.getByLabelText('Side panel')).toHaveTextContent('panel')
    rerender(<Shell route={parse('#/board')} {...props} sideBeside>page</Shell>)
    expect(screen.getByRole('separator')).toBeInTheDocument()
  })

  it('has none under 768px', () => {
    matchWidth(767)
    render(<Shell route={parse('#/board')} {...props} side={<p>panel</p>}>page</Shell>)
    expect(screen.queryByRole('separator')).toBeNull()
    matchWidth(390)
  })

  it('drives the shell width and remembers it per view', async () => {
    matchWidth(1600)
    window.localStorage.clear()
    const user = userEvent.setup()
    const { container, rerender } = render(<Shell route={parse('#/board')} {...props} side={<p>panel</p>}>page</Shell>)
    const shell = container.querySelector('.sh-shell') as HTMLElement
    const start = Number.parseFloat(shell.style.getPropertyValue('--sh-side-w'))
    screen.getByRole('separator').focus()
    await user.keyboard('{ArrowLeft}')
    expect(Number.parseFloat(shell.style.getPropertyValue('--sh-side-w'))).toBe(start + 16)
    expect(window.localStorage.getItem('shepherd.sidewidth.board')).toBe(String(start + 16))
    rerender(<Shell route={parse('#/runs')} {...props} side={<p>panel</p>}>page</Shell>)
    expect(Number.parseFloat(shell.style.getPropertyValue('--sh-side-w'))).toBe(start)
    rerender(<Shell route={parse('#/board')} {...props} side={<p>panel</p>}>page</Shell>)
    expect(Number.parseFloat(shell.style.getPropertyValue('--sh-side-w'))).toBe(start + 16)
    await user.dblClick(screen.getByRole('separator'))
    expect(Number.parseFloat(shell.style.getPropertyValue('--sh-side-w'))).toBe(start)
    expect(window.localStorage.getItem('shepherd.sidewidth.board')).toBeNull()
  })
})

describe('Divider on a full-width item', () => {
  afterEach(() => {
    window.location.hash = ''
  })

  it('reads full width from the address, encoded or not', () => {
    expect(openFull('#/artifacts?open=artifact:1/full')).toBe(true)
    expect(openFull('#/artifacts?open=artifact%3A1%2Ffull')).toBe(true)
    expect(openFull('#/artifacts?open=artifact:1')).toBe(false)
    expect(openFull('#/artifacts?open=lane:4/full&x=1')).toBe(true)
  })

  it('is absent when the item opens full width, and returns when it leaves it', async () => {
    window.location.hash = '#/artifacts?open=artifact:1'
    setup(500)
    expect(screen.getByRole('separator')).toBeInTheDocument()
    await act(async () => {
      window.location.hash = '#/artifacts?open=artifact%3A1%2Ffull'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(screen.queryByRole('separator')).toBeNull()
    await act(async () => {
      window.location.hash = '#/artifacts?open=artifact:1'
      window.dispatchEvent(new HashChangeEvent('hashchange'))
    })
    expect(screen.getByRole('separator')).toBeInTheDocument()
  })
})

import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { parse } from '../router'
import { Icon, ICON_NAMES, OfflineContext } from '../ui'
import { inboxCount } from '../state/attention'
import { NAV } from './nav'
import { KeepGoingIcon, railStatus } from './RailFoot'
import { Shell } from './Shell'
import { railName } from './Sidebar'
import { RAIL_KEY, readRail, writeRail } from './useRail'

const waiting: Waiting = {
  count: 2,
  counts: { decisions: 1, mrs: 0, ship: 0, next: 1, requests: 0 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 1, title: 'd' },
    { kind: 'next', action: 'review', next_id: 3, title: 'n' },
  ],
}

function setWidth(width: number) {
  window.matchMedia = ((q: string) => {
    const max = Number(/max-width:\s*(\d+)px/.exec(q)?.[1] ?? Infinity)
    return { matches: width <= max, media: q, addEventListener() {}, removeEventListener() {} }
  }) as unknown as typeof window.matchMedia
}

const keepOff = { on: false, spent_usd: 0, cap_usd: 0, runs_now: 0, max_runs: 0 }

function view({ w = waiting, broken = 0, running = 0, offline = false, unseen }: { w?: Waiting | null; broken?: number; running?: number; offline?: boolean; unseen?: Set<'board'> } = {}) {
  return render(
    <OfflineContext.Provider value={{ offline, retryIn: offline ? 3 : null }}>
      <Shell
        route={parse('#/board')}
        waiting={w}
        workspaces={[{ id: 1, name: 'acme', path: '/a', created: '', repos: 1, lanes: 1 }]}
        footer={<button type="button">help</button>}
        boardBroken={broken}
        unseen={unseen}
        status={{ counts: { running, waiting: 1, broken, routing: 0 }, lists: { running: [], waiting: [], broken: [], routing: [] } }}
      >
        <h1>Page</h1>
        <input aria-label="Draft" />
      </Shell>
    </OfflineContext.Provider>,
  )
}

const INBOX = `Inbox, ${inboxCount(waiting)} waiting`
const nav = () => screen.getByRole('navigation', { name: 'Views' })
const sidebar = () => screen.getByRole('complementary')

beforeEach(() => {
  window.localStorage.clear()
  setWidth(1440)
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response(JSON.stringify(keepOff), { status: 200 }))))
})

afterEach(() => {
  vi.unstubAllGlobals()
  // @ts-expect-error restore jsdom's lack of matchMedia
  delete window.matchMedia
})

describe('view icons', () => {
  it('draws one 16px stroked icon per view, hidden from assistive tech', () => {
    const items = NAV.flatMap((g) => g.items)
    expect(new Set(items.map((i) => i.icon)).size).toBe(items.length)
    for (const it of items) expect(ICON_NAMES).toContain(it.icon)
    const { container } = render(<Icon name="inbox" />)
    const svg = container.querySelector('svg') as SVGElement
    expect(svg).toHaveAttribute('viewBox', '0 0 16 16')
    expect(svg).toHaveAttribute('stroke', 'currentColor')
    expect(svg).toHaveAttribute('fill', 'none')
    expect(svg).toHaveAttribute('stroke-linecap', 'round')
    expect(svg).toHaveAttribute('stroke-linejoin', 'round')
    expect(svg).toHaveAttribute('aria-hidden', 'true')
  })

  it('shows each view icon beside its label in the open sidebar', () => {
    view()
    const inbox = within(nav()).getByRole('link', { name: /Inbox/ })
    expect(inbox.querySelector('[data-icon="inbox"]')).not.toBeNull()
  })
})

describe('the icon rail', () => {
  it('collapses by the chevron, remembers it, and opens again', async () => {
    const user = userEvent.setup()
    const { unmount } = view()
    expect(sidebar()).not.toHaveClass('is-rail')
    await user.click(screen.getByRole('button', { name: 'Collapse the sidebar to icons' }))
    expect(sidebar()).toHaveClass('is-rail')
    expect(window.localStorage.getItem(RAIL_KEY)).toBe('1')
    unmount()

    view()
    expect(sidebar()).toHaveClass('is-rail')
    const expand = screen.getByRole('button', { name: 'Expand the sidebar' })
    expect(expand).toHaveAttribute('aria-expanded', 'false')
    await user.click(expand)
    expect(sidebar()).not.toHaveClass('is-rail')
    expect(window.localStorage.getItem(RAIL_KEY)).toBeNull()
  })

  it('leaves the saved choice alone on \\ in the narrow layout, where the rail does not show', async () => {
    const user = userEvent.setup()
    setWidth(600)
    view()
    await user.keyboard('\\')
    expect(window.localStorage.getItem(RAIL_KEY)).toBeNull()
  })

  it('toggles on \\, but not while typing, and leaves [ to the stepping keys', async () => {
    const user = userEvent.setup()
    view()
    await user.keyboard('[[')
    expect(sidebar()).not.toHaveClass('is-rail')
    expect(window.localStorage.getItem(RAIL_KEY)).toBeNull()
    await user.keyboard('\\')
    expect(sidebar()).toHaveClass('is-rail')
    await user.click(screen.getByRole('textbox', { name: 'Draft' }))
    await user.keyboard('\\')
    expect(sidebar()).toHaveClass('is-rail')
    ;(document.activeElement as HTMLElement).blur()
    await user.keyboard('\\')
    expect(sidebar()).not.toHaveClass('is-rail')
  })

  it('shows each view as its icon, named by label and count', () => {
    writeRail(window.localStorage, true)
    view({ broken: 2 })
    const links = within(nav()).getAllByRole('link')
    expect(links).toHaveLength(NAV.flatMap((g) => g.items).length)
    for (const l of links) expect(l.textContent?.replace('✗', '')).toBe('')
    const inbox = within(nav()).getByRole('link', { name: INBOX })
    expect(inbox).toHaveAttribute('title', INBOX)
    expect(within(nav()).getByRole('link', { name: 'Next up, 1 waiting' })).toBeInTheDocument()
    expect(within(nav()).getByRole('link', { name: 'Board, 2 broken' })).toHaveAttribute('aria-current', 'page')
    expect(within(nav()).getByRole('link', { name: 'Settings' })).toHaveAttribute('title', 'Settings')
  })

  it('dots Inbox in lantern while something waits and Board with a red cross while broken', () => {
    writeRail(window.localStorage, true)
    view({ broken: 1, unseen: new Set(['board']) })
    const inbox = within(nav()).getByRole('link', { name: /^Inbox/ })
    const board = within(nav()).getByRole('link', { name: /^Board/ })
    expect(inbox.querySelector('[data-dot="waiting"]')).toHaveClass('sh-rail-dot', 'is-waiting')
    expect(board.querySelector('[data-dot="broken"]')).toHaveTextContent('✗')
    expect(board.querySelector('[data-dot="unseen"]')).not.toBeNull()
  })

  it('drops the dots at zero, but keeps the count in the name', () => {
    writeRail(window.localStorage, true)
    view({ w: null })
    const inbox = within(nav()).getByRole('link', { name: 'Inbox, 0 waiting' })
    expect(inbox.querySelector('[data-dot]')).toBeNull()
    expect(within(nav()).getByRole('link', { name: 'Board, 0 broken' }).querySelector('[data-dot]')).toBeNull()
  })

  it('makes search a magnifier button that opens the palette, and the mark the mark only', async () => {
    writeRail(window.localStorage, true)
    const open = vi.fn()
    render(
      <Shell route={parse('#/board')} waiting={null} workspaces={[{ id: 1, name: 'acme', path: '/a', created: '', repos: 1, lanes: 1 }]} footer={null} paletteOpen={open}>
        <h1>Page</h1>
      </Shell>,
    )
    const find = screen.getByRole('button', { name: 'Search or act' })
    expect(find.querySelector('[data-icon="search"]')).not.toBeNull()
    await userEvent.click(find)
    expect(open).toHaveBeenCalled()
    const mark = screen.getByRole('link', { name: /go to the board/ })
    expect(mark).not.toHaveTextContent('acme')
  })

  it('stays a full sheet below 760px whatever was remembered', () => {
    writeRail(window.localStorage, true)
    setWidth(390)
    view()
    expect(screen.getByRole('complementary', { hidden: true })).not.toHaveClass('is-rail')
    expect(screen.queryByRole('button', { name: /sidebar/, hidden: true })).toBeNull()
  })

  it('reaches every rail stop by Tab', async () => {
    writeRail(window.localStorage, true)
    view()
    const seen: string[] = []
    for (let i = 0; i < 4 + NAV.flatMap((g) => g.items).length; i++) {
      await userEvent.tab()
      seen.push(document.activeElement?.getAttribute('aria-label') ?? document.activeElement?.textContent ?? '')
    }
    expect(seen).toContain('Expand the sidebar')
    expect(seen).toContain('Search or act')
    expect(seen).toContain(INBOX)
    expect(seen).toContain('Status')
  })
})

describe('the collapsed footer', () => {
  it('names one status glyph by its words', () => {
    expect(railStatus({ offline: false, running: 0, broken: 0 })).toEqual({ glyph: '✓', tone: 'ok', words: 'Online' })
    expect(railStatus({ offline: false, running: 3, broken: 0 })).toEqual({ glyph: '●', tone: 'live', words: '3 working' })
    expect(railStatus({ offline: false, running: 3, broken: 1 })).toEqual({ glyph: '✗', tone: 'bad', words: '1 failing' })
    expect(railStatus({ offline: true, running: 3, broken: 0 })).toEqual({ glyph: '✗', tone: 'bad', words: 'Offline' })
  })

  it.each([
    [{}, 'Online', '✓'],
    [{ running: 2 }, '2 working', '●'],
    [{ broken: 1 }, '1 failing', '✗'],
    [{ offline: true }, 'Offline', '✗'],
  ])('shows %o as %s', (state, words, glyph) => {
    writeRail(window.localStorage, true)
    view(state)
    const btn = screen.getByRole('button', { name: `${words}, show the footer` })
    expect(btn).toHaveTextContent(glyph)
    expect(btn).toHaveAttribute('title', words)
  })

  it('opens the full footer in a flyout and closes it on Esc, back on the glyph', async () => {
    const user = userEvent.setup()
    writeRail(window.localStorage, true)
    view()
    const btn = screen.getByRole('button', { name: 'Online, show the footer' })
    expect(screen.queryByRole('button', { name: 'help' })).toBeNull()
    await user.click(btn)
    const pop = screen.getByRole('dialog', { name: 'Footer' })
    expect(within(pop).getByRole('button', { name: 'help' })).toBeInTheDocument()
    expect(await within(pop).findByRole('switch', { name: 'Keep going' })).toBeInTheDocument()
    expect(btn).toHaveAttribute('aria-expanded', 'true')
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Footer' })).toBeNull()
    expect(btn).toHaveFocus()
  })

  it('closes the flyout on a click elsewhere', async () => {
    writeRail(window.localStorage, true)
    view()
    await userEvent.click(screen.getByRole('button', { name: 'Online, show the footer' }))
    expect(screen.getByRole('dialog', { name: 'Footer' })).toBeInTheDocument()
    fireEvent.mouseDown(screen.getByRole('heading', { name: 'Page' }))
    expect(screen.queryByRole('dialog', { name: 'Footer' })).toBeNull()
  })

  it('reads Keep going once on mount with its defaults, then waits for the interval', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(JSON.stringify(keepOff), { status: 200 })))
    vi.stubGlobal('fetch', fetchMock)
    render(<KeepGoingIcon />)
    await screen.findByRole('switch', { name: 'Keep going' })
    // Let any further renders and effects settle: a restarting effect would read again each time.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50))
    })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('keeps Keep going as a small switch, lit while on', async () => {
    const set = vi.fn(() => Promise.resolve({ ...keepOff, on: true }))
    render(<KeepGoingIcon load={() => Promise.resolve(keepOff)} set={set} />)
    const sw = await screen.findByRole('switch', { name: 'Keep going' })
    expect(sw).toHaveAttribute('aria-checked', 'false')
    expect(sw).not.toHaveClass('is-on')
    await act(async () => sw.click())
    expect(set).toHaveBeenCalledWith(true)
    await waitFor(() => expect(sw).toHaveClass('is-on'))
    expect(sw).toHaveAttribute('aria-checked', 'true')
  })
})

describe('railName and storage', () => {
  it('names counted views with their count and others by label', () => {
    expect(railName({ key: 'inbox', label: 'Inbox' }, 3)).toBe('Inbox, 3 waiting')
    expect(railName({ key: 'board', label: 'Board' }, 0)).toBe('Board, 0 broken')
    expect(railName({ key: 'files', label: 'Files' }, 0)).toBe('Files')
  })

  it('reads a missing or refused store as open', () => {
    expect(readRail(null)).toBe(false)
    writeRail(window.localStorage, true)
    expect(readRail(window.localStorage)).toBe(true)
  })
})

describe('the stub entries', () => {
  it('lists Classifiers and Guardrails in Setup, each reaching its own page', () => {
    const setup = NAV.find((g) => g.name === 'Setup')?.items ?? []
    const labels = setup.map((i) => i.label)
    expect(labels).toContain('Classifiers')
    expect(labels).toContain('Guardrails')
    for (const key of ['classifiers', 'guardrails'] as const) {
      const it = setup.find((i) => i.key === key)
      expect(it?.icon).toBe(key)
      const r = parse(it?.to() ?? '')
      expect(r).toEqual({ page: 'rules', view: key })
      expect(NAV.flatMap((g) => g.items).filter((o) => o.current(r)).map((o) => o.key)).toEqual([key])
    }
  })
})

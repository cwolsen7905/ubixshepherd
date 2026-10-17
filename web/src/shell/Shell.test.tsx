import { act, render, renderHook, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { faviconHref } from '../favicon'
import { NAV } from './nav'
import { parse } from '../router'
import { Shell } from './Shell'
import { waitingKey } from '../model/waitingStep'
import { seenStore } from '../state/seen'
import { useAttentionChrome } from './useAttentionChrome'

const waiting: Waiting = {
  count: 4,
  counts: { decisions: 1, mrs: 1, ship: 0, next: 1, requests: 1 },
  items: [
    { kind: 'decision', action: 'answer', decision_id: 1, title: 'd' },
    { kind: 'request', action: 'route', request_id: 2, title: 'r' },
    { kind: 'next', action: 'review', next_id: 3, title: 'n' },
    { kind: 'mr', action: 'approve', mr: 9, title: 'm' },
  ],
}

/** matchMedia answering for a window `width` wide. */
function setWidth(width: number) {
  window.matchMedia = ((q: string) => {
    const max = Number(/max-width:\s*(\d+)px/.exec(q)?.[1] ?? Infinity)
    return { matches: width <= max, media: q, addEventListener() {}, removeEventListener() {} }
  }) as unknown as typeof window.matchMedia
}

function view(w: Waiting | null = waiting, hash = '#/', lanes?: { id: number; name: string }[]) {
  return render(
    <Shell
      route={parse(hash)}
      lanes={lanes}
      waiting={w}
      workspaces={[{ id: 1, name: 'acme', path: '/a', created: '', repos: 1, lanes: 1 }]}
      footer={<button type="button">help</button>}
    >
      <h1>Page</h1>
    </Shell>,
  )
}

afterEach(() => {
  // @ts-expect-error restore jsdom's lack of matchMedia
  delete window.matchMedia
})

describe('Shell', () => {
  it('lists Inbox before Conversation and dots Conversation while a waiting item is unseen', () => {
    setWidth(1440)
    seenStore.mark([])
    view()
    const nav = screen.getByRole('navigation', { name: 'Views' })
    const links = within(nav).getAllByRole('link')
    expect(links[0]).toHaveTextContent('Inbox')
    expect(links[1]).toHaveTextContent('Conversation')
    expect(within(links[1] as HTMLElement).getByTitle('New since you last looked')).toBeInTheDocument()
    expect(within(links[0] as HTMLElement).queryByTitle('New since you last looked')).toBeNull()
  })

  it('drops the Conversation dot once every waiting item has been seen', () => {
    setWidth(1440)
    act(() => seenStore.mark(waiting.items.map(waitingKey)))
    view()
    const nav = screen.getByRole('navigation', { name: 'Views' })
    expect(within(nav).queryByTitle('New since you last looked')).toBeNull()
  })

  it('puts the Keep going switch in the sidebar footer', async () => {
    setWidth(1440)
    vi.stubGlobal('fetch', vi.fn(() =>
      Promise.resolve(new Response(JSON.stringify({ on: false, spent_usd: 0, cap_usd: 5, runs_now: 0, max_runs: 3 }), { status: 200 })),
    ))
    view()
    expect(await screen.findByRole('switch', { name: 'Keep going' })).toBeInTheDocument()
    vi.unstubAllGlobals()
  })

  it('takes the nav counts from the attention list', () => {
    setWidth(1440)
    view()
    const nav = screen.getByRole('navigation', { name: 'Views' })
    expect(within(nav).getByRole('link', { name: /Inbox/ })).toHaveTextContent('3')
    expect(within(nav).getByRole('link', { name: /Next up/ })).toHaveTextContent('1')
    expect(within(nav).getByRole('link', { name: /Board/ })).not.toHaveTextContent(/\d/)
  })

  it('shows no counts before the list loads', () => {
    setWidth(1440)
    view(null)
    expect(within(screen.getByRole('navigation', { name: 'Views' })).getByRole('link', { name: /Inbox/ })).not.toHaveTextContent(/\d/)
  })

  it('marks the current page', () => {
    setWidth(1440)
    view(waiting, '#/runs')
    expect(screen.getByRole('link', { name: 'Runs' })).toHaveAttribute('aria-current', 'page')
  })

  it('is full from 760px up and a sheet below it', () => {
    setWidth(1440)
    const { container, unmount } = view()
    expect(container.querySelector('.sh-shell')).toHaveAttribute('data-mode', 'full')
    expect(container.querySelector('.sh-shell')).toHaveAttribute('data-component', 'AppShell')
    unmount()
    setWidth(1000)
    const b = view()
    expect(b.container.querySelector('.sh-shell')).toHaveAttribute('data-mode', 'full')
    expect(screen.queryByRole('button', { name: 'Menu' })).toBeNull()
    b.unmount()
    setWidth(700)
    const c = view()
    expect(c.container.querySelector('.sh-shell')).toHaveAttribute('data-mode', 'sheet')
    expect(screen.getByRole('button', { name: 'Menu' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('keeps the sheet closed until asked, then closes it on Escape and returns focus', async () => {
    setWidth(700)
    view()
    expect(screen.queryByRole('navigation', { name: 'Views' })).toBeNull()
    const menu = screen.getByRole('button', { name: 'Menu' })
    await userEvent.click(menu)
    expect(screen.getByRole('navigation', { name: 'Views' })).toBeVisible()
    expect(menu).toHaveAttribute('aria-expanded', 'true')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('navigation', { name: 'Views' })).toBeNull()
    expect(menu).toHaveFocus()
  })

  it('has the landmarks and tabs sidebar, then the page head, then the page', async () => {
    setWidth(1440)
    view()
    expect(screen.getByRole('navigation', { name: 'Views' })).toBeInTheDocument()
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('complementary')).toBeInTheDocument()
    // The skip link first, then the views, then the footer, then the page head.
    const labels = NAV.flatMap((g) => g.items.map((i) => i.label))
    const stops = labels.length
    const seen: string[] = []
    for (let i = 0; i < stops + 4; i++) {
      await userEvent.tab()
      seen.push(document.activeElement?.textContent?.trim() ?? '')
    }
    const at = (re: RegExp) => seen.findIndex((t) => re.test(t))
    expect(at(/Skip to the page/)).toBe(0)
    expect(at(/Inbox/)).toBeLessThan(at(/^help$/))
    // Every view is a stop, in the nav's order, before the footer.
    const order = labels.map((l) => at(new RegExp(`^${l}\\d*$`)))
    expect(order.every((n) => n > 0)).toBe(true)
    expect(order).toEqual([...order].sort((a, b) => a - b))
    for (const l of ['Files', 'History', 'Rules', 'Status']) expect(seen.includes(l)).toBe(true)
    expect(Math.max(...order)).toBeLessThan(at(/^help$/))
  })

  it('has no page bar from 760px up, so the page head is the one title', () => {
    for (const w of [1440, 1000, 760]) {
      setWidth(w)
      const { container, unmount } = view(waiting, '#/board')
      expect(container.querySelector('.sh-mobile-bar')).toBeNull()
      expect(screen.queryByRole('banner')).toBeNull()
      unmount()
    }
  })

  it('shows the menu, the title and the waiting count in a bar below 760px', () => {
    setWidth(390)
    view(waiting, '#/runs')
    const bar = screen.getByRole('banner')
    expect(within(bar).getByRole('button', { name: 'Menu' })).toBeInTheDocument()
    expect(within(bar).getByText('Runs')).toHaveClass('sh-mobile-title')
    const count = within(bar).getByRole('link', { name: '3 waiting on you' })
    expect(count).toHaveAttribute('href', '#/inbox')
  })

  it('gives the 390px status count sheet its sheet class', async () => {
    setWidth(390)
    const lists = { running: [], waiting: [], broken: [], routing: [] }
    render(
      <Shell route={parse('#/')} waiting={waiting} workspaces={[]} footer={null} status={{ counts: { running: 0, waiting: 2, broken: 0, routing: 0 }, lists }}>
        <h1>Page</h1>
      </Shell>,
    )
    await userEvent.click(screen.getByRole('link', { name: '2 waiting on you' }))
    const card = document.querySelector('[data-component="CountList"]')
    expect(card?.closest('.sh-count-sheet')).not.toBeNull()
  })

  it('leaves the count out of the bar when nothing waits', () => {
    setWidth(390)
    view(null, '#/runs')
    expect(within(screen.getByRole('banner')).queryByRole('link')).toBeNull()
  })

  it('titles the bar by the route: the lane page shows its lane, not Board', () => {
    setWidth(390)
    view(waiting, '#/lanes/7', [{ id: 7, name: 'feat/export-csv' }])
    expect(screen.getByRole('banner')).toHaveTextContent('feat/export-csv')
    expect(screen.getByRole('banner')).not.toHaveTextContent('Board')
  })

  it('moves focus to the page on the skip link', async () => {
    setWidth(1440)
    view()
    await userEvent.tab()
    await userEvent.keyboard('{Enter}')
    expect(screen.getByRole('main')).toHaveFocus()
  })
})

describe('useAttentionChrome', () => {
  it('sets the title and the icon from the attention list', () => {
    document.head.innerHTML = '<link rel="icon" href="/x.svg">'
    const { rerender } = renderHook(({ w }) => useAttentionChrome(w), { initialProps: { w: waiting as Waiting | null } })
    expect(document.title).toBe('3 waiting on you · Shepherd')
    expect(document.querySelector('link[rel="icon"]')).toHaveAttribute('href', faviconHref(true))
    act(() => rerender({ w: { ...waiting, count: 0, items: [] } }))
    expect(document.title).toBe('Shepherd')
    expect(document.querySelector('link[rel="icon"]')).toHaveAttribute('href', faviconHref(false))
  })
})

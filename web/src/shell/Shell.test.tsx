import { act, render, renderHook, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { Waiting } from '../api/types'
import { faviconHref } from '../favicon'
import { parse } from '../router'
import { Shell } from './Shell'
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

function view(w: Waiting | null = waiting, hash = '#/') {
  return render(
    <Shell
      route={parse(hash)}
      waiting={w}
      workspaces={[{ id: 1, name: 'acme', path: '/a', created: '', repos: 1, lanes: 1 }]}
      footer={<button type="button">help</button>}
      headActions={<button type="button">waiting</button>}
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
  it('takes the nav counts from the attention list', () => {
    setWidth(1440)
    view()
    const nav = screen.getByRole('navigation', { name: 'Views' })
    expect(within(nav).getByRole('link', { name: /Decisions/ })).toHaveTextContent('2')
    expect(within(nav).getByRole('link', { name: /Next up/ })).toHaveTextContent('1')
    expect(within(nav).getByRole('link', { name: /Board/ })).not.toHaveTextContent(/\d/)
  })

  it('shows no counts before the list loads', () => {
    setWidth(1440)
    view(null)
    expect(within(screen.getByRole('navigation', { name: 'Views' })).getByRole('link', { name: /Decisions/ })).not.toHaveTextContent(/\d/)
  })

  it('marks the current page', () => {
    setWidth(1440)
    view(waiting, '#/runs')
    expect(screen.getByRole('link', { name: 'Runs' })).toHaveAttribute('aria-current', 'page')
  })

  it('is full above 1280, icons below it and a sheet below 760', () => {
    setWidth(1440)
    const { container, unmount } = view()
    expect(container.querySelector('.shell')).toHaveAttribute('data-mode', 'full')
    unmount()
    setWidth(1200)
    const b = view()
    expect(b.container.querySelector('.shell')).toHaveAttribute('data-mode', 'icons')
    expect(screen.queryByRole('button', { name: 'Menu' })).toBeNull()
    b.unmount()
    setWidth(700)
    const c = view()
    expect(c.container.querySelector('.shell')).toHaveAttribute('data-mode', 'sheet')
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
    expect(screen.getByRole('banner')).toBeInTheDocument()
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('complementary')).toBeInTheDocument()
    // The skip link first, then the views, then the footer, then the page head.
    const seen: string[] = []
    for (let i = 0; i < 14; i++) {
      await userEvent.tab()
      seen.push(document.activeElement?.textContent?.trim() ?? '')
    }
    const at = (re: RegExp) => seen.findIndex((t) => re.test(t))
    expect(at(/Skip to the page/)).toBe(0)
    expect(at(/Decisions/)).toBeLessThan(at(/^help$/))
    expect(at(/^help$/)).toBeLessThan(at(/^waiting$/))
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
    expect(document.title).toBe('4 waiting on you · Shepherd')
    expect(document.querySelector('link[rel="icon"]')).toHaveAttribute('href', faviconHref(true))
    act(() => rerender({ w: { ...waiting, count: 0, items: [] } }))
    expect(document.title).toBe('Shepherd')
    expect(document.querySelector('link[rel="icon"]')).toHaveAttribute('href', faviconHref(false))
  })
})

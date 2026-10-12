import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BackLink, routeName } from './BackLink'
import { href, parse } from '../router'

async function visit(hash: string) {
  window.location.hash = hash
  await waitFor(() => expect(window.location.hash).toBe(hash))
  await new Promise((r) => setTimeout(r, 0))
}

describe('BackLink', () => {
  it('names the board when there is nowhere else to go back to', () => {
    render(<BackLink />)
    const a = screen.getByRole('link', { name: '← Board' })
    expect(a).toHaveAttribute('href', '#/')
  })

  it('goes back to the page it came from, with its filters and scroll, labelled for it', async () => {
    const scrollTo = vi.fn()
    vi.stubGlobal('scrollTo', scrollTo)
    vi.stubGlobal('requestAnimationFrame', (f: () => void) => { f(); return 0 })
    await visit('#/timeline?range=week')
    window.scrollY = 0
    await visit('#/runs/5/log')
    render(<BackLink />)
    const a = screen.getByRole('link', { name: '← Timeline' })
    expect(a).toHaveAttribute('href', '#/timeline?range=week')
    await userEvent.click(a)
    expect(window.location.hash).toBe('#/timeline?range=week')
    expect(scrollTo).toHaveBeenCalledWith(0, 0)
    vi.unstubAllGlobals()
  })
})

describe('routeName', () => {
  it('labels every page', () => {
    expect(routeName(parse(href.board('week')))).toBe('Board')
    expect(routeName(parse(href.lane(4)))).toBe('Lane 4')
    expect(routeName(parse(href.log(9)))).toBe('Run 9')
    expect(routeName(parse(href.decisions(2)))).toBe('Decisions')
    expect(routeName(parse(href.project('shop')))).toBe('shop')
  })
})

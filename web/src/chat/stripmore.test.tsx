import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { WaitingItem } from '../api/types'
import { PendingStrip } from './strip'

const items: WaitingItem[] = [1, 2, 3, 4, 5].map((id) => ({ kind: 'decision', action: 'answer', decision_id: id, title: `item ${id}` }))

vi.mock('../state/context', () => ({ useSnapshot: () => ({ waiting: { items } }) }))

describe('PendingStrip overflow', () => {
  afterEach(() => vi.restoreAllMocks())

  it('shows the items that fit, then +N for the rest, which open from its menu', async () => {
    const user = userEvent.setup()
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(300)
    window.location.hash = '#/chat'
    render(<PendingStrip />)
    expect(screen.getByRole('navigation', { name: 'Waiting on you' })).toHaveAttribute('data-component', 'PendingBar')
    expect(screen.getByText('5 waiting on you')).toBeInTheDocument()
    const more = await screen.findByRole('button', { name: /^\+\d+$/ })
    // 298 px less room for +N leaves two 100 px chips; three overflow.
    expect(more).toHaveTextContent('+3')
    const over = document.querySelectorAll('.sh-pending-item.is-over')
    expect(over).toHaveLength(3)
    for (const c of over) expect(c).toHaveAttribute('tabindex', '-1')

    await user.click(more)
    await user.click(within(screen.getByRole('group', { name: 'More waiting items' })).getByRole('button', { name: /item 5/ }))
    expect(decodeURIComponent(window.location.hash)).toContain('open=decision:5')
    expect(screen.queryByRole('group', { name: 'More waiting items' })).toBeNull()
  })

  it('closes the menu on Escape', async () => {
    const user = userEvent.setup()
    vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(100)
    vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(300)
    render(<PendingStrip />)
    await user.click(await screen.findByRole('button', { name: /^\+\d+$/ }))
    expect(screen.getByRole('group', { name: 'More waiting items' })).toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('group', { name: 'More waiting items' })).toBeNull()
  })
})

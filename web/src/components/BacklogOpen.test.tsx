import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { itemOpen, openClick, openHref, openRow } from './BacklogOpen'

beforeEach(() => {
  window.location.hash = '#/board'
})
afterEach(cleanup)

describe('itemOpen', () => {
  it('opens a decision by its pane, else the lane, else nothing', () => {
    expect(itemOpen({ decision: { id: 4 }, laneId: 2 })).toEqual({ kind: 'decision', id: 4 })
    expect(itemOpen({ laneId: 2 })).toEqual({ kind: 'lane', id: 2 })
    expect(itemOpen({})).toBeNull()
  })
})

describe('a link row', () => {
  const o = { kind: 'lane', id: 2 }
  it('sets open= on click, and leaves a modified click to the browser', async () => {
    render(<a href={openHref(o)} onClick={openClick(o)}>row</a>)
    expect(fireEvent.click(screen.getByRole('link'), { ctrlKey: true })).toBe(true) // not prevented
    await userEvent.click(screen.getByRole('link'))
    expect(window.location.hash).toBe('#/board?open=lane%3A2')
  })
})

describe('a table row', () => {
  const o = { kind: 'run', id: 9 }
  it('is focusable, opens on Enter and click, and marks itself when open', async () => {
    const { rerender } = render(<table><tbody><tr {...openRow(o)}><td>row</td></tr></tbody></table>)
    const tr = screen.getByRole('row')
    expect(tr).toHaveAttribute('tabindex', '0')
    expect(tr).not.toHaveAttribute('aria-current')
    tr.focus()
    await userEvent.keyboard('{Enter}')
    expect(window.location.hash).toBe('#/board?open=run%3A9')
    rerender(<table><tbody><tr {...openRow(o)}><td>row</td></tr></tbody></table>)
    expect(screen.getByRole('row')).toHaveAttribute('aria-current', 'true')
  })
  it('leaves a link inside it to its own click', async () => {
    render(<table><tbody><tr {...openRow(o)}><td><a href="#/log/9">log</a></td></tr></tbody></table>)
    await userEvent.click(screen.getByRole('link'))
    expect(window.location.hash).toBe('#/log/9')
  })
})

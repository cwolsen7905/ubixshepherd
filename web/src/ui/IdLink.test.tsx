import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { refocus } from '../router'
import { IdLink } from './IdLink'

vi.mock('../router', async (orig) => ({ ...(await orig<typeof import('../router')>()), refocus: vi.fn() }))

it('opens its own thing without selecting the row it sits in', () => {
  window.location.hash = '#/inbox'
  const select = vi.fn()
  render(
    <div role="button" tabIndex={0} onClick={select}>
      <IdLink to={{ kind: 'question', id: 196, text: 'Q-196' }} />
    </div>,
  )
  fireEvent.click(screen.getByText('Q-196'))
  expect(select).not.toHaveBeenCalled()
  expect(window.location.hash).toBe('#/inbox?open=question%3A196')
})

it('is not an anchor inside another anchor', () => {
  const { container } = render(
    <a href="#/project/x">
      <IdLink to={{ kind: 'decision', id: 370, text: 'D-370' }} />
    </a>,
  )
  expect(container.querySelectorAll('a a')).toHaveLength(0)
  expect(screen.getByText('D-370').tagName).toBe('SPAN')
})

it('is not an anchor inside a button', () => {
  const { container } = render(
    <button type="button">
      <IdLink to={{ kind: 'run', id: 12, text: 'R-12' }} />
    </button>,
  )
  expect(container.querySelectorAll('button a')).toHaveLength(0)
  expect(screen.getByText('R-12').tagName).toBe('SPAN')
})

it('refocuses the open thing when its own ID is clicked again', () => {
  window.location.hash = '#/inbox?open=question%3A196'
  render(<IdLink to={{ kind: 'question', id: 196, text: 'Q-196' }} />)
  fireEvent.click(screen.getByText('Q-196'))
  expect(refocus).toHaveBeenCalledTimes(1)
  expect(window.location.hash).toBe('#/inbox?open=question%3A196')
})

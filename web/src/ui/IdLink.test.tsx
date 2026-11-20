import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { IdLink } from './IdLink'

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

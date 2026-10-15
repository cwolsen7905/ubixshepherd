import { render, screen } from '@testing-library/react'
import { StatusCounts } from './StatusCounts'

describe('StatusCounts', () => {
  it('shows all four, zeros muted, with names a screen reader can read', () => {
    render(<StatusCounts counts={{ running: 2, waiting: 0, broken: 1, routing: 0 }} />)
    expect(screen.getByRole('status', { name: 'Status counts' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '2 running' })).toHaveAttribute('href', '#/board')
    expect(screen.getByRole('button', { name: '0 waiting on you' })).toHaveClass('status-zero')
    expect(screen.getByRole('link', { name: '1 broken or blocked' })).not.toHaveClass('status-zero')
    expect(screen.getByRole('link', { name: '0 needing routing' })).toHaveAttribute('href', '#/decisions')
  })

  it('keeps the words in the markup for the narrow layout to hide, and the glyphs from readers', () => {
    const { container } = render(<StatusCounts counts={{ running: 1, waiting: 1, broken: 1, routing: 1 }} />)
    expect(container.querySelectorAll('.status-word')).toHaveLength(4)
    expect(container.querySelectorAll('.status-glyph[aria-hidden="true"]')).toHaveLength(4)
  })

  it('opens the waiting panel through its toggle', async () => {
    const toggle = vi.fn()
    const b = document.createElement('button')
    b.className = 'waiting-toggle'
    b.addEventListener('click', toggle)
    document.body.append(b)
    render(<StatusCounts counts={{ running: 0, waiting: 3, broken: 0, routing: 0 }} />)
    screen.getByRole('button', { name: '3 waiting on you' }).click()
    expect(toggle).toHaveBeenCalled()
    b.remove()
  })
})

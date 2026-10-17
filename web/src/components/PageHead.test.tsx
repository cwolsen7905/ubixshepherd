import { render, screen } from '@testing-library/react'
import { PageHead } from './PageHead'

describe('PageHead', () => {
  it('renders one h1 with chips and hints in their slots', () => {
    render(<PageHead title="Board" chips={<span>2 waiting</span>} hints={<kbd>?</kbd>} sub="all repos" />)
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByRole('heading', { name: 'Board' })).toBeInTheDocument()
    expect(screen.getByText('2 waiting').parentElement).toHaveClass('page-chips')
    expect(screen.getByText('?').parentElement).toHaveClass('page-actions')
    expect(screen.getByText('all repos')).toHaveClass('page-sub')
  })

  it('leaves out empty slots', () => {
    const { container } = render(<PageHead title="Runs" className="runs-head" />)
    expect(container.querySelector('.page-chips, .page-actions, .page-sub')).toBeNull()
    expect(container.firstElementChild).toHaveClass('page-head', 'runs-head')
  })
})

import { fireEvent, render, screen } from '@testing-library/react'
import { ArtifactMarkdown } from './ArtifactMarkdown'

describe('ArtifactMarkdown', () => {
  it('forgets a failed image when the file shown changes', () => {
    const { rerender } = render(<ArtifactMarkdown text="![a](x.png)" laneId={1} path="docs/a.md" />)
    fireEvent.error(screen.getByRole('img', { name: 'a' }))
    expect(screen.getByText(/could not be loaded/)).toBeInTheDocument()
    rerender(<ArtifactMarkdown text="![b](y.png)" laneId={1} path="docs/b.md" />)
    expect(screen.getByRole('img', { name: 'b' })).toBeInTheDocument()
  })
})

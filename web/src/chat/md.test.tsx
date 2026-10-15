import { render, screen } from '@testing-library/react'
import { Markdown } from './md'

describe('chat Markdown', () => {
  it('renders a fenced block with a Copy code button, and keeps the chat class names', () => {
    const { container } = render(<Markdown text={'Run this:\n\n```sh\nshepherd status\n```\n\n- one\n'} />)
    expect(screen.getAllByRole('button', { name: 'Copy code' })).toHaveLength(1)
    expect(container.querySelector('.md .cv-code pre code')?.textContent).toBe('shepherd status')
    expect(container.querySelector('.md ul li')?.textContent).toBe('one')
  })
})

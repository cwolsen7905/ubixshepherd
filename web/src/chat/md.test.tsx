import { render, screen } from '@testing-library/react'
import { Markdown } from './md'

describe('chat Markdown', () => {
  it('renders a fenced block with a Copy code button, and keeps the chat class names', () => {
    const { container } = render(<Markdown text={'Run this:\n\n```sh\nshepherd status\n```\n\n- one\n'} />)
    expect(screen.getAllByRole('button', { name: 'Copy code' })).toHaveLength(1)
    expect(container.querySelector('.md .cv-code pre code')?.textContent).toBe('shepherd status')
    expect(container.querySelector('.md ul li')?.textContent).toBe('one')
  })

  it('treats a lone backtick as text, so the rest of the message is not code', () => {
    const { container } = render(<Markdown text={'It said (`kelpie:triage` on the thre…; then `kelpie:respond` and more, to `be sure'} />)
    expect(container.querySelectorAll('code')).toHaveLength(0)
    expect(container.textContent).toContain('and more, to')
    const ok = render(<Markdown text={'Run `make check` then `make build`, a lone ` here'} />)
    expect(ok.container.querySelectorAll('code')).toHaveLength(0)
    const fine = render(<Markdown text={'Run `make check` then `make build`.\n\n```\na `b\n```'} />)
    expect([...fine.container.querySelectorAll('code')].map((c) => c.textContent)).toEqual(['make check', 'make build', 'a `b'])
  })
})

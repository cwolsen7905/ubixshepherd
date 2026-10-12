import { render, screen } from '@testing-library/react'
import { GateBadge, gateTitle } from './GateBadge'

describe('GateBadge', () => {
  it('says each state in words, with the commit, time and command on hover', () => {
    render(<GateBadge gate={{ state: 'passed', sha: '0123456789abcdef', at: '2026-10-11T10:00:00Z', command: 'make check', source: 'ship' }} />)
    const el = screen.getByText('gate passed')
    expect(el).toHaveClass('tone-ok')
    expect(el.title).toContain('commit 01234567')
    expect(el.title).toContain('2026-10-11T10:00:00Z')
    expect(el.title).toContain('command: make check')
  })

  it('tones failed and stale, and keeps not run neutral', () => {
    const { rerender } = render(<GateBadge gate={{ state: 'failed' }} />)
    expect(screen.getByText('gate failed')).toHaveClass('tone-bad')
    rerender(<GateBadge gate={{ state: 'stale', sha: 'abcdef12' }} />)
    expect(screen.getByText('gate stale')).toHaveClass('tone-warn')
    rerender(<GateBadge gate={{ state: 'not_run' }} />)
    expect(screen.getByText('gate not run')).toHaveClass('tone-muted')
  })

  it('draws nothing without a record', () => {
    const { container } = render(<GateBadge gate={undefined} />)
    expect(container).toBeEmptyDOMElement()
    expect(gateTitle({ state: 'not_run' })).toContain('has not run')
  })
})

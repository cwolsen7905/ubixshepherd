import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { ErrorBoundary } from '../components/ErrorBoundary'

let broken = true
function Bomb() {
  if (broken) throw new Error('boom')
  return <p>ok</p>
}

beforeEach(() => {
  broken = true
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.spyOn(console, 'log').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

describe('ChatThread error boundary', () => {
  it('shows the boundary fallback when a child throws, rather than blanking the page', () => {
    render(
      <ErrorBoundary region="The conversation">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByRole('alert').textContent).toContain('The conversation could not be shown')
    expect(screen.getByRole('alert').textContent).toContain('boom')
  })

  it('recovers when the child stops throwing', () => {
    const { rerender } = render(
      <ErrorBoundary region="The conversation" resetKey="a">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByRole('alert').textContent).toContain('The conversation could not be shown')
    broken = false
    rerender(
      <ErrorBoundary region="The conversation" resetKey="b">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByText('ok')).toBeTruthy()
  })
})

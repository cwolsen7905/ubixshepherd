import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { ErrorBoundary } from './ErrorBoundary'

let broken = true
function Bomb() {
  if (broken) throw new Error('items is undefined')
  return <p>fine</p>
}

beforeEach(() => {
  broken = true
  vi.spyOn(console, 'error').mockImplementation(() => {})
})
afterEach(() => vi.restoreAllMocks())

describe('ErrorBoundary', () => {
  it('shows a panel for the failed region and leaves its siblings', () => {
    render(
      <>
        <p>sidebar</p>
        <ErrorBoundary region="The page">
          <Bomb />
        </ErrorBoundary>
      </>,
    )
    expect(screen.getByText('sidebar')).toBeTruthy()
    expect(screen.getByRole('alert').textContent).toContain('The page could not be shown')
    expect(screen.getByRole('alert').textContent).toContain('items is undefined')
  })

  it('retries: remounts the children', () => {
    render(
      <ErrorBoundary region="The page">
        <Bomb />
      </ErrorBoundary>,
    )
    broken = false
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(screen.getByText('fine')).toBeTruthy()
  })

  it('clears when resetKey changes', () => {
    const { rerender } = render(
      <ErrorBoundary region="The page" resetKey="a">
        <Bomb />
      </ErrorBoundary>,
    )
    broken = false
    rerender(
      <ErrorBoundary region="The page" resetKey="b">
        <Bomb />
      </ErrorBoundary>,
    )
    expect(screen.getByText('fine')).toBeTruthy()
  })
})

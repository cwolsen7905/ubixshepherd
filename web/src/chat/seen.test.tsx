import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import type { WaitingItem } from '../api/types'
import { AwayBlock, Announcer } from './seen'

const dec = (id: number): WaitingItem => ({ kind: 'decision', action: 'answer', decision_id: id, title: `d${id}`, lane: 'feat/x', since: '2026-10-16T10:00:00Z' })
afterEach(() => window.localStorage.clear())

describe('AwayBlock', () => {
  it('names what needs the person first and renders nothing for a quiet gap', () => {
    const since = Date.parse('2026-10-16T09:00:00Z')
    const { container, rerender } = render(<AwayBlock since={since} items={[dec(1)]} feed={[]} onDismiss={() => {}} />)
    expect(screen.getByRole('link').textContent).toBe('1 decision waiting on you')
    rerender(<AwayBlock since={since} items={[]} feed={[]} onDismiss={() => {}} />)
    expect(container.textContent).toBe('')
  })
})

describe('Announcer', () => {
  it('stays silent on first load and says a new item politely, without moving focus', async () => {
    const { rerender } = render(<Announcer items={[dec(1)]} />)
    const region = screen.getByRole('status')
    expect(region.getAttribute('aria-live')).toBe('polite')
    expect(region.textContent).toBe('')
    const before = document.activeElement
    rerender(<Announcer items={[dec(1), dec(2)]} />)
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5))
    })
    expect(region.textContent).toBe('New decision from feat/x')
    expect(document.activeElement).toBe(before)
  })
})

describe('Announcer clearing', () => {
  it('clears the text after a quiet re-render, not only on the next announcement', async () => {
    vi.useFakeTimers()
    const { rerender } = render(<Announcer items={[dec(1)]} />)
    rerender(<Announcer items={[dec(1), dec(2)]} />)
    await act(async () => void vi.advanceTimersByTime(10))
    expect(screen.getByRole('status').textContent).toBe('New decision from feat/x')
    rerender(<Announcer items={[dec(1), dec(2)]} />)
    await act(async () => void vi.advanceTimersByTime(8100))
    expect(screen.getByRole('status').textContent).toBe('')
    vi.useRealTimers()
  })
})

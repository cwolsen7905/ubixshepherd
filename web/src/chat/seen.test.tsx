import { afterEach, describe, expect, it, vi } from 'vitest'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { seenStore } from '../state/seen'
import type { WaitingItem } from '../api/types'
import { AwayBlock, Announcer } from './seen'

const dec = (id: number): WaitingItem => ({ kind: 'decision', action: 'answer', decision_id: id, title: `d${id}`, lane: 'feat/x', since: '2026-10-16T10:00:00Z' })
afterEach(() => window.localStorage.clear())

describe('AwayBlock', () => {
  it('names what needs the person first and renders nothing for a quiet gap', () => {
    const since = Date.parse('2026-10-16T09:00:00Z')
    const { container, rerender } = render(<AwayBlock since={since} items={[dec(1)]} feed={[]} onDismiss={() => {}} now={Date.parse('2026-10-16T11:14:00Z')} />)
    expect(screen.getByRole('region', { name: 'Since you were away' })).toBeInTheDocument()
    expect(screen.getByText(/Decision: d1/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Answer' })).toBeInTheDocument()
    expect(screen.getByText(/2h 14m, last here/)).toBeInTheDocument()
    rerender(<AwayBlock since={since} items={[]} feed={[]} onDismiss={() => {}} />)
    expect(container.textContent).toBe('')
  })
})

describe('AwayBlock, Mark all seen', () => {
  it('marks everything waiting as seen and dismisses the card', () => {
    const onDismiss = vi.fn()
    render(<AwayBlock since={Date.parse('2026-10-16T09:00:00Z')} items={[dec(1), dec(2)]} feed={[]} onDismiss={onDismiss} now={Date.parse('2026-10-16T11:14:00Z')} />)
    fireEvent.click(screen.getByRole('button', { name: 'Mark all seen' }))
    expect(onDismiss).toHaveBeenCalled()
    expect(seenStore.getSnapshot().seen).toEqual(expect.arrayContaining(['decision:1:0', 'decision:2:0']))
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

import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import type { DeskEvent, DeskQuestion } from '../api/types'
import { DeskRow } from './thread'

const ev = (o: Partial<DeskEvent>) => ({ seq: 1, kind: 'user', created: '2026-01-01T00:00:00Z', ...o }) as DeskEvent
const q = (id: number, state: DeskQuestion['state']) => ({ id, turn: 1, state, text: 'which?', workspace_id: 1, created: '' }) as DeskQuestion

describe('DeskRow reply references', () => {
  it('links a reply to the question it names', () => {
    const onReplyRef = vi.fn()
    render(<DeskRow e={ev({ text: '[Replying to your question 7: which?]\n\nthe app' })} now={Date.now()} onReplyRef={onReplyRef} />)
    fireEvent.click(screen.getByRole('button', { name: 'Replying to your question 7' }))
    expect(onReplyRef).toHaveBeenCalledWith(7)
  })

  it('keeps the anchor of an answered question and offers no actions', () => {
    const { container } = render(
      <DeskRow e={ev({ seq: 3, kind: 'assistant', text: 'Which one?', turn: 1 })} now={Date.now()} questions={[q(7, 'answered'), q(8, 'skipped')]} />,
    )
    expect(container.querySelector('#thread-q7')).not.toBeNull()
    expect(container.querySelector('#thread-q8')).not.toBeNull()
    expect(container.querySelector('.msg-asked')).toBeNull()
  })

  it('highlights the message when a later question of its turn is the jump target', () => {
    const { container } = render(
      <DeskRow e={ev({ seq: 3, kind: 'assistant', text: 'Which one?', turn: 1 })} now={Date.now()} questions={[q(7, 'answered'), q(8, 'answered')]} flash="q8" />,
    )
    expect(container.querySelector('.msg-assistant.is-flash')).not.toBeNull()
  })
})

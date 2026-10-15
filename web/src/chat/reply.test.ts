import { describe, expect, it } from 'vitest'
import type { DeskEvent, DeskQuestion } from '../api/types'
import { closedQuestions, questionsByMessage, replyJumpNote, replyRef } from './reply'

const ev = (seq: number, kind: DeskEvent['kind'], turn?: number) => ({ seq, kind, turn, created: '' }) as DeskEvent
const q = (id: number, turn: number, state: DeskQuestion['state'] = 'open') => ({ id, turn, state, text: '', workspace_id: 1, created: '' }) as DeskQuestion

describe('replyRef', () => {
  it('reads the question a reply names', () => {
    expect(replyRef('[Replying to your question 12: which db?]\n\nthe app')).toBe(12)
  })
  it('finds it after an About line', () => {
    expect(replyRef('About lane x:\n\n[Replying to your question 3: ok?]\n\nyes')).toBe(3)
  })
  it('ignores prose that merely mentions it', () => {
    expect(replyRef('I was Replying to your question 4: earlier')).toBeNull()
    expect(replyRef('hello')).toBeNull()
  })
})

describe('questionsByMessage', () => {
  const events = [ev(1, 'user', 1), ev(2, 'assistant', 1), ev(3, 'assistant', 1)]
  it('puts a question under the last message of its turn', () => {
    expect(questionsByMessage(events, [q(5, 1)]).get(3)?.map((x) => x.id)).toEqual([5])
  })
  it('keeps answered and skipped questions so a reply can still find its row', () => {
    const m = questionsByMessage(events, [q(5, 1, 'answered'), q(6, 1, 'skipped')])
    expect(m.get(3)?.map((x) => x.id)).toEqual([5, 6])
  })
  it('has nothing for a turn that is not loaded', () => {
    expect(questionsByMessage(events, [q(7, 99)]).size).toBe(0)
  })
})

describe('replyJumpNote', () => {
  it('is silent when the row was found and names the question when not', () => {
    expect(replyJumpNote(4, true)).toBe('')
    expect(replyJumpNote(4, false)).toContain('Question 4')
  })
})

describe('closedQuestions', () => {
  it('reads answered and skipped, so a reply to either can find its row', async () => {
    const asked: string[] = []
    const got = await closedQuestions(async (state) => {
      asked.push(state)
      return [q(state === 'answered' ? 1 : 2, 1, state)]
    })
    expect(asked.sort()).toEqual(['answered', 'skipped'])
    expect(got.map((x) => x.id)).toEqual([1, 2])
  })
})

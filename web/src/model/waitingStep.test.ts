import type { WaitingItem } from '../api/types'
import { openKey, openTarget, stepAnchor, stepIndex, waitingKey } from './waitingStep'

const q = (id: number, turn = 2): WaitingItem => ({ kind: 'question', action: 'reply', question_id: id, turn, title: 'Ship it?' })

describe('stepIndex', () => {
  const keys = ['a', 'b', 'c']
  it('goes down and wraps', () => {
    expect(stepIndex(keys, null, 'down')).toBe(0)
    expect(stepIndex(keys, 'a', 'down')).toBe(1)
    expect(stepIndex(keys, 'c', 'down')).toBe(0)
  })
  it('goes up and wraps', () => {
    expect(stepIndex(keys, null, 'up')).toBe(2)
    expect(stepIndex(keys, 'b', 'up')).toBe(0)
    expect(stepIndex(keys, 'a', 'up')).toBe(2)
  })
  it('starts over when the current item has left the list, and says -1 for none', () => {
    expect(stepIndex(keys, 'gone', 'down')).toBe(0)
    expect(stepIndex(keys, 'gone', 'up')).toBe(2)
    expect(stepIndex([], null, 'down')).toBe(-1)
  })
})

describe('waitingKey', () => {
  it('tells a question from a decision with the same number', () => {
    expect(waitingKey(q(4))).not.toBe(waitingKey({ kind: 'decision', action: 'answer', decision_id: 4, title: 't' }))
  })
})

describe('stepAnchor', () => {
  const ctx = {
    askedRows: new Map([[7, 31]]),
    unplaced: new Set([8]),
    laneAnchor: (id: number) => (id === 2 ? 'f40' : undefined),
  }
  it('finds the message of a question', () => expect(stepAnchor(q(5), ctx)).toBe('q5'))
  it('finds a decision by its asked row, else its card', () => {
    expect(stepAnchor({ kind: 'decision', action: 'answer', decision_id: 7, title: 't' }, ctx)).toBe('f31')
    expect(stepAnchor({ kind: 'decision', action: 'answer', decision_id: 8, title: 't' }, ctx)).toBe('d8')
    expect(stepAnchor({ kind: 'decision', action: 'answer', decision_id: 9, title: 't' }, ctx)).toBeNull()
  })
  it('finds a lane item by the lane\'s latest row and a next-up item by none', () => {
    expect(stepAnchor({ kind: 'mr', action: 'approve', lane_id: 2, mr: 3, title: 't' }, ctx)).toBe('f40')
    expect(stepAnchor({ kind: 'ship', action: 'ship', lane_id: 3, title: 't' }, ctx)).toBeNull()
    expect(stepAnchor({ kind: 'next', action: 'review', next_id: 1, title: 't' }, ctx)).toBeNull()
  })
})

describe('openTarget and openKey', () => {
  const dec: WaitingItem = { kind: 'decision', action: 'answer', decision_id: 5, title: 'd' }
  const ques = q(2)
  const mr: WaitingItem = { kind: 'mr', action: 'approve', lane_id: 1, mr: 9, title: 'm' }
  it('maps decisions and questions, and nothing else', () => {
    expect(openTarget(dec)).toEqual({ kind: 'decision', id: 5 })
    expect(openTarget(ques)).toEqual({ kind: 'question', id: 2 })
    expect(openTarget(mr)).toBeNull()
  })
  it('finds the key of the open item', () => {
    expect(openKey([mr, dec, ques], { kind: 'question', id: 2 })).toBe(waitingKey(ques))
    expect(openKey([mr, dec], { kind: 'decision', id: 6 })).toBeNull()
    expect(openKey([dec], undefined)).toBeNull()
  })
})

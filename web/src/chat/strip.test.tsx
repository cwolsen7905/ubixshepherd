import type { WaitingItem } from '../api/types'
import { neighbour, waitingKey } from '../model/waitingStep'
import { advanceFrom } from './strip'

const dec = (id: number): WaitingItem => ({ kind: 'decision', action: 'answer', decision_id: id, title: `d${id}` })
const items = [dec(1), dec(2), dec(3)]

describe('neighbour', () => {
  it('starts at the first item or the last when nothing is open', () => {
    expect(neighbour(items, null, 'down')).toBe(items[0])
    expect(neighbour(items, null, 'up')).toBe(items[2])
  })
  it('steps and wraps', () => {
    expect(neighbour(items, waitingKey(items[2] as WaitingItem), 'down')).toBe(items[0])
    expect(neighbour(items, waitingKey(items[0] as WaitingItem), 'up')).toBe(items[2])
  })
  it('skips the item just answered', () => {
    const k = waitingKey(items[1] as WaitingItem)
    expect(neighbour(items, k, 'down', k)).toBe(items[2])
  })
  it('starts from the top when the open item left the list', () => {
    expect(neighbour([dec(1), dec(3)], waitingKey(dec(2)), 'down')).toEqual(dec(1))
  })
})

describe('advanceFrom', () => {
  it('opens the next item, or closes the inspector when none is left', () => {
    window.location.hash = '#/?open=decision:1'
    advanceFrom(items, waitingKey(dec(1)))
    expect(decodeURIComponent(window.location.hash)).toContain('open=decision:2')
    advanceFrom([dec(1)], waitingKey(dec(1)))
    expect(window.location.hash).not.toContain('open=')
  })
})

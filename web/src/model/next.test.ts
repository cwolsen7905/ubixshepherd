import { nextItem } from '../test/fixtures'
import {
  canMove, canStart, closedCount, deskWorkspace, nextSections, planLabel, railItems, readRailView, stateActions,
  startText, waitingCount, writeRailView,
} from './next'

const first = nextItem({ id: 1, rank: 1 })
const started = nextItem({ id: 2, rank: 2, state: 'started', lane_id: 4 })
const third = nextItem({ id: 3, rank: 3 })
const planned = nextItem({ id: 4, rank: 4, state: 'planned', stored_state: 'planned' })
const deferred = nextItem({ id: 5, rank: 5, state: 'deferred', stored_state: 'deferred' })
const done = nextItem({ id: 6, rank: 6, state: 'done', stored_state: 'done' })
const dropped = nextItem({ id: 7, rank: 7, state: 'dropped', stored_state: 'dropped' })
const items = [first, started, third, planned, deferred, done, dropped]

describe('next-up model', () => {
  it('groups by chosen state and holds back done and dropped', () => {
    expect(nextSections(items, false).map((s) => [s.state, s.items.map((i) => i.id)])).toEqual([
      ['approved', [1, 2, 3]],
      ['planned', [4]],
      ['deferred', [5]],
    ])
    expect(nextSections(items, true).map((s) => s.state)).toEqual(['approved', 'planned', 'deferred', 'done', 'dropped'])
    expect(closedCount(items)).toBe(2)
  })

  it('moves only within a section', () => {
    expect(canMove(items, first, 'up')).toBe(false)
    expect(canMove(items, first, 'down')).toBe(true)
    expect(canMove(items, third, 'down')).toBe(false)
    expect(canMove(items, planned, 'up')).toBe(false)
    expect(canMove(items, planned, 'down')).toBe(false)
  })

  it('offers the person each state change that makes sense', () => {
    expect(stateActions(first).map((a) => a.state)).toEqual(['deferred', 'dropped'])
    expect(stateActions(planned).map((a) => a.state)).toEqual(['approved', 'deferred', 'dropped'])
    expect(stateActions(deferred).map((a) => a.state)).toEqual(['approved', 'dropped'])
    expect(stateActions(dropped).map((a) => a.state)).toEqual(['planned'])
    expect(stateActions(done)).toEqual([])
  })

  it('lists for the rail the approved items nothing has started', () => {
    const list = { items, next_up: 1 }
    expect(railItems(list).map((i) => i.id)).toEqual([1, 3])
    expect(waitingCount(list)).toBe(2)
    const many = { items: Array.from({ length: 9 }, (_, i) => nextItem({ id: i + 1, rank: i })) }
    expect(railItems(many)).toHaveLength(5)
    expect(waitingCount(many)).toBe(9)
  })

  it('words the Start request and says when it applies', () => {
    expect(startText({ id: 7, title: 'Do it' })).toBe('Open a lane for next-up item 7: Do it')
    expect(canStart(first)).toBe(true)
    expect(canStart(started)).toBe(false)
  })

  it('shortens a plan path and picks the desk workspace', () => {
    expect(planLabel('docs/lane-diff.md')).toBe('docs/lane-diff.md')
    expect(planLabel('a/b/docs/x.md')).toBe('docs/x.md')
    expect(deskWorkspace([])).toBe(0)
    expect(deskWorkspace([{ id: 3 } as never])).toBe(0)
    expect(deskWorkspace([{ id: 3 } as never, { id: 4 } as never])).toBe(3)
  })

  it('keeps the rail view, Now when none or storage fails', () => {
    const data = new Map<string, string>()
    const store = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) }
    expect(readRailView(store)).toBe('now')
    writeRailView(store, 'next')
    expect(readRailView(store)).toBe('next')
    expect(readRailView({ getItem: () => { throw new Error('no') } })).toBe('now')
  })
})

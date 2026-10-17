import type { Waiting, WaitingItem } from '../api/types'
import { lane, run } from '../test/fixtures'
import { attentionTitle, inboxTitle, statusCounts, topicWaits, type StatusInput } from './attention'

const seen = { until: '2026-10-08T09:00:00Z', keys: new Set<string>() }
const waiting = (...items: WaitingItem[]): Waiting => ({
  count: items.length,
  counts: { decisions: 0, mrs: 0, ship: 0, next: 0, requests: 0, questions: 0 },
  version: 'v',
  items,
})
const blocked = (o: Partial<WaitingItem> = {}): WaitingItem => ({ kind: 'blocked', action: 'open', title: 'b', ...o })
const req = (id: number): WaitingItem => ({ kind: 'request', action: 'route', request_id: id, title: 'r' })
const input = (o: Partial<StatusInput> = {}): StatusInput => ({
  lanes: [], runs: [], decisions: [], requests: [], seen, waiting: null, ...o,
})

describe('statusCounts', () => {
  it('is all zeros before anything has loaded', () => {
    expect(statusCounts(input())).toEqual({ running: 0, waiting: 0, broken: 0, routing: 0 })
  })

  it('counts lanes with a run in flight as running', () => {
    const lanes = [lane({ id: 1, name: 'a' }), lane({ id: 2, name: 'b' })]
    const runs = [run({ id: 1, lane_id: 1, state: 'running', ended: undefined }), run({ id: 2, lane_id: 2, state: 'succeeded' })]
    expect(statusCounts(input({ lanes, runs })).running).toBe(1)
  })

  it('counts a lane both failed and blocked once', () => {
    const lanes = [lane({ id: 1, name: 'a' }), lane({ id: 2, name: 'b' })]
    const runs = [run({ id: 1, lane_id: 1, state: 'failed' })]
    const w = waiting(blocked({ lane_id: 1, lane: 'a' }), blocked({ lane_id: 2, lane: 'b' }))
    expect(statusCounts(input({ lanes, runs, waiting: w })).broken).toBe(2)
  })

  it('matches a blocked item without a lane id by repo and name', () => {
    const l = lane({ id: 1, name: 'a', repo: 'acme' })
    const runs = [run({ id: 1, lane_id: 1, state: 'failed' })]
    const w = waiting(blocked({ lane: 'a', repo: 'acme' }))
    expect(statusCounts(input({ lanes: [l], runs, waiting: w })).broken).toBe(1)
  })

  it('keeps the four counts disjoint: a request or blocked item is not also waiting on you', () => {
    const decision: WaitingItem = { kind: 'decision', action: 'answer', decision_id: 1, title: 'd' }
    const lanes = [lane({ id: 1, name: 'a' })]
    const c = statusCounts(input({ lanes, waiting: waiting(decision, req(4), blocked({ lane_id: 1, lane: 'a' })) }))
    expect(c).toEqual({ running: 0, waiting: 1, broken: 1, routing: 1 })
    // The window title and favicon read the same number as the strip.
    const w = waiting(decision, req(4), blocked({ lane_id: 1, lane: 'a' }))
    expect(attentionTitle(w)).toBe(`${c.waiting} waiting on you`)
  })
})

describe('inboxTitle', () => {
  it('names the parts of the count', () => {
    const w = waiting({ kind: 'decision', action: 'answer', title: 'd' }, req(1), req(2))
    expect(inboxTitle(w)).toBe('1 decision, 2 requests needing routing')
  })
})

describe('topicWaits', () => {
  const topic = { waits: 5, links: [{ kind: 'decision', ref: '3' }, { kind: 'lane', ref: '9' }, { kind: 'run', ref: '2' }] }
  it('counts linked things that are on the list', () => {
    const w = { count: 1, counts: { decisions: 1, mrs: 0, ship: 0, next: 0 }, items: [{ kind: 'decision', action: 'answer', decision_id: 3, title: 'x' }] } as Waiting
    expect(topicWaits(topic, w)).toBe(1)
  })
  it('keeps the daemon count until the list loads', () => {
    expect(topicWaits(topic, null)).toBe(5)
  })
})

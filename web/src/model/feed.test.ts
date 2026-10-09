import { aboutLane, feedEvent, type Refs } from './feed'
import { feedItem, request, run } from '../test/fixtures'

const lane = { id: 1, name: 'feat/login', repo: 'acme-api' }
const refs: Refs = {
  runs: [run({ id: 10, lane_id: 1 }), run({ id: 11, lane_id: 2 })],
  decisionRuns: new Map([[5, 10], [6, 11]]),
  requests: [request({ id: 3, from_lane: 'feat/login' }), request({ id: 4, from_lane: 'fix/crash' })],
}

describe('feedEvent', () => {
  it("takes the daemon's event, or maps the raw kind for an older daemon", () => {
    const f = { items: [feedItem({ kind: 'decision' }), feedItem({ kind: 'request_failed' }), feedItem({ kind: 'brand_new' })], last: 1 }
    expect([0, 1, 2].map((i) => feedEvent(f, i))).toEqual(['decision_asked', 'request_attention', 'info'])
    expect(feedEvent({ ...f, events: ['mr', 'pipeline', 'info'] }, 0)).toBe('mr')
  })

  it('prefers a kind it knows when the daemon calls it info', () => {
    expect(feedEvent({ items: [feedItem({ kind: 'run_quota' })], events: ['info'], last: 1 }, 0)).toBe('run_quota')
  })
})

describe('aboutLane', () => {
  it('finds run kinds by the run, decisions by their run, requests by their lanes', () => {
    expect(aboutLane(feedItem({ kind: 'run_passed', ref: 10 }), lane, refs)).toBe(true)
    expect(aboutLane(feedItem({ kind: 'commit', ref: 11 }), lane, refs)).toBe(false)
    expect(aboutLane(feedItem({ kind: 'decision', ref: 5 }), lane, refs)).toBe(true)
    expect(aboutLane(feedItem({ kind: 'decision_answered', ref: 6 }), lane, refs)).toBe(false)
    expect(aboutLane(feedItem({ kind: 'request_routed', ref: 3 }), lane, refs)).toBe(true)
    expect(aboutLane(feedItem({ kind: 'request', ref: 4 }), lane, refs)).toBe(false)
  })

  it("matches lane kinds by id only when the text names the lane, since some carry a repo's id", () => {
    expect(aboutLane(feedItem({ kind: 'pipeline', ref: 1, text: 'pipeline 7 failed for !3 (lane feat/login)' }), lane, refs)).toBe(true)
    expect(aboutLane(feedItem({ kind: 'lane_opened', ref: 1, text: 'imported 2 lane(s) from the coordination file' }), lane, refs)).toBe(false)
    expect(aboutLane(feedItem({ kind: 'budget', ref: 0 }), lane, refs)).toBe(false)
  })
})

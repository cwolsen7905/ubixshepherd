import { describe, expect, it } from 'vitest'
import type { FeedEntry } from './feed'
import {
  BACKLOG_GROUPS, backlogCounts, backlogItems, backlogText, readRailOpen, writeRailOpen, type BacklogInput,
} from './backlog'
import { decision, lane, request, run } from '../test/fixtures'

const entry = (o: Partial<FeedEntry> & { id: number }): FeedEntry => ({
  kind: 'run_passed', event: 'run_passed', text: 'run 10 passed', ref: 10, created: '2026-10-08T10:04:00Z', ...o,
})

function input(o: Partial<BacklogInput> = {}): BacklogInput {
  const feed = o.feed ?? []
  return {
    lanes: [], runs: [], decisions: [], requests: [], feed, shown: new Set(feed.map((f) => f.id)), ...o,
  }
}

const groups = (o: Partial<BacklogInput>) => backlogItems(input(o)).map((i) => `${i.group}:${i.key}`)

describe('backlogItems', () => {
  it('orders needs, broken, idle, working, review', () => {
    const lanes = [
      lane({ id: 1, name: 'idle-lane' }),
      lane({ id: 2, name: 'busy' }),
      lane({ id: 3, name: 'broke' }),
      lane({ id: 4, name: 'mr', mr: 7, mr_state: 'open', pipeline_status: 'passed' }),
    ]
    const runs = [
      run({ id: 21, lane_id: 2, state: 'running', ended: undefined, started: '2026-10-08T10:00:00Z' }),
      run({ id: 31, lane_id: 3, state: 'failed' }),
      run({ id: 11, lane_id: 1, state: 'succeeded', ended: '2026-10-08T09:00:00Z' }),
      run({ id: 41, lane_id: 4, state: 'succeeded' }),
    ]
    const got = groups({ lanes, runs, decisions: [decision({ id: 9 })], requests: [request({ id: 4 })] })
    expect(got).toEqual([
      'needs:decision:9', 'needs:request:4', 'broken:lane:3', 'idle:idle:1', 'working:lane:2', 'review:lane:4',
    ])
  })

  it('lists an open lane with no run as idle, longest idle first, from the last run end or creation', () => {
    const lanes = [
      lane({ id: 1, name: 'fresh', created: '2026-10-08T09:00:00Z' }),
      lane({ id: 2, name: 'older', created: '2026-10-07T09:00:00Z' }),
      lane({ id: 3, name: 'rested', created: '2026-10-01T00:00:00Z' }),
    ]
    const runs = [run({ id: 5, lane_id: 3, ended: '2026-10-08T09:30:00Z' })]
    const items = backlogItems(input({ lanes, runs }))
    expect(items.map((i) => i.name)).toEqual(['older', 'fresh', 'rested'])
    expect(items.map((i) => i.detail)).toEqual(['no run yet', 'no run yet', 'last run 5 succeeded'])
    expect(items[2]?.at).toBe('2026-10-08T09:30:00Z')
  })

  it('leaves out done work, closed lanes, lanes waiting on a pipeline, and dismissed failures', () => {
    const lanes = [
      lane({ id: 1, name: 'closed', state: 'closed', closed: '2026-10-08T10:00:00Z', mr_state: 'merged' }),
      lane({ id: 2, name: 'ci', mr: 3, mr_state: 'open', pipeline_status: 'running' }),
      lane({ id: 3, name: 'dropped' }),
      lane({ id: 4, name: 'opening', state: 'opening' }),
    ]
    const runs = [
      run({ id: 7, lane_id: 2 }),
      run({ id: 8, lane_id: 3, state: 'failed', dismissed: '2026-10-08T11:00:00Z' }),
    ]
    expect(groups({ lanes, runs })).toEqual([])
  })

  it('shows a dismissed failure again when asked, as broken', () => {
    const lanes = [lane({ id: 3, name: 'dropped' })]
    const runs = [run({ id: 8, lane_id: 3, state: 'failed', dismissed: '2026-10-08T11:00:00Z' })]
    expect(groups({ lanes, runs, showDismissed: true })).toEqual(['broken:lane:3'])
  })

  it('qualifies a lane name two repos share', () => {
    const lanes = [lane({ id: 1, name: 'feat/x', repo: 'api' }), lane({ id: 2, name: 'feat/x', repo: 'web' })]
    expect(backlogItems(input({ lanes })).map((i) => i.name).sort()).toEqual(['api:feat/x', 'web:feat/x'])
  })

  it('does not show a lane with a failed pipeline as idle', () => {
    const lanes = [lane({ id: 1, mr: 3, mr_state: 'open', pipeline_status: 'failed' })]
    expect(groups({ lanes })).toEqual(['broken:lane:1'])
  })
})

describe('anchors', () => {
  const lanes = [lane({ id: 1 })]
  const runs = [run({ id: 10, lane_id: 1, ended: '2026-10-08T10:04:00Z' })]

  it('names the latest thread row about the lane, so an idle lane leads to where it went quiet', () => {
    const feed = [
      entry({ id: 5, kind: 'run_started', event: 'run_started', ref: 10 }),
      entry({ id: 6, ref: 10 }),
      entry({ id: 7, kind: 'run_passed', ref: 99 }),
    ]
    const [it] = backlogItems(input({ lanes, runs, feed }))
    expect(it?.group).toBe('idle')
    expect(it?.anchor).toBe('f6')
  })

  it('ignores rows the thread does not show', () => {
    const feed = [entry({ id: 5 }), entry({ id: 6 })]
    const [it] = backlogItems(input({ lanes, runs, feed, shown: new Set([5]) }))
    expect(it?.anchor).toBe('f5')
    expect(backlogItems(input({ lanes, runs, feed, shown: new Set() }))[0]?.anchor).toBeUndefined()
  })

  it('anchors a decision to its asked row, or to the bottom of the thread when none is loaded', () => {
    const d = decision({ id: 9 })
    const asked = entry({ id: 12, kind: 'decision', event: 'decision_asked', ref: 9 })
    expect(backlogItems(input({ lanes, runs, decisions: [d], feed: [asked] })).find((i) => i.decision)?.anchor).toBe('f12')
    expect(backlogItems(input({ lanes, runs, decisions: [d] })).find((i) => i.decision)?.anchor).toBe('d9')
  })

  it('anchors a request to its attention row', () => {
    const q = request({ id: 4 })
    const row = entry({ id: 20, kind: 'request_needs_routing', event: 'request_attention', ref: 4 })
    expect(backlogItems(input({ lanes, runs, requests: [q], feed: [row] })).find((i) => i.request)?.anchor).toBe('f20')
  })
})

describe('counts and storage', () => {
  it('counts and words the groups that hold anything', () => {
    const items = backlogItems(input({ lanes: [lane({ id: 1 }), lane({ id: 2, name: 'b' })], decisions: [decision()] }))
    const n = backlogCounts(items)
    expect(n.needs).toBe(1)
    // The lane the decision is about is already listed as needing the person, not as idle.
    expect(n.idle).toBe(1)
    expect(backlogText(n)).toBe('1 needs you · 1 idle')
    expect(BACKLOG_GROUPS[0]).toBe('needs')
  })

  it('remembers the rail open or closed, and reports no choice', () => {
    const data = new Map<string, string>()
    const store = { getItem: (k: string) => data.get(k) ?? null, setItem: (k: string, v: string) => void data.set(k, v) }
    expect(readRailOpen(store)).toBeNull()
    writeRailOpen(store, false)
    expect(readRailOpen(store)).toBe(false)
    writeRailOpen(store, true)
    expect(readRailOpen(store)).toBe(true)
  })
})

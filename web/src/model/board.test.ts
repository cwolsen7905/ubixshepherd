import { boardItems, canDismiss, counts, dismissedCount, readShowDismissed, writeShowDismissed, countsText, laneClashes, noteOutcome, type BoardInput, type Seen } from './board'
import { decision, lane, request, run } from '../test/fixtures'

// The person last cleared the board at 09:00; the fixtures' runs end at 10:04.
const seen: Seen = { until: '2026-10-08T09:00:00Z', keys: new Set() }

function items(o: Partial<BoardInput>) {
  return boardItems({ lanes: [], runs: [], decisions: [], requests: [], seen, ...o })
}

function only(o: Partial<BoardInput>) {
  const all = items(o)
  expect(all).toHaveLength(1)
  return all[0]!
}

const openMR = (status: 'passed' | 'failed' | 'running') =>
  ({ mr: 34, mr_state: 'open', mr_url: 'https://forge.example.com/acme/api/-/merge_requests/34', pipeline_status: status }) as const

describe('boardItems', () => {
  it('says what a running run is doing now, when the daemon says', () => {
    const progress = { line: 'running make check (3 commits, 12m)', step: 'running make check', commits: 3, elapsed_seconds: 720 }
    const runs = [run({ id: 12, state: 'running', ended: undefined, progress })]
    expect(only({ lanes: [lane()], runs })).toMatchObject({ group: 'working', detail: 'running make check (3 commits, 12m)' })
  })

  it('puts decisions and requests that need routing first', () => {
    const [d, q] = items({ decisions: [decision()], requests: [request()], lanes: [lane()] })
    expect(d).toMatchObject({ group: 'needs', detail: expect.stringMatching(/^decision 5: /), laneId: 1 })
    expect(q).toMatchObject({ group: 'needs', glyph: '!', detail: expect.stringMatching(/^request 3 needs routing: /) })
  })

  it('reads a lane by its latest run', () => {
    const runs = [run({ id: 10, state: 'failed' }), run({ id: 12, state: 'running', ended: undefined }), run({ id: 11, state: 'succeeded' })]
    expect(only({ lanes: [lane()], runs })).toMatchObject({ group: 'working', detail: 'run 12', at: runs[1]?.started })
  })

  it('calls a lane broken when its latest run failed or was interrupted', () => {
    for (const state of ['failed', 'interrupted'] as const) {
      expect(only({ lanes: [lane()], runs: [run({ state })] })).toMatchObject({ group: 'broken', detail: `run 10 ${state}`, at: '2026-10-08T10:04:00Z' })
    }
  })

  it('asks the person to approve an open MR held at require-approval, aged from the pipeline', () => {
    for (const job of ['pending', 'manual', 'blocked', 'failed']) {
      const l = lane({
        ...openMR('running'), pipeline_status: job === 'failed' ? 'failed' : 'running', require_approval: job,
        pipeline_started: '2026-10-08T11:00:00Z', mr_updated: '2026-10-08T10:30:00Z',
      })
      expect(only({ lanes: [l], runs: [run()] })).toMatchObject({
        group: 'needs', detail: 'approve MR !34', laneId: 1, at: '2026-10-08T11:00:00Z',
      })
    }
    const f = lane({ ...openMR('failed'), require_approval: 'failed', pipeline_started: '2026-10-08T11:00:00Z', pipeline_finished: '2026-10-08T11:05:00Z' })
    expect(only({ lanes: [f], runs: [run()] }).at).toBe('2026-10-08T11:05:00Z')
    const m = lane({ ...openMR('running'), require_approval: 'manual', mr_updated: '2026-10-08T10:30:00Z' })
    expect(only({ lanes: [m], runs: [run()] }).at).toBe('2026-10-08T10:30:00Z')
  })

  it('does not ask for approval once approved, with no require-approval job, or when the pipeline broke elsewhere', () => {
    const approved = lane({ ...openMR('running'), require_approval: 'manual', approved: true })
    expect(items({ lanes: [approved], runs: [run()] }).map((i) => i.group)).not.toContain('needs')
    const running = lane({ ...openMR('running'), require_approval: 'running' })
    expect(items({ lanes: [running], runs: [run()] }).map((i) => i.group)).not.toContain('needs')
    const elsewhere = lane({ ...openMR('failed'), require_approval: 'manual' })
    expect(only({ lanes: [elsewhere], runs: [run()] })).toMatchObject({ group: 'broken', detail: 'pipeline failed' })
    const closed = lane({ ...openMR('running'), require_approval: 'manual', mr_state: 'merged' })
    expect(items({ lanes: [closed], runs: [run()] }).map((i) => i.group)).not.toContain('needs')
  })

  it('calls a lane broken when its open MR failed its pipeline, and says when an agent is fixing it', () => {
    expect(only({ lanes: [lane(openMR('failed'))], runs: [run()] })).toMatchObject({ group: 'broken', detail: 'pipeline failed' })
    const fixing = run({ id: 11, state: 'running', ended: undefined })
    expect(only({ lanes: [lane(openMR('failed'))], runs: [run(), fixing] })).toMatchObject({ group: 'broken', detail: 'pipeline failed, fixing' })
  })

  it('puts an open MR with a green pipeline to review, with or without a run', () => {
    expect(only({ lanes: [lane(openMR('passed'))] })).toMatchObject({ group: 'review', detail: 'waiting for a merge' })
    expect(only({ lanes: [lane(openMR('passed'))], runs: [run()] })).toMatchObject({ group: 'review' })
    expect(items({ lanes: [lane({ ...openMR('passed'), mr_state: 'merged' })] })).toEqual([])
  })

  it('shows a finished run as done until the person has seen it', () => {
    expect(only({ lanes: [lane()], runs: [run()] })).toMatchObject({ group: 'done', detail: 'run 10 succeeded' })
    expect(items({ lanes: [lane()], runs: [run()], seen: { ...seen, keys: new Set(['run:10']) } })).toEqual([])
    expect(items({ lanes: [lane()], runs: [run()], seen: { ...seen, until: '2026-10-08T11:00:00Z' } })).toEqual([])
  })

  it('shows a merged, closed lane as done until seen', () => {
    const closed = lane({ state: 'closed', mr: 34, mr_state: 'merged', closed: '2026-10-08T10:30:00Z' })
    expect(only({ lanes: [closed] })).toMatchObject({ group: 'done', detail: 'merged' })
    expect(items({ lanes: [closed], seen: { ...seen, keys: new Set(['lane:1']) } })).toEqual([])
    expect(items({ lanes: [{ ...closed, mr_state: 'closed' }] })).toEqual([])
  })

  it('orders by group, waiting longest first, and done newest first', () => {
    const lanes = [1, 2, 3, 4, 5, 6].map((id) => lane({ id, name: `l${id}` }))
    lanes[2] = lane({ id: 3, name: 'l3', ...openMR('passed') })
    const runs = [
      run({ id: 1, lane_id: 1, state: 'failed', ended: '2026-10-08T10:20:00Z' }),
      run({ id: 2, lane_id: 2, state: 'failed', ended: '2026-10-08T10:10:00Z' }),
      run({ id: 4, lane_id: 4, state: 'running', started: '2026-10-08T10:00:00Z', ended: undefined }),
      run({ id: 5, lane_id: 5, ended: '2026-10-08T10:01:00Z' }),
      run({ id: 6, lane_id: 6, ended: '2026-10-08T10:02:00Z' }),
    ]
    const got = items({ lanes, runs, decisions: [decision({ lane: 'l9' })] }).map((it) => `${it.group}:${it.name}`)
    expect(got).toEqual(['needs:l9', 'broken:l2', 'broken:l1', 'review:l3', 'working:l4', 'done:l6', 'done:l5'])
  })

  it('names a lane by repo:lane only when two repos use its name', () => {
    const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/x' }), lane({ id: 2, repo: 'acme-web', name: 'feat/x' }), lane({ id: 3, name: 'feat/y' })]
    const runs = lanes.map((l) => run({ id: l.id, lane_id: l.id }))
    expect(items({ lanes, runs }).map((it) => it.name).sort()).toEqual(['acme-api:feat/x', 'acme-web:feat/x', 'feat/y'])
    expect(laneClashes(lanes)).toEqual(new Set(['feat/x']))
  })
})

describe('counts', () => {
  it('counts by group and writes the groups that hold anything, most urgent first', () => {
    const n = counts(items({ decisions: [decision()], lanes: [lane(), lane({ id: 2, name: 'b' })], runs: [run({ state: 'failed' }), run({ id: 11, lane_id: 2, state: 'failed' })] }))
    expect(n).toEqual({ needs: 1, broken: 2, review: 0, working: 0, done: 0 })
    expect(countsText(n)).toBe('1 needs you · 2 broken')
    expect(countsText(counts([]))).toBe('')
  })
})

describe('noteOutcome', () => {
  const running = [run({ id: 10, state: 'running', ended: undefined }), run({ id: 11 })]

  it("marks a running run's end as the feed reports it", () => {
    const out = noteOutcome(running, 'run_failed', 10, '2026-10-08T10:09:00Z')
    expect(out[0]).toMatchObject({ state: 'failed', ended: '2026-10-08T10:09:00Z' })
    expect(noteOutcome(running, 'run_passed', 10, 'x')[0]?.state).toBe('succeeded')
    expect(noteOutcome(running, 'run_interrupted', 10, 'x')[0]?.state).toBe('interrupted')
  })

  it('leaves the list alone for other events, other runs, and runs that already ended', () => {
    expect(noteOutcome(running, 'run_ended', 10, 'x')).toBe(running)
    expect(noteOutcome(running, 'run_passed', 99, 'x')).toBe(running)
    expect(noteOutcome(running, 'run_failed', 11, 'x')).toBe(running)
    expect(noteOutcome(running, 'run_passed', undefined, 'x')).toBe(running)
  })
})

describe('dismissed runs', () => {
  const dismissed = { dismissed: '2026-10-08T10:30:00Z' }

  it('takes a dismissed failed run off the board', () => {
    for (const state of ['failed', 'interrupted', 'stopped'] as const) {
      expect(items({ lanes: [lane()], runs: [run({ state, ...dismissed })] })).toEqual([])
    }
  })

  it('shows a lane\'s next run as usual, because dismissal belongs to one run', () => {
    const runs = [run({ id: 10, state: 'failed', ...dismissed }), run({ id: 11, state: 'running', ended: undefined })]
    expect(only({ lanes: [lane()], runs })).toMatchObject({ group: 'working', detail: 'run 11' })
    const again = [run({ id: 10, state: 'failed', ...dismissed }), run({ id: 11, state: 'failed', ended: '2026-10-08T11:00:00Z' })]
    expect(only({ lanes: [lane()], runs: again })).toMatchObject({ group: 'broken', detail: 'run 11 failed' })
  })

  it('still flags a failed pipeline, whatever happened to the run', () => {
    expect(only({ lanes: [lane(openMR('failed'))], runs: [run({ state: 'failed', ...dismissed })] })).toMatchObject({
      group: 'broken',
      detail: 'pipeline failed',
    })
  })

  it('lists dismissed runs, marked, when asked', () => {
    const runs = [run({ state: 'failed', ...dismissed })]
    expect(only({ lanes: [lane()], runs, showDismissed: true })).toMatchObject({ group: 'broken', detail: 'run 10 failed, dismissed' })
  })

  it('counts the lanes whose latest run is dismissed', () => {
    const lanes = [lane(), lane({ id: 2, name: 'other' }), lane({ id: 3, name: 'closed', state: 'closed' })]
    const runs = [
      run({ id: 10, lane_id: 1, state: 'failed', ...dismissed }),
      run({ id: 11, lane_id: 2, state: 'failed' }),
      run({ id: 12, lane_id: 3, state: 'failed', ...dismissed }),
    ]
    expect(dismissedCount(lanes, runs)).toBe(1)
  })

  it('offers Dismiss on a failed run not yet dismissed, and not otherwise', () => {
    const can = (o: Partial<BoardInput>) => items(o).filter(canDismiss).length === 1
    expect(can({ lanes: [lane()], runs: [run({ state: 'failed' })] })).toBe(true)
    expect(can({ lanes: [lane()], runs: [run({ state: 'failed', ...dismissed })], showDismissed: true })).toBe(false)
    expect(can({ lanes: [lane()], runs: [run()] })).toBe(false)
    expect(can({ lanes: [lane()], runs: [run({ state: 'running', ended: undefined })] })).toBe(false)
  })

  it('remembers whether the person shows them', () => {
    const mem = new Map<string, string>()
    const store = {
      getItem: (k: string) => mem.get(k) ?? null,
      setItem: (k: string, v: string) => void mem.set(k, v),
      removeItem: (k: string) => void mem.delete(k),
    }
    expect(readShowDismissed(store)).toBe(false)
    writeShowDismissed(store, true)
    expect(readShowDismissed(store)).toBe(true)
    writeShowDismissed(store, false)
    expect(readShowDismissed(store)).toBe(false)
  })
})

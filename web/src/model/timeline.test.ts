import { describe, expect, it } from 'vitest'
import {
  agentClass, agentsIn, assemble, fetchSince, labelWidth, layoutTimeline, levelOfDetail, MIN_BAR_PX, overlaps, runLabel, runPhrase,
  same, spendAt, stabilise, startOfDay, summaryText, tableRows, ticks, windowFor, xOf,
  type BarMark, type Metrics, type TimeWindow, type TimelineData, type TLane, type TRun,
} from './timeline'
import { busyDay } from './timeline.fixtures'

const MIN = 60_000
const HOUR = 60 * MIN
const METRICS: Metrics = { width: 1200, rem: 16 }
const at = (h: number, m = 0) => new Date(2026, 9, 10, h, m).getTime()

// The tests run under Node, which follows a change to TZ at once; the app's types do not know process.
const env = (globalThis as unknown as { process: { env: Record<string, string | undefined> } }).process.env

function withTZ<T>(tz: string, fn: () => T): T {
  const old = env.TZ
  env.TZ = tz
  try {
    return fn()
  } finally {
    if (old === undefined) delete env.TZ
    else env.TZ = old
  }
}

describe('the visible window', () => {
  it('runs Today from local midnight, with headroom to the right of now', () => {
    const w = windowFor('today', at(15), 1)
    expect(w.from).toBe(at(0))
    expect(w.now).toBe(at(15))
    expect(w.to).toBeGreaterThan(at(15))
    expect(w.to - w.now).toBeLessThanOrEqual(0.02 * 15 * HOUR + 1)
  })

  it('shows at least an hour of a day that has only begun', () => {
    const w = windowFor('today', at(0, 5), 1)
    expect(w.from).toBe(at(0))
    expect(w.to - w.from).toBeGreaterThanOrEqual(HOUR)
  })

  it('takes the last 24 hours and 7 days back from now', () => {
    const d = windowFor('24h', at(15), 1)
    expect(d.now - d.from).toBeGreaterThanOrEqual(24 * HOUR)
    expect(d.now - d.from).toBeLessThan(24 * HOUR + 40 * MIN)
    const wk = windowFor('week', at(15), 1)
    expect(wk.now - wk.from).toBeGreaterThanOrEqual(7 * 24 * HOUR)
    expect(wk.to).toBeGreaterThan(wk.now)
  })

  it('rounds the right edge up to the quantum, so marks hold still between recomputes', () => {
    const a = windowFor('24h', at(15, 0), 5 * MIN)
    const b = windowFor('24h', at(15, 1), 5 * MIN)
    expect(a.to % (5 * MIN)).toBe(0)
    expect(b.to).toBe(a.to)
    expect(b.from).toBe(a.from)
  })

  it('counts Today from the local midnight across a daylight-saving change', () => {
    withTZ('America/New_York', () => {
      // 1 November 2026: the clocks go back, a day of 25 hours.
      const now = new Date(2026, 10, 1, 15, 0).getTime()
      const from = startOfDay(now)
      expect(windowFor('today', now, 1).from).toBe(from)
      expect(now - from).toBe(16 * HOUR)
      // The 7-day window is 7 x 24 absolute hours, not seven calendar days.
      const wk = windowFor('week', now, 1)
      expect(wk.to - wk.from).toBe(Math.round(7 * 24 * HOUR * 1.02))
    })
  })

  it('reads the history from midnight of the first day, or six hours back, whichever is earlier', () => {
    expect(fetchSince(at(0))).toBe(at(0) - 6 * HOUR)
    expect(fetchSince(at(15))).toBe(at(0))
    expect(fetchSince(at(3))).toBe(at(3) - 6 * HOUR)
  })

  it('positions a time within the window, clamped to the plot', () => {
    const w: TimeWindow = { from: 0, to: 1000, now: 900 }
    expect(xOf(w, 200, 500)).toBe(100)
    expect(xOf(w, 200, -50)).toBe(0)
    expect(xOf(w, 200, 5000)).toBe(200)
  })
})

describe('axis ticks', () => {
  it('lands hourly ticks on local hour boundaries and names the day boundary', () => {
    const w: TimeWindow = { from: at(0, 0), to: at(8, 30), now: at(8, 20) }
    const t = ticks(w, 12)
    expect(t.map((x) => x.label).slice(0, 4)).toEqual(['Sat 10 Oct', '01:00', '02:00', '03:00'])
    expect(t[0]?.major).toBe(true)
    expect(t.every((x) => x.at >= w.from && x.at <= w.to)).toBe(true)
  })

  it('coarsens as the window grows or the width shrinks', () => {
    const w: TimeWindow = { from: at(0), to: at(15), now: at(15) }
    expect(ticks(w, 4).length).toBeLessThan(ticks(w, 24).length)
    // 15 hours in four ticks is a step of 6 hours: 00:00, 06:00, 12:00.
    expect(ticks(w, 4).map((x) => x.label)).toEqual(['Sat 10 Oct', '06:00', '12:00'])
  })

  it('names each day on a week, at midnight', () => {
    const t = ticks(windowFor('week', at(15), 1), 8)
    expect(t.length).toBeGreaterThanOrEqual(6)
    expect(t.every((x) => x.major)).toBe(true)
    expect(t.some((x) => x.label === 'Sat 10 Oct')).toBe(true)
  })

  it('keeps 3-hour ticks on wall-clock hours on the day the clocks change', () => {
    withTZ('America/New_York', () => {
      const from = new Date(2026, 10, 1, 0, 0).getTime()
      const to = new Date(2026, 10, 1, 23, 0).getTime()
      const labels = ticks({ from, to, now: to }, 8).map((t) => t.label)
      expect(labels).toEqual(['Sun 1 Nov', '03:00', '06:00', '09:00', '12:00', '15:00', '18:00', '21:00'])
    })
  })
})

describe('the busy day', () => {
  const { src, now, ids } = busyDay()
  const data = assemble(src)
  const w = windowFor('today', now, 1)
  const layout = layoutTimeline(data, w, METRICS)
  const tracks = layout.groups.flatMap((g) => g.lanes)

  it('has a track for every lane that did anything today, grouped by repo in name order', () => {
    expect(layout.groups.map((g) => g.repo)).toEqual(['acme-api', 'acme-docs', 'acme-infra', 'acme-mobile', 'acme-web'])
    // 24 lanes, the idle one left out.
    expect(tracks).toHaveLength(24)
    expect(layout.summary.idle).toBe(1)
    expect(layoutTimeline(data, w, METRICS, { showIdle: true }).groups.flatMap((g) => g.lanes)).toHaveLength(25)
  })

  it('orders a repo’s lanes by when they began', () => {
    for (const g of layout.groups) {
      const firsts = g.lanes.map((l) =>
        Math.min(...l.bars.flatMap((b) => (b.kind === 'bar' ? [b.run.started] : b.runs.map((r) => r.started)))),
      )
      expect(firsts).toEqual([...firsts].sort((a, b) => a - b))
    }
  })

  it('draws a bar for every run overlapping the day, five of them running', () => {
    const bars = tracks.flatMap((t) => t.bars)
    const runs = bars.flatMap((b) => (b.kind === 'bar' ? [b.run] : b.runs))
    expect(runs.length).toBe(data.runs.filter((r) => overlaps(r, w)).length)
    expect(bars.filter((b): b is BarMark => b.kind === 'bar' && b.running)).toHaveLength(5)
    expect(layout.summary.running).toBe(5)
    expect(new Set(runs.map((r) => r.agent))).toEqual(new Set(['claude', 'opencode', 'cursor', 'copilot']))
  })

  it('draws a running bar to now, and no further', () => {
    const running = tracks.flatMap((t) => t.bars).filter((b): b is BarMark => b.kind === 'bar' && b.running)
    for (const b of running) expect(b.x + b.w).toBeCloseTo(layout.nowX, 0)
  })

  it('draws a run that began before midnight from the left edge and flags it', () => {
    const bar = tracks.flatMap((t) => t.bars).find((b): b is BarMark => b.kind === 'bar' && b.run.id === ids.straddling)
    expect(bar?.continuing).toBe(true)
    expect(bar?.x).toBe(0)
    expect(bar?.w).toBeGreaterThan(0)
  })

  it('marks the two failures with a notch and nothing else', () => {
    const failed = tracks.flatMap((t) => t.bars).filter((b): b is BarMark => b.kind === 'bar' && b.failed)
    expect(failed.map((b) => b.run.id).sort()).toEqual([...ids.failed].sort())
  })

  it('puts the waiting decision and the request on their lanes, joined to the bar that asked', () => {
    const marks = tracks.flatMap((t) => t.decisions)
    const open = marks.filter((d) => d.open)
    expect(open.map((d) => d.key).sort()).toEqual(['decision:900', 'request:950'])
    const decision = open.find((d) => d.key === 'decision:900')
    expect(decision?.joinTo).toBeGreaterThan(decision?.x ?? 0)
    expect(marks.filter((d) => !d.open).map((d) => d.id).sort()).toEqual([...ids.answered].sort())
    expect(layout.summary.waiting).toBe(2)
  })

  it('puts a merge tick on each merged lane, closed or not yet closed', () => {
    const merges = tracks.flatMap((t) => t.merges)
    expect(merges).toHaveLength(3)
    expect(layout.summary.merged).toBe(3)
    const open = tracks.find((t) => t.id === ids.openMerged)
    expect(open?.merges[0]?.at).toBe(now - 40 * MIN)
  })

  it('ends the spend curve at the day’s total, the front desk’s share placed at now', () => {
    expect(layout.spend.total).toBeCloseTo(18.4, 1)
    expect(layout.spend.steps.at(-1)?.source).toBe('rest')
    expect(layout.spend.overBudget).toBe(false)
  })

  it('answers the board’s counts in one line', () => {
    expect(summaryText(layout.summary, 'today')).toBe('5 running, 2 waiting on you, 3 merged, $18.40 of $25.00 today')
    expect(summaryText({ ...layout.summary, spent: 3, budget: 0 }, 'week')).toBe('5 running, 2 waiting on you, 3 merged')
    expect(summaryText({ running: 0, waiting: 0, merged: 0, idle: 0, spent: null, budget: 0 }, 'today')).toBe('')
  })

  it('offers every run of the window to the table, newest first', () => {
    const rows = tableRows(data, w)
    expect(rows.length).toBe(data.runs.filter((r) => overlaps(r, w)).length)
    expect(rows.map((r) => r.run.started)).toEqual(rows.map((r) => r.run.started).sort((a, b) => b - a))
    expect(rows[0]?.repo).toMatch(/^acme-/)
  })

  it('lists the four agents first, then any other the data holds', () => {
    expect(agentsIn(data)).toEqual(['claude', 'opencode', 'cursor', 'copilot'])
    const odd: TimelineData = { ...data, runs: [...data.runs, { ...(data.runs[0] as TRun), id: 9999, agent: 'zed' }] }
    expect(agentsIn(odd)).toEqual(['claude', 'opencode', 'cursor', 'copilot', 'zed'])
  })
})

describe('overlap', () => {
  const w: TimeWindow = { from: 1000, to: 2000, now: 1900 }
  const run = (o: Partial<TRun>): TRun => ({ id: 1, laneId: 1, agent: 'claude', state: 'succeeded', started: 1100, ended: 1200, commits: 0, task: '', ...o })

  it('keeps a run that ends inside the window and one still going, drops one that ended before or began after', () => {
    expect(overlaps(run({ started: 500, ended: 1500 }), w)).toBe(true)
    expect(overlaps(run({ started: 500, ended: 900 }), w)).toBe(false)
    expect(overlaps(run({ started: 2000, ended: 2100 }), w)).toBe(false)
    expect(overlaps(run({ started: 500, ended: undefined, state: 'running' }), w)).toBe(true)
  })
})

describe('level of detail', () => {
  const bar = (id: number, x: number, w: number, o: Partial<BarMark> = {}): BarMark => ({
    kind: 'bar', key: `run:${id}`, x, w, running: false, failed: false, continuing: false, labelled: false,
    run: { id, laneId: 1, agent: 'claude', state: 'succeeded', started: id, ended: id + 1, commits: 0, task: '' }, ...o,
  })

  it('merges runs narrower than 2 px that sit together into one tick', () => {
    const out = levelOfDetail([bar(1, 10, 0.4), bar(2, 10.6, 0.5), bar(3, 11.4, 0.5), bar(4, 100, 30)])
    expect(out).toHaveLength(2)
    const tick = out[0]
    expect(tick?.kind).toBe('density')
    if (tick?.kind === 'density') {
      expect(tick.runs.map((r) => r.id)).toEqual([1, 2, 3])
      expect(tick.w).toBeGreaterThanOrEqual(MIN_BAR_PX)
    }
    expect(out[1]?.kind).toBe('bar')
  })

  it('keeps a lone narrow run as itself, at the minimum width', () => {
    const out = levelOfDetail([bar(1, 10, 0.3), bar(2, 100, 20)])
    expect(out[0]).toMatchObject({ kind: 'bar', w: MIN_BAR_PX })
  })

  it('never merges a running bar, and does not bridge a wide bar between narrow ones', () => {
    const out = levelOfDetail([bar(1, 10, 0.4), bar(2, 10.5, 0.4, { running: true }), bar(3, 40, 10), bar(4, 60, 0.4), bar(5, 60.5, 0.4)])
    expect(out.map((o) => o.kind)).toEqual(['bar', 'bar', 'bar', 'density'])
  })

  it('counts the failures in a tick', () => {
    const out = levelOfDetail([bar(1, 10, 0.4, { failed: true }), bar(2, 10.5, 0.4)])
    expect(out[0]).toMatchObject({ kind: 'density', failed: 1 })
  })
})

describe('the spend band', () => {
  const lane: TLane = { id: 1, repo: 'r', name: 'l', state: 'open', created: 0 }
  const run = (id: number, endedAt: number, cost: number): TRun => ({
    id, laneId: 1, agent: 'claude', state: 'succeeded', started: endedAt - 5 * MIN, ended: endedAt, commits: 1, costUsd: cost, task: '',
  })
  const day = '2026-10-10'
  const w = windowFor('today', at(15), 1)

  it('adds each run’s cost at the moment it ended and ends at the day’s total', () => {
    const data: TimelineData = {
      lanes: [lane], runs: [run(1, at(9), 2), run(2, at(11), 3), run(3, at(13), 1.5)], decisions: [],
      spend: { day, usd: 6.5, budget: 25 },
    }
    const band = layoutTimeline(data, w, METRICS).spend
    expect(band.steps.map((s) => s.usd)).toEqual([2, 3, 1.5])
    expect(band.total).toBeCloseTo(6.5)
    expect(spendAt(band, at(10)).cum).toBe(2)
    expect(spendAt(band, at(12)).cum).toBe(5)
    expect(spendAt(band, at(12)).step?.run).toBe(2)
  })

  it('places what the runs do not account for at now, as the rest of the day’s total', () => {
    const data: TimelineData = { lanes: [lane], runs: [run(1, at(9), 2)], decisions: [], spend: { day, usd: 5, budget: 25 } }
    const band = layoutTimeline(data, w, METRICS).spend
    expect(band.total).toBeCloseTo(5)
    expect(band.steps.at(-1)).toMatchObject({ source: 'rest', usd: 3, at: at(15) })
  })

  it('turns the part past the budget into its own shape, and says so', () => {
    const under = layoutTimeline({ lanes: [lane], runs: [run(1, at(9), 2)], decisions: [], spend: { day, usd: 2, budget: 25 } }, w, METRICS).spend
    expect(under.overBudget).toBe(false)
    expect(under.over).toBe('')
    const over = layoutTimeline({ lanes: [lane], runs: [run(1, at(9), 20), run(2, at(12), 9)], decisions: [], spend: { day, usd: 29, budget: 25 } }, w, METRICS).spend
    expect(over.overBudget).toBe(true)
    expect(over.over).not.toBe('')
    expect(over.budgetY).toBeGreaterThan(0)
  })

  it('falls back to nothing at midnight on a week, since the budget is a day’s', () => {
    const wk = windowFor('week', at(15), 1)
    const yesterday = at(15) - 24 * HOUR
    const data: TimelineData = {
      lanes: [lane], runs: [run(1, yesterday, 6), run(2, at(10), 2)], decisions: [], spend: { day, usd: 2, budget: 25 },
    }
    const band = layoutTimeline(data, wk, METRICS).spend
    expect(spendAt(band, yesterday + MIN).cum).toBe(6)
    expect(spendAt(band, at(0) - MIN).cum).toBe(6)
    expect(spendAt(band, at(0) + MIN).cum).toBe(0)
    expect(band.total).toBe(2)
  })

  it('counts a day’s spend from its midnight when the window begins part way through it', () => {
    const w24 = windowFor('24h', at(15), 1)
    const early = startOfDay(w24.from) + HOUR
    const data: TimelineData = { lanes: [lane], runs: [run(1, early, 4)], decisions: [], spend: null }
    const band = layoutTimeline(data, w24, METRICS).spend
    // At the left edge of the window the day has already spent what it spent.
    expect(band.points[0]?.cum).toBe(4)
  })

  it('does not count a running run until it ends', () => {
    const running: TRun = { ...run(1, at(14), 9), state: 'running', ended: undefined }
    const band = layoutTimeline({ lanes: [lane], runs: [running], decisions: [], spend: null }, w, METRICS).spend
    expect(band.steps).toEqual([])
  })
})

describe('stable identity', () => {
  it('keeps the marks, tracks and groups that did not change', () => {
    const { src, now } = busyDay()
    const w = windowFor('today', now, 1)
    const a = layoutTimeline(assemble(src), w, METRICS)
    // The same data fetched again: new objects, equal values.
    const b = stabilise(a, layoutTimeline(assemble(structuredClone(src)), w, METRICS))
    expect(b.groups).toBe(a.groups)
    expect(b.groups[0]?.lanes[0]).toBe(a.groups[0]?.lanes[0])
  })

  it('replaces only the track that changed', () => {
    const { src, now } = busyDay()
    const w = windowFor('today', now, 1)
    const a = layoutTimeline(assemble(src), w, METRICS)
    const changed = structuredClone(src)
    const run = changed.runs.find((r) => r.state === 'running')
    if (!run) throw new Error('no running run in the fixture')
    run.state = 'failed'
    run.ended = new Date(now - 60_000).toISOString()
    const b = stabilise(a, layoutTimeline(assemble(changed), w, METRICS))
    const tracksA = a.groups.flatMap((g) => g.lanes)
    const tracksB = b.groups.flatMap((g) => g.lanes)
    const moved = tracksB.filter((t, i) => t !== tracksA[i])
    expect(moved.map((t) => t.id)).toEqual([run.lane_id])
  })

  it('compares plain data deeply', () => {
    expect(same({ a: [1, { b: 2 }] }, { a: [1, { b: 2 }] })).toBe(true)
    expect(same({ a: [1] }, { a: [1, 2] })).toBe(false)
    expect(same([1], { 0: 1 })).toBe(false)
  })
})

describe('words', () => {
  const run: TRun = { id: 5, laneId: 1, agent: 'claude', state: 'running', started: at(14, 46), commits: 2, costUsd: 0.84, task: 't' }

  it('names a bar as the person would hear it', () => {
    expect(runLabel(run, 'feat/export-csv', at(15))).toBe('claude, feat/export-csv, running for 14m 0s, 2 commits, $0.84')
    expect(runPhrase({ ...run, state: 'failed', ended: at(14, 47) }, at(15))).toBe('failed after 1m 0s')
    expect(runLabel({ ...run, commits: 1, costUsd: undefined, state: 'succeeded', ended: at(14, 50) }, 'x', at(15))).toBe(
      'claude, x, succeeded in 4m 0s, 1 commit',
    )
  })

  it('gives an agent the page does not know a neutral class', () => {
    expect(agentClass('claude')).toBe('claude')
    expect(agentClass('zed')).toBe('other')
    expect(labelWidth('opencode', 16)).toBeGreaterThan(labelWidth('cursor', 16))
  })
})

describe('a run’s task', () => {
  it('is the whole prompt for a snapshot run, so the Task: marker survives', () => {
    const { src } = busyDay()
    const snap = src.runs[0]!
    const prompt = 'You are working for acme in lane x.\nTask:\nFix the thing.'
    const data = assemble({ ...src, runs: [{ ...snap, prompt }, ...src.runs.slice(1)] })
    expect(data.runs.find((r) => r.id === snap.id)?.task).toBe(prompt)
  })

  it('falls back to the history task when the snapshot prompt is empty, and to the history task alone', () => {
    const { src } = busyDay()
    const snap = src.runs[0]!
    const hist = src.historyRuns!.find((h) => h.id === snap.id)!
    const data = assemble({ ...src, runs: [{ ...snap, prompt: '' }, ...src.runs.slice(1)] })
    expect(data.runs.find((r) => r.id === snap.id)?.task).toBe(hist.task)
    const gone = assemble({ ...src, runs: src.runs.slice(1) })
    expect(gone.runs.find((r) => r.id === snap.id)?.task).toBe(hist.task)
  })
})

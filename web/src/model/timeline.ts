// The swarm timeline's model: the daemon's lists turned into positioned marks. Everything
// here is a pure function of the data, the visible window and the page's metrics (the plot's
// width and the root font size in px), so the geometry is tested here and the component only
// draws it. See docs/swarm-timeline.md.
import type { DecisionView, LaneRecord, LaneState, LaneView, MRState, RequestView, RunRecord, RunState, RunView, SpendToday } from '../api/types'
import { duration, runCost, usd } from '../format'
import type { TimelineRange } from '../router'

export const TIMELINE_RANGES: readonly { id: TimelineRange; label: string }[] = [
  { id: 'today', label: 'Today' },
  { id: '24h', label: 'Last 24 hours' },
  { id: 'week', label: 'Last 7 days' },
]

export const DEFAULT_TIMELINE_RANGE: TimelineRange = 'today'

/** What a range's spend is called: "today", "in the last 24 hours". */
export const TIMELINE_PHRASE: Record<TimelineRange, string> = { today: 'today', '24h': 'in the last 24 hours', week: 'in the last 7 days' }

const MIN = 60_000
const HOUR = 60 * MIN
const DAY = 24 * HOUR

// --- Agents -----------------------------------------------------------------------

/** The agents that have a colour of their own, in the fixed order of the legend. Colour follows the agent, never its rank. */
export const KNOWN_AGENTS = ['claude', 'opencode', 'cursor', 'copilot'] as const

/** The class suffix of an agent: its own name, or "other" for one the page does not know (a future adapter). */
export function agentClass(agent: string): string {
  return (KNOWN_AGENTS as readonly string[]).includes(agent) ? agent : 'other'
}

// --- Window and ticks -------------------------------------------------------------

export interface TimeWindow {
  /** ms since the epoch: the left edge. */
  from: number
  /** The right edge, a little past now so nothing runs off between recomputes. */
  to: number
  now: number
}

/** Local midnight of the day holding `ms`. */
export function startOfDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** Headroom to the right of now, as a share of the visible span. */
export const HEADROOM = 0.02

/** The shortest span Today shows, so the first minutes of a day are not one enormous bar. */
export const MIN_TODAY_SPAN = HOUR

/**
 * The visible window of a range at `now`. Today runs from local midnight, so a day with a
 * daylight-saving change is 23 or 25 hours long; the other two are 24 hours and 7 days back.
 * The right edge is rounded up to `quantum` ms (a couple of pixels' worth), so marks keep
 * their positions between recomputes and a new window is a small jump rather than a drift.
 */
export function windowFor(range: TimelineRange, now: number, quantum = MIN): TimeWindow {
  const q = Math.max(1, quantum)
  if (range === 'today') {
    const from = startOfDay(now)
    const span = Math.max(now - from, MIN_TODAY_SPAN)
    return { from, to: Math.ceil((from + span + Math.max(0, now - from) * HEADROOM) / q) * q, now }
  }
  const span = range === '24h' ? DAY : 7 * DAY
  const to = Math.ceil((now + span * HEADROOM) / q) * q
  return { from: to - Math.round(span * (1 + HEADROOM)), to, now }
}

/**
 * The right edge's rounding for a range at a plot width: about two pixels of time, but not
 * under a minute, so a bar does not move between the recomputes that the page makes.
 */
export function quantumFor(range: TimelineRange, now: number, width: number): number {
  const span = range === 'today' ? Math.max(now - startOfDay(now), MIN_TODAY_SPAN) : range === '24h' ? DAY : 7 * DAY
  return Math.max(MIN, Math.round((span / Math.max(1, width)) * 2))
}

/** How far back the history must be read to draw a window: a run that began before it can still be in it, and a day's spend counts from midnight. */
export const LOOKBACK = 6 * HOUR

export function fetchSince(from: number): number {
  return Math.min(startOfDay(from), from - LOOKBACK)
}

export interface Tick {
  at: number
  label: string
  /** A day boundary, named ("Mon 6 Oct") rather than timed. */
  major: boolean
}

const STEPS = [5 * MIN, 15 * MIN, 30 * MIN, HOUR, 3 * HOUR, 6 * HOUR, 12 * HOUR, DAY]

const pad2 = (n: number) => String(n).padStart(2, '0')
const hm = (d: Date) => `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

/** "Mon 6 Oct". */
export function dayName(ms: number): string {
  const d = new Date(ms)
  return `${WEEKDAYS[d.getDay()]} ${d.getDate()} ${MONTHS[d.getMonth()]}`
}

/** "14:05". */
export function timeOfDay(ms: number): string {
  return hm(new Date(ms))
}

/**
 * Axis ticks at local-time boundaries: the smallest of 5, 15, 30 minutes, 1, 3, 6, 12 hours
 * and a day that leaves no more than `maxTicks` in the window. Steps of three hours or more
 * are built from wall-clock times, so a daylight-saving change does not shift them.
 */
export function ticks(w: TimeWindow, maxTicks: number): Tick[] {
  const span = w.to - w.from
  const step = STEPS.find((s) => span / s <= Math.max(1, maxTicks)) ?? DAY
  const out: Tick[] = []
  const push = (at: number) => {
    if (at < w.from || at > w.to) return
    const d = new Date(at)
    const midnight = d.getHours() === 0 && d.getMinutes() === 0
    out.push({ at, major: midnight, label: midnight ? dayName(at) : hm(d) })
  }
  if (step >= 3 * HOUR) {
    const hours = step / HOUR
    for (let day = new Date(startOfDay(w.from)); day.getTime() <= w.to; day.setDate(day.getDate() + 1)) {
      if (step === DAY) {
        push(day.getTime())
        continue
      }
      for (let h = 0; h < 24; h += hours) push(new Date(day.getFullYear(), day.getMonth(), day.getDate(), h).getTime())
    }
  } else {
    // Sub-hour and hour steps: from the first local boundary, in absolute steps.
    const d = new Date(w.from)
    d.setSeconds(0, 0)
    let at = d.getTime()
    const offsetMin = d.getMinutes()
    const stepMin = step / MIN
    at += ((stepMin - (offsetMin % stepMin)) % stepMin) * MIN
    if (at < w.from) at += step
    for (; at <= w.to; at += step) push(at)
  }
  return out
}

// --- Input ------------------------------------------------------------------------

export interface TLane {
  id: number
  repo: string
  name: string
  state: LaneState
  created: number
  closed?: number
  mr?: number
  mrState?: MRState
  mrUrl?: string
  /** When the merge request was merged, as of the daemon's last poll with the forge. */
  mergedAt?: number
}

export interface TRun {
  id: number
  laneId: number
  agent: string
  model?: string
  state: RunState
  started: number
  ended?: number
  commits: number
  costUsd?: number
  credits?: number
  /** costUsd is the agent's own estimate, not a bill. */
  costEstimated?: boolean
  /** The whole prompt while the run is in the snapshot (the UI finds the Task: marker in it); the one-line history task once it has left. */
  task: string
  /** What the run is doing now, while it runs. */
  progress?: string
  /** Why a failed run failed, when the daemon has worked it out. */
  diagnosis?: string
}

export interface TDecision {
  /** A decision, or a request addressed to the person (needs_routing). */
  source: 'decision' | 'request'
  id: number
  runId: number
  laneId: number
  agent: string
  created: number
  answered?: number
  question: string
  recommendation?: string
}

export interface TimelineData {
  lanes: TLane[]
  runs: TRun[]
  decisions: TDecision[]
  spend: { day: string; usd: number; budget: number } | null
}

/** The daemon's lists as the page holds them. */
export interface Sources {
  /** The snapshot's open lanes. */
  lanes: LaneView[]
  /** Closed lanes from the history since the fetch start. */
  closedLanes?: LaneRecord[]
  /** The snapshot's newest runs: fresh, and the only ones with progress. */
  runs: RunView[]
  /** Every run from the history since the fetch start. */
  historyRuns?: RunRecord[]
  /** Open decisions. */
  decisions: DecisionView[]
  answered?: DecisionView[]
  /** Requests that need routing. */
  requests: RequestView[]
  spend: SpendToday | null
}

const ms = (iso: string | undefined): number | undefined => {
  if (!iso) return undefined
  const t = Date.parse(iso)
  return Number.isNaN(t) ? undefined : t
}

const firstLine = (s: string) => (s.split('\n').find((l) => l.trim() !== '') ?? '').trim()

function laneOf(l: LaneView | LaneRecord): TLane {
  const outcome = 'outcome' in l ? l.outcome : undefined
  const closed = ms(l.closed)
  const merged = l.mr_state === 'merged' || outcome === 'merged'
  return {
    id: l.id, repo: l.repo, name: l.name, state: l.state, created: ms(l.created) ?? 0,
    ...(closed !== undefined ? { closed } : {}),
    ...(l.mr ? { mr: l.mr } : {}),
    ...(l.mr_state ? { mrState: l.mr_state } : {}),
    ...(l.mr_url ? { mrUrl: l.mr_url } : {}),
    ...(merged ? { mergedAt: ms(l.mr_updated) ?? closed } : {}),
  }
}

/**
 * One data set from the snapshot's lists and the history reads. A run in both is the
 * snapshot's (it is fresher); a lane the lists do not hold is made from what its runs say.
 */
export function assemble(src: Sources): TimelineData {
  const lanes = new Map<number, TLane>()
  for (const l of src.closedLanes ?? []) lanes.set(l.id, laneOf(l))
  for (const l of src.lanes) lanes.set(l.id, laneOf(l))

  const runs = new Map<number, TRun>()
  const stub = (id: number, repo: string, name: string, state: LaneState, at: number) => {
    if (!lanes.has(id)) lanes.set(id, { id, repo, name, state, created: at })
  }
  for (const r of src.historyRuns ?? []) {
    const started = ms(r.started) ?? 0
    stub(r.lane_id, r.repo, r.lane, r.lane_state, started)
    runs.set(r.id, {
      id: r.id, laneId: r.lane_id, agent: r.agent, ...(r.model ? { model: r.model } : {}), state: r.state, started,
      ...(r.ended ? { ended: ms(r.ended) } : {}), commits: r.commits,
      ...(r.cost_usd !== undefined ? { costUsd: r.cost_usd } : {}), ...(r.credits ? { credits: r.credits } : {}), ...(r.cost_estimated ? { costEstimated: true } : {}), task: r.task,
    })
  }
  for (const r of src.runs) {
    const started = ms(r.started) ?? 0
    stub(r.lane_id, r.repo, r.lane, 'open', started)
    runs.set(r.id, {
      id: r.id, laneId: r.lane_id, agent: r.agent, ...(r.model ? { model: r.model } : {}), state: r.state, started,
      ...(r.ended ? { ended: ms(r.ended) } : {}), commits: r.commits,
      ...(r.cost_usd !== undefined ? { costUsd: r.cost_usd } : {}), ...(r.credits ? { credits: r.credits } : {}), ...(r.cost_estimated ? { costEstimated: true } : {}),
      task: (r.prompt?.trim() ? r.prompt : runs.get(r.id)?.task) || '', ...(r.progress ? { progress: r.progress.line } : {}),
      ...(r.diagnosis ? { diagnosis: r.diagnosis.summary } : {}),
    })
  }

  // A decision's lane: through its run when the run is known, else by the repo and name the
  // daemon gave it (names repeat across repos, so both).
  const byName = new Map<string, number>()
  for (const l of lanes.values()) byName.set(`${l.repo}\u0000${l.name}`, l.id)
  const laneFor = (runId: number, repo: string, name: string) => runs.get(runId)?.laneId ?? byName.get(`${repo}\u0000${name}`)

  const decisions: TDecision[] = []
  for (const d of [...src.decisions, ...(src.answered ?? [])]) {
    const laneId = laneFor(d.run_id, d.repo, d.lane)
    const created = ms(d.created)
    if (laneId === undefined || created === undefined) continue
    decisions.push({
      source: 'decision', id: d.id, runId: d.run_id, laneId, agent: d.agent, created,
      ...(d.state === 'answered' ? { answered: ms(d.answered) ?? created } : {}),
      question: d.question, ...(d.recommendation ? { recommendation: d.recommendation } : {}),
    })
  }
  for (const q of src.requests) {
    const laneId = laneFor(q.from_run, q.repo, q.from_lane)
    const created = ms(q.created)
    if (laneId === undefined || created === undefined) continue
    decisions.push({
      source: 'request', id: q.id, runId: q.from_run, laneId, agent: q.from_agent, created,
      question: firstLine(q.message),
    })
  }

  return {
    lanes: [...lanes.values()],
    runs: [...runs.values()],
    decisions,
    spend: src.spend ? { day: src.spend.day, usd: src.spend.usd, budget: src.spend.budget } : null,
  }
}

// --- Marks ------------------------------------------------------------------------

/** What the geometry is measured in: the plot's width and the root font size, both in px. */
export interface Metrics {
  width: number
  rem: number
}

export const ROW_REM = 1.75
export const BAR_REM = 0.9
export const SPEND_REM = 3.5
/** A bar narrower than this is merged with its neighbours into a density tick. */
export const MIN_BAR_PX = 2
/** The pulsing cap of a running bar. */
export const CAP_REM = 0.5

export interface BarMark {
  kind: 'bar'
  key: string
  run: TRun
  x: number
  w: number
  running: boolean
  /** Failed or interrupted: drawn with a notch. */
  failed: boolean
  /** Began before the window: drawn from its left edge. */
  continuing: boolean
  /** The bar is wide enough to carry the agent's name. */
  labelled: boolean
}

/** Runs too narrow to see one by one on a track. */
export interface DensityMark {
  kind: 'density'
  key: string
  x: number
  w: number
  runs: TRun[]
  failed: number
}

export interface DecisionMark {
  key: string
  source: 'decision' | 'request'
  id: number
  runId: number
  agent: string
  x: number
  created: number
  answered?: number
  open: boolean
  /** Asked before the window began: held at its left edge. */
  continuing: boolean
  /** Where the asking run's bar ends, when that is to the right of the mark: the line that joins them. */
  joinTo?: number
  question: string
  recommendation?: string
}

export interface MergeMark {
  key: string
  x: number
  at: number
  mr?: number
  mrUrl?: string
}

export interface LaneTrack {
  key: string
  id: number
  repo: string
  name: string
  state: LaneState
  mr?: number
  running: boolean
  waiting: boolean
  bars: (BarMark | DensityMark)[]
  decisions: DecisionMark[]
  merges: MergeMark[]
}

export interface RepoGroup {
  repo: string
  lanes: LaneTrack[]
}

export interface SpendStep {
  at: number
  usd: number
  /** A run's own cost, or what the day's total holds beyond the runs' (the front desk, untimed spend). */
  source: 'run' | 'rest'
  run?: number
}

export interface SpendBand {
  /** The cumulative spend of the day after each point: a step function that falls back to 0 at each midnight. */
  points: { at: number; cum: number; step?: SpendStep }[]
  steps: SpendStep[]
  budget: number
  /** The top of the scale, in dollars. */
  max: number
  height: number
  budgetY: number
  line: string
  fill: string
  /** The part above the budget line, as path data; "" while under. */
  over: string
  overBudget: boolean
  /** The cumulative spend today at now. */
  total: number
}

export interface Summary {
  running: number
  waiting: number
  merged: number
  /** Open lanes with nothing in the window, left out unless asked for. */
  idle: number
  spent: number | null
  budget: number
}

export interface TimelineLayout {
  window: TimeWindow
  metrics: Metrics
  groups: RepoGroup[]
  spend: SpendBand
  summary: Summary
  /** The x of now, in px. */
  nowX: number
}

/** A position in the window, in px, clamped to the plot. */
export function xOf(w: TimeWindow, width: number, t: number): number {
  const x = ((t - w.from) / (w.to - w.from)) * width
  return Math.min(width, Math.max(0, x))
}

/** The time at an x in px. */
export function timeAt(w: TimeWindow, width: number, x: number): number {
  return w.from + (x / width) * (w.to - w.from)
}

const round = (n: number) => Math.round(n * 10) / 10

const FAILED: readonly RunState[] = ['failed', 'interrupted']

/** Whether a run is in the window: it began before the right edge and had not ended before the left. */
export function overlaps(r: TRun, w: TimeWindow): boolean {
  if (r.started >= w.to) return false
  const end = runEnd(r, w.now)
  return end >= w.from
}

/** The end of a run: its end, now while it runs, else its start (a run the daemon never saw end). */
export function runEnd(r: TRun, now: number): number {
  return r.ended ?? (r.state === 'running' ? now : r.started)
}

/** The width an agent's name needs at the label size, in px; a close estimate, since no layout is available. */
export function labelWidth(agent: string, rem: number): number {
  return Math.ceil(agent.length * 0.6 * 0.75 * rem) + 0.7 * rem
}

function barFor(r: TRun, w: TimeWindow, m: Metrics): BarMark {
  const running = r.state === 'running'
  const start = Math.max(r.started, w.from)
  const x = xOf(w, m.width, start)
  const end = running ? w.now : Math.min(runEnd(r, w.now), w.to)
  const raw = xOf(w, m.width, end) - x
  const width = running ? Math.max(raw, CAP_REM * m.rem * 0.5) : Math.max(0, raw)
  return {
    kind: 'bar', key: `run:${r.id}`, run: r, x: round(x), w: round(width), running,
    failed: FAILED.includes(r.state), continuing: r.started < w.from,
    labelled: agentClass(r.agent) === 'other' || width >= labelWidth(r.agent, m.rem),
  }
}

/**
 * Merges runs too narrow to see into density ticks. Runs closer than the minimum width and
 * each narrower than it become one tick; a running bar never merges, and a lone narrow run
 * stays itself, drawn at the minimum width.
 */
export function levelOfDetail(bars: BarMark[], minW = MIN_BAR_PX): (BarMark | DensityMark)[] {
  const out: (BarMark | DensityMark)[] = []
  let group: BarMark[] = []
  const flush = () => {
    if (group.length === 0) return
    const first = group[0] as BarMark
    const last = group[group.length - 1] as BarMark
    if (group.length === 1) {
      out.push(first.w < minW ? { ...first, w: minW, labelled: false } : first)
    } else {
      out.push({
        kind: 'density', key: `density:${first.run.id}`, x: first.x, w: Math.max(minW, round(last.x + last.w - first.x)),
        runs: group.map((b) => b.run), failed: group.filter((b) => b.failed).length,
      })
    }
    group = []
  }
  for (const b of [...bars].sort((a, c) => a.x - c.x || a.run.id - c.run.id)) {
    const narrow = b.w < minW && !b.running
    const prev = group[group.length - 1]
    if (narrow && (!prev || b.x - (prev.x + prev.w) < minW)) {
      group.push(b)
      continue
    }
    flush()
    if (narrow) group.push(b)
    else out.push(b)
  }
  flush()
  return out
}

function spendBand(data: TimelineData, w: TimeWindow, m: Metrics): SpendBand {
  const height = SPEND_REM * m.rem
  const dayStart = startOfDay(w.now)
  const steps: SpendStep[] = []
  for (const r of data.runs) {
    if (r.ended === undefined || !r.costUsd || r.costUsd <= 0) continue
    steps.push({ at: r.ended, usd: r.costUsd, source: 'run', run: r.id })
  }
  // The day's total from the daemon holds the front desk's turns and spend with no time
  // of its own: whatever the runs do not account for is placed at now.
  if (data.spend && data.spend.day === localDay(w.now)) {
    const inRuns = steps.filter((s) => s.at >= dayStart).reduce((n, s) => n + s.usd, 0)
    const rest = data.spend.usd - inRuns
    if (rest > 0.005) steps.push({ at: w.now, usd: rest, source: 'rest' })
  }
  steps.sort((a, b) => a.at - b.at || (a.run ?? 0) - (b.run ?? 0))

  // The curve: what is spent in the day so far, falling back to nothing at each midnight.
  const events: { at: number; step?: SpendStep; reset?: boolean }[] = steps.map((step) => ({ at: step.at, step }))
  for (let d = startOfDay(w.from); d <= w.now; d = nextDay(d)) events.push({ at: d, reset: true })
  events.sort((a, b) => a.at - b.at || (a.reset ? -1 : 0) - (b.reset ? -1 : 0))
  let cum = 0
  const after: SpendBand['points'] = []
  for (const e of events) {
    cum = e.reset ? 0 : cum + (e.step?.usd ?? 0)
    after.push({ at: e.at, cum, ...(e.step ? { step: e.step } : {}) })
  }
  const before = after.filter((p) => p.at < w.from).at(-1)
  const points: SpendBand['points'] = [{ at: w.from, cum: before?.cum ?? 0 }, ...after.filter((p) => p.at >= w.from)]

  const budget = data.spend?.budget ?? 0
  // Room above the budget line for its label.
  const top = Math.max(...points.map((p) => p.cum), budget, 1)
  const max = top * (budget > 0 ? 1.45 : 1.1)
  const yOf = (v: number) => round(height - 2 - (v / max) * (height - 6))
  const x = (t: number) => round(xOf(w, m.width, t))
  const nowX = x(w.now)
  const base = round(height - 2)

  let line = ''
  let fill = ''
  let over = ''
  points.forEach((p, i) => {
    const x0 = x(p.at)
    const y = yOf(p.cum)
    line += i === 0 ? `M${x0} ${y}` : `H${x0}V${y}`
    fill += i === 0 ? `M${x0} ${base}V${y}` : `H${x0}V${y}`
    const x1 = i + 1 < points.length ? x((points[i + 1] as SpendBand['points'][number]).at) : nowX
    if (budget > 0 && p.cum > budget && x1 > x0) over += `M${x0} ${yOf(budget)}H${x1}V${y}H${x0}Z`
  })
  line += `H${nowX}`
  fill += `H${nowX}V${base}Z`

  const total = (points[points.length - 1] as SpendBand['points'][number]).cum
  return {
    points, steps: steps.filter((s) => s.at >= w.from), budget, max, height, budgetY: budget > 0 ? yOf(budget) : 0,
    line, fill, over, overBudget: budget > 0 && total > budget, total,
  }
}

function nextDay(ms: number): number {
  const d = new Date(ms)
  d.setDate(d.getDate() + 1)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** The local date as the daemon names a spend day: "2026-10-10". */
export function localDay(ms: number): string {
  const d = new Date(ms)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}

/** What a day has spent at a time, and the step that last moved it. */
export function spendAt(band: SpendBand, t: number): { cum: number; step?: SpendStep } {
  let at: SpendBand['points'][number] | undefined
  for (const p of band.points) {
    if (p.at > t) break
    at = p
  }
  const step = [...band.steps].reverse().find((s) => s.at <= t)
  return { cum: at?.cum ?? 0, ...(step ? { step } : {}) }
}

export interface LayoutOptions {
  /** Include the open lanes that have nothing in the window. */
  showIdle?: boolean
}

/** What the window holds, by lane: its runs, and the decisions that touch it. */
function inWindow(data: TimelineData, w: TimeWindow) {
  const runs = new Map<number, TRun[]>()
  for (const r of data.runs) {
    if (!overlaps(r, w)) continue
    const list = runs.get(r.laneId) ?? []
    list.push(r)
    runs.set(r.laneId, list)
  }
  const decisions = new Map<number, TDecision[]>()
  for (const d of data.decisions) {
    const open = d.answered === undefined
    // A waiting decision is held at the left edge when it was asked before the window; an
    // answered one is drawn only where it was asked.
    if (!(open ? d.created < w.to : d.created < w.to && d.created >= w.from)) continue
    const list = decisions.get(d.laneId) ?? []
    list.push(d)
    decisions.set(d.laneId, list)
  }
  return { runs, decisions }
}

/** Whether a lane has anything in the window: a run, a decision, a merge, its opening or its close. */
function touched(lane: TLane, runs: TRun[], asks: TDecision[], w: TimeWindow): boolean {
  const within = (t: number | undefined) => t !== undefined && t >= w.from && t < w.to
  return runs.length > 0 || asks.length > 0 || within(lane.mergedAt) || within(lane.created) || within(lane.closed)
}

/** The counts of the page's summary line, and how many open lanes the window leaves idle. */
export function summarise(data: TimelineData, w: TimeWindow): Summary {
  const { runs, decisions } = inWindow(data, w)
  let idle = 0
  let merged = 0
  for (const lane of data.lanes) {
    if (lane.mergedAt !== undefined && lane.mergedAt >= w.from && lane.mergedAt < w.to) merged++
    if (lane.state !== 'closed' && !touched(lane, runs.get(lane.id) ?? [], decisions.get(lane.id) ?? [], w)) idle++
  }
  return {
    running: data.runs.filter((r) => r.state === 'running').length,
    waiting: data.decisions.filter((d) => d.answered === undefined).length,
    merged, idle, spent: data.spend ? data.spend.usd : null, budget: data.spend?.budget ?? 0,
  }
}

/** The whole picture: groups of lane tracks with their marks, the spend band, the summary. */
export function layoutTimeline(data: TimelineData, w: TimeWindow, m: Metrics, o: LayoutOptions = {}): TimelineLayout {
  const { runs: runsByLane, decisions: decisionsByLane } = inWindow(data, w)
  const runById = new Map(data.runs.map((r) => [r.id, r]))

  const groups = new Map<string, { first: number; id: number; track: LaneTrack }[]>()
  for (const lane of data.lanes) {
    const runs = runsByLane.get(lane.id) ?? []
    const asks = decisionsByLane.get(lane.id) ?? []
    const mergedIn = lane.mergedAt !== undefined && lane.mergedAt >= w.from && lane.mergedAt < w.to
    if (!touched(lane, runs, asks, w) && (lane.state === 'closed' || !o.showIdle)) continue

    const bars = levelOfDetail(runs.map((r) => barFor(r, w, m)))
    const decisions: DecisionMark[] = asks.map((d) => {
      const run = runById.get(d.runId)
      const x = xOf(w, m.width, d.created)
      const end = run && run.ended !== undefined ? xOf(w, m.width, Math.min(run.ended, w.to)) : undefined
      return {
        key: `${d.source}:${d.id}`, source: d.source, id: d.id, runId: d.runId, agent: d.agent, x: round(x),
        created: d.created, ...(d.answered !== undefined ? { answered: d.answered } : {}), open: d.answered === undefined,
        continuing: d.created < w.from,
        ...(end !== undefined && end - x > 3 ? { joinTo: round(end) } : {}),
        question: d.question, ...(d.recommendation ? { recommendation: d.recommendation } : {}),
      }
    })
    const merges: MergeMark[] =
      mergedIn && lane.mergedAt !== undefined
        ? [{
            key: `merge:${lane.id}`, x: round(xOf(w, m.width, lane.mergedAt)), at: lane.mergedAt,
            ...(lane.mr ? { mr: lane.mr } : {}), ...(lane.mrUrl ? { mrUrl: lane.mrUrl } : {}),
          }]
        : []
    const track: LaneTrack = {
      key: `lane:${lane.id}`, id: lane.id, repo: lane.repo, name: lane.name, state: lane.state,
      ...(lane.mr ? { mr: lane.mr } : {}),
      running: runs.some((r) => r.state === 'running'), waiting: decisions.some((d) => d.open), bars, decisions, merges,
    }
    const list = groups.get(lane.repo) ?? []
    list.push({ id: lane.id, first: runs.length ? Math.min(...runs.map((r) => r.started)) : lane.created, track })
    groups.set(lane.repo, list)
  }

  const repos: RepoGroup[] = [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([repo, list]) => ({
      repo,
      lanes: list.sort((a, b) => a.first - b.first || a.id - b.id).map((e) => e.track),
    }))

  return {
    window: w, metrics: m, groups: repos, spend: spendBand(data, w, m), summary: summarise(data, w),
    nowX: round(xOf(w, m.width, w.now)),
  }
}

/** Equal plain data: numbers, strings, booleans, arrays and objects of them. */
export function same(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'object' || typeof b !== 'object' || a === null || b === null) return false
  if (Array.isArray(a) !== Array.isArray(b)) return false
  const ka = Object.keys(a)
  if (ka.length !== Object.keys(b).length) return false
  return ka.every((k) => same((a as Record<string, unknown>)[k], (b as Record<string, unknown>)[k]))
}

/**
 * Gives every mark, track and group of `next` the identity it had in `prev` when nothing
 * about it changed, so memoised components skip what did not move.
 */
export function stabilise(prev: TimelineLayout | null, next: TimelineLayout): TimelineLayout {
  if (!prev) return next
  const reuse = <T,>(old: T | undefined, now: T): T => (old !== undefined && same(old, now) ? old : now)
  const tracks = new Map<string, LaneTrack>()
  for (const g of prev.groups) for (const t of g.lanes) tracks.set(t.key, t)
  const groups = next.groups.map((g) => {
    const lanes = g.lanes.map((t) => {
      const old = tracks.get(t.key)
      if (!old) return t
      const bars = t.bars.map((b) => reuse(old.bars.find((o) => o.key === b.key), b))
      const decisions = t.decisions.map((d) => reuse(old.decisions.find((o) => o.key === d.key), d))
      const merges = t.merges.map((x) => reuse(old.merges.find((o) => o.key === x.key), x))
      const sameList = <T,>(a: T[], b: T[]) => a.length === b.length && a.every((v, i) => v === b[i])
      const unchanged =
        sameList(bars, old.bars) && sameList(decisions, old.decisions) && sameList(merges, old.merges) &&
        old.name === t.name && old.state === t.state && old.mr === t.mr && old.running === t.running && old.waiting === t.waiting
      return unchanged ? old : { ...t, bars, decisions, merges }
    })
    const oldGroup = prev.groups.find((o) => o.repo === g.repo)
    return oldGroup && oldGroup.lanes.length === lanes.length && oldGroup.lanes.every((l, i) => l === lanes[i]) ? oldGroup : { ...g, lanes }
  })
  const unchanged = groups.length === prev.groups.length && groups.every((g, i) => g === prev.groups[i])
  return { ...next, groups: unchanged ? prev.groups : groups }
}

/** The agents to put in the legend: the four known ones, then any other the data holds. */
export function agentsIn(data: TimelineData): string[] {
  const extra = [...new Set(data.runs.map((r) => r.agent))].filter((a) => agentClass(a) === 'other').sort()
  return [...KNOWN_AGENTS, ...extra]
}

// --- Words ------------------------------------------------------------------------

/** "running for 14m 03s", "succeeded in 4m 12s", "failed after 38s". */
export function runPhrase(r: TRun, now: number): string {
  const took = duration(new Date(r.started).toISOString(), r.ended !== undefined ? new Date(r.ended).toISOString() : undefined, now)
  if (r.state === 'running') return `running for ${took}`
  const verb: Record<RunState, string> = {
    running: 'running', succeeded: 'succeeded in', failed: 'failed after', interrupted: 'interrupted after', stopped: 'stopped after',
  }
  return `${verb[r.state]} ${took}`
}

/** A bar's accessible name: "claude, feat/export-csv, running for 14m 03s, 2 commits, $0.84". */
export function runLabel(r: TRun, lane: string, now: number): string {
  const parts = [r.agent, lane, runPhrase(r, now), `${r.commits} ${r.commits === 1 ? 'commit' : 'commits'}`]
  const cost = runCost({ cost_usd: r.costUsd, credits: r.credits, cost_estimated: r.costEstimated, agent: r.agent })
  if (cost) parts.push(cost)
  return parts.join(', ')
}

/** The page's one line under its heading: "3 running, 1 waiting on you, 2 merged, $14.20 of $25.00 today". */
export function summaryText(s: Summary, range: TimelineRange): string {
  const parts: string[] = []
  if (s.running > 0) parts.push(`${s.running} running`)
  if (s.waiting > 0) parts.push(`${s.waiting} waiting on you`)
  if (s.merged > 0) parts.push(`${s.merged} merged`)
  if (s.spent !== null && range === 'today') {
    parts.push(s.budget > 0 ? `${usd(s.spent)} of ${usd(s.budget)} today` : `${usd(s.spent)} today`)
  }
  return parts.join(', ')
}

/** Every run in the window, newest first, with its lane's name: the table view's rows. */
export function tableRows(data: TimelineData, w: TimeWindow): { run: TRun; repo: string; lane: string }[] {
  const lanes = new Map(data.lanes.map((l) => [l.id, l]))
  return data.runs
    .filter((r) => overlaps(r, w))
    .sort((a, b) => b.started - a.started || b.id - a.id)
    .map((run) => ({ run, repo: lanes.get(run.laneId)?.repo ?? '', lane: lanes.get(run.laneId)?.name ?? `lane ${run.laneId}` }))
}

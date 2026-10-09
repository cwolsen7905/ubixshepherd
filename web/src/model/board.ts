// The board groups lanes by what they need from the person, with the same rules as the
// terminal's dock (internal/chat/dock.go): built from the daemon's lists on every change,
// never from the feed, since a feed item does not say whether a run succeeded.
import type { DecisionView, LaneView, RequestView, RunView } from '../api/types'

export type Group = 'needs' | 'broken' | 'review' | 'working' | 'done'

/** Most urgent first, as the dock orders them. */
export const GROUPS: readonly Group[] = ['needs', 'broken', 'review', 'working', 'done']

export const GROUP_NAMES: Record<Group, string> = {
  needs: 'needs you',
  broken: 'broken',
  review: 'to review',
  working: 'working',
  done: 'done unseen',
}

export interface BoardItem {
  key: string
  group: Group
  /** Overrides the group's glyph, as for a request that needs routing. */
  glyph?: string
  /** As shown: repo:lane when two repos use the lane's name. */
  name: string
  laneId?: number
  repo: string
  agent?: string
  /** For the MR badge; a lane with no merge request has no mr. */
  lane?: LaneView
  run?: RunView
  decision?: DecisionView
  request?: RequestView
  detail: string
  /** Orders items in a group: waiting longest first, or for done, the latest first. */
  at: string
}

/** What the person has seen: finished before seenUntil, or opened by key ("run:12"). */
export interface Seen {
  until: string
  keys: ReadonlySet<string>
}

export interface BoardInput {
  lanes: LaneView[]
  runs: RunView[]
  decisions: DecisionView[]
  requests: RequestView[]
  seen: Seen
}

const t = (s: string) => Date.parse(s)

export function unseen(seen: Seen, key: string, at: string): boolean {
  return t(at) > t(seen.until) && !seen.keys.has(key)
}

/** The lane names that more than one repo uses. */
export function laneClashes(lanes: LaneView[]): Set<string> {
  const repos = new Map<string, string>()
  const clash = new Set<string>()
  for (const l of lanes) {
    const r = repos.get(l.name)
    if (r !== undefined && r !== l.repo) clash.add(l.name)
    repos.set(l.name, l.repo)
  }
  return clash
}

/** The latest run of each lane, by id. */
export function latestRuns(runs: RunView[]): Map<number, RunView> {
  const latest = new Map<number, RunView>()
  for (const r of runs) {
    const l = latest.get(r.lane_id)
    if (!l || r.id > l.id) latest.set(r.lane_id, r)
  }
  return latest
}

export function mrOpen(l: LaneView): boolean {
  return !!l.mr && l.mr_state !== 'merged' && l.mr_state !== 'closed'
}

export function boardItems({ lanes, runs, decisions, requests, seen }: BoardInput): BoardItem[] {
  const clash = laneClashes(lanes)
  const name = (repo: string, lane: string) => (clash.has(lane) && repo ? `${repo}:${lane}` : lane)
  const laneByName = (repo: string, lane: string) => lanes.find((l) => l.name === lane && l.repo === repo)
  const items: BoardItem[] = []

  for (const d of decisions) {
    items.push({
      key: `decision:${d.id}`, group: 'needs', name: name(d.repo, d.lane), laneId: laneByName(d.repo, d.lane)?.id,
      repo: d.repo, agent: d.agent, decision: d, at: d.created, detail: `decision ${d.id}: ${oneLine(d.question)}`,
    })
  }
  for (const q of requests) {
    items.push({
      key: `request:${q.id}`, group: 'needs', glyph: '!', name: name(q.repo, q.from_lane),
      laneId: laneByName(q.repo, q.from_lane)?.id, repo: q.repo, agent: q.from_agent, request: q, at: q.created,
      detail: `request ${q.id} needs routing: ${oneLine(q.message)}`,
    })
  }

  const latest = latestRuns(runs)
  for (const l of lanes) {
    const run = latest.get(l.id)
    const base = { key: `lane:${l.id}`, name: name(l.repo, l.name), laneId: l.id, repo: l.repo, agent: run?.agent, lane: l, run }
    const running = run?.state === 'running'
    const failedRun = run?.state === 'failed' || run?.state === 'interrupted'
    const failedPipeline = mrOpen(l) && l.pipeline_status === 'failed'

    if (l.state === 'closed') {
      if (l.mr_state !== 'merged' || !l.closed || !unseen(seen, `lane:${l.id}`, l.closed)) continue
      items.push({ ...base, group: 'done', at: l.closed, detail: 'merged' })
    } else if (failedRun || failedPipeline) {
      let detail = failedRun ? `run ${run.id} ${run.state}` : 'pipeline failed'
      if (running) detail += ', fixing'
      items.push({ ...base, group: 'broken', at: failedRun ? endedAt(run) : l.created, detail })
    } else if (running) {
      items.push({ ...base, group: 'working', at: run.started, detail: `run ${run.id}` })
    } else if (mrOpen(l) && l.pipeline_status === 'passed') {
      items.push({ ...base, group: 'review', at: l.created, detail: 'waiting for a merge' })
    } else if (run?.ended && unseen(seen, `run:${run.id}`, run.ended)) {
      items.push({ ...base, group: 'done', at: run.ended, detail: `run ${run.id} ${run.state}` })
    }
  }

  const order = (g: Group) => GROUPS.indexOf(g)
  return items.sort((a, b) => {
    if (a.group !== b.group) return order(a.group) - order(b.group)
    return a.group === 'done' ? t(b.at) - t(a.at) : t(a.at) - t(b.at)
  })
}

export function counts(items: BoardItem[]): Record<Group, number> {
  const n: Record<Group, number> = { needs: 0, broken: 0, review: 0, working: 0, done: 0 }
  for (const it of items) n[it.group]++
  return n
}

/** The groups that hold anything, most urgent first: "1 needs you · 2 broken". */
export function countsText(n: Record<Group, number>): string {
  return GROUPS.filter((g) => n[g] > 0).map((g) => `${n[g]} ${GROUP_NAMES[g]}`).join(' · ')
}

/**
 * Lets the board show a run's end as soon as the feed reports it. The runs list stays the
 * source of truth: the next poll replaces this.
 */
export function noteOutcome(runs: RunView[], event: string, ref: number | undefined, created: string): RunView[] {
  const state = ({ run_passed: 'succeeded', run_failed: 'failed', run_interrupted: 'interrupted' } as const)[
    event as 'run_passed' | 'run_failed' | 'run_interrupted'
  ]
  if (!state || !ref) return runs
  let changed = false
  const out = runs.map((r) => {
    if (r.id !== ref || r.state !== 'running') return r
    changed = true
    return { ...r, state, ended: created }
  })
  return changed ? out : runs
}

function endedAt(r: RunView): string {
  return r.ended ?? r.started
}

export function oneLine(s: string): string {
  return s.split(/\s+/).filter(Boolean).join(' ')
}

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
  /** Also list runs the person dismissed, marked as such; off by default. */
  showDismissed?: boolean
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

const SHOW_DISMISSED_KEY = 'shepherd.showDismissed'

export function readShowDismissed(store: Pick<Storage, 'getItem'>): boolean {
  try {
    return store.getItem(SHOW_DISMISSED_KEY) === '1'
  } catch {
    return false
  }
}

export function writeShowDismissed(store: Pick<Storage, 'setItem' | 'removeItem'>, on: boolean) {
  try {
    if (on) store.setItem(SHOW_DISMISSED_KEY, '1')
    else store.removeItem(SHOW_DISMISSED_KEY)
  } catch {
    // no storage: the choice lasts until the page reloads
  }
}

/** A run that ended without succeeding: the ones the person may dismiss. */
export function endedBadly(r: RunView | undefined): r is RunView {
  return r?.state === 'failed' || r?.state === 'interrupted' || r?.state === 'stopped'
}

/** How many open lanes' latest runs are dismissed, hidden unless `showDismissed`. */
export function dismissedCount(lanes: LaneView[], runs: RunView[]): number {
  const latest = latestRuns(runs)
  return lanes.filter((l) => {
    const r = latest.get(l.id)
    return l.state !== 'closed' && endedBadly(r) && !!r.dismissed
  }).length
}

/** Whether the board offers Dismiss on this item: its latest run failed or was stopped, and is not yet dismissed. */
export function canDismiss(it: BoardItem): boolean {
  return it.group !== 'working' && endedBadly(it.run) && !it.run.dismissed
}

export function mrOpen(l: LaneView): boolean {
  return !!l.mr && l.mr_state !== 'merged' && l.mr_state !== 'closed'
}

/**
 * Whether the forge is still working on the open merge request's pipeline, so the lane is
 * waiting on the forge and not on the person. No pipeline at all (a repo whose CI skips
 * the branch), and a canceled, skipped or unknown one, are not in progress: nothing will
 * move the merge request but the person.
 */
export function pipelineInProgress(l: LaneView): boolean {
  return l.pipeline_status === 'pending' || l.pipeline_status === 'running'
}

/** The require-approval job's statuses that hold a merge: not started, waiting to be played, blocked, or failed for want of a sign-off. */
const HELD_JOB = new Set(['pending', 'manual', 'blocked', 'failed', 'created', 'scheduled'])

/**
 * An open merge request whose pipeline is held (or failed) at the repo's require-approval
 * job and has no approval: only the person can move it. A pipeline that failed elsewhere
 * is broken, not waiting for them, so a held job counts only while the pipeline has not
 * failed, or when it is the job that failed.
 */
export function awaitingApproval(l: LaneView): boolean {
  const job = l.require_approval
  if (l.state === 'closed' || !mrOpen(l) || l.approved || !job || !HELD_JOB.has(job)) return false
  return l.pipeline_status !== 'failed' || job === 'failed'
}

/** Since the pipeline reached the job (finished, when it failed there), else started, else the MR last changed. */
function approvalSince(l: LaneView): string {
  return l.pipeline_finished ?? l.pipeline_started ?? l.mr_updated ?? l.created
}

export function boardItems({ lanes, runs, decisions, requests, seen, showDismissed = false }: BoardInput): BoardItem[] {
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
    // A dismissed run leaves the board with its lane's attention; the lane's next run is
    // another run and shows as usual.
    const dismissed = endedBadly(run) && !!run.dismissed
    const failedRun = (run?.state === 'failed' || run?.state === 'interrupted') && (!dismissed || showDismissed)
    const failedPipeline = mrOpen(l) && l.pipeline_status === 'failed'

    if (l.state === 'closed') {
      if (l.mr_state !== 'merged' || !l.closed || !unseen(seen, `lane:${l.id}`, l.closed)) continue
      items.push({ ...base, group: 'done', at: l.closed, detail: 'merged' })
    } else if (!failedRun && awaitingApproval(l)) {
      items.push({ ...base, group: 'needs', at: approvalSince(l), detail: `approve MR !${l.mr}` })
    } else if (failedRun || failedPipeline) {
      let detail = failedRun ? `run ${run.id} ${run.state}${dismissed ? ', dismissed' : ''}` : 'pipeline failed'
      if (running) detail += ', fixing'
      items.push({ ...base, group: 'broken', at: failedRun ? endedAt(run) : l.created, detail })
    } else if (running) {
      items.push({ ...base, group: 'working', at: run.started, detail: run.progress?.line ?? `run ${run.id}` })
    } else if (mrOpen(l) && !pipelineInProgress(l)) {
      // Passed, or nothing the forge is still running (no pipeline, canceled, skipped,
      // unknown): the merge request waits on the person.
      const detail = l.pipeline_status === 'passed' ? 'waiting for a merge'
        : l.pipeline_status ? `MR !${l.mr} open, pipeline ${l.pipeline_status}` : `MR !${l.mr} open, no pipeline`
      items.push({ ...base, group: 'review', at: l.created, detail })
    } else if (run?.ended && !dismissed && unseen(seen, `run:${run.id}`, run.ended)) {
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

/**
 * The counts with "needs you" taken from the attention store's list, so the bar counts
 * agree with every other place that counts what waits on the person. Until the list has
 * loaded the board's own rule stands.
 */
export function withAttention(n: Record<Group, number>, waiting: { items?: { kind: string }[] } | null): Record<Group, number> {
  return waiting ? { ...n, needs: waitingOnYouCount(waiting) } : n
}

/** Waiting kinds counted under "needs routing" or "broken", not "waiting on you". */
const COUNTED_ELSEWHERE = ['request', 'blocked']

/** Whether a waiting item of this kind waits on the person alone. */
export function waitsOnYou(kind: string): boolean {
  return !COUNTED_ELSEWHERE.includes(kind)
}

/** How many items wait on the person alone: the one count the title, strip and groups share. */
export function waitingOnYouCount(waiting: { items?: { kind: string }[] }): number {
  return (waiting.items ?? []).filter((it) => waitsOnYou(it.kind)).length
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

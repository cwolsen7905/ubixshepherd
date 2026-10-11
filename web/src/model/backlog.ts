// The chat pane's backlog: the work that waits on the person or has gone quiet, built on
// the board's rules (boardItems) so the two never disagree about what is broken or
// working. It differs in two ways: done work is history and is left out, and an open lane
// with no run in flight and nothing to wait for is listed as idle, which the board does not.
import type { FeedEntry } from './feed'
import { aboutLane } from './feed'
import {
  boardItems, laneClashes, latestRuns, endedBadly, mrOpen, type BoardItem,
} from './board'
import type { DecisionView, LaneView, RequestView, RunView } from '../api/types'
import type { Tone } from './marks'

export type BacklogGroup = 'needs' | 'broken' | 'idle' | 'working' | 'review'

/** Most urgent first. Idle sits above working: a run in flight needs nothing from anyone. */
export const BACKLOG_GROUPS: readonly BacklogGroup[] = ['needs', 'broken', 'idle', 'working', 'review']

export const BACKLOG_NAMES: Record<BacklogGroup, string> = {
  needs: 'needs you',
  broken: 'broken',
  idle: 'idle',
  working: 'working',
  review: 'to review',
}

export const BACKLOG_MARKS: Record<BacklogGroup, { glyph: string; tone: Tone }> = {
  needs: { glyph: '?', tone: 'warn' },
  broken: { glyph: '✗', tone: 'broken' },
  idle: { glyph: '○', tone: 'muted' },
  working: { glyph: '●', tone: 'accent' },
  review: { glyph: '◆', tone: 'accent' },
}

export interface BacklogItem {
  key: string
  group: BacklogGroup
  glyph?: string
  name: string
  repo: string
  laneId?: number
  lane?: LaneView
  run?: RunView
  decision?: DecisionView
  request?: RequestView
  detail: string
  /** Since when it has waited, working or sat idle; orders items within a group. */
  at: string
  /**
   * The thread row where this work last said something: the latest row about its lane
   * that the thread shows. Absent when the thread holds none, which includes work older
   * than the loaded window.
   */
  anchor?: string
}

export interface BacklogInput {
  lanes: LaneView[]
  runs: RunView[]
  decisions: DecisionView[]
  requests: RequestView[]
  feed: FeedEntry[]
  /** Feed ids of the rows the thread shows, which anchors may name. */
  shown: ReadonlySet<number>
  showDismissed?: boolean
}

/** The id of the thread element an anchor names: `f<feed id>` for a feed row, `d<id>` for an unplaced decision. */
export const threadId = (anchor: string) => `thread-${anchor}`

/** Everything unseen ended before this, so the board's "done" group comes back empty. */
const FOREVER = { until: '9999-12-31T00:00:00Z', keys: new Set<string>() }

export function backlogItems(input: BacklogInput): BacklogItem[] {
  const { lanes, runs, decisions, requests, feed, shown } = input
  const board = boardItems({ lanes, runs, decisions, requests, seen: FOREVER, showDismissed: input.showDismissed })
  const items: BacklogItem[] = board.filter((b) => b.group !== 'done').map((b) => fromBoard(b))

  const listed = new Set(board.map((b) => b.laneId))
  const latest = latestRuns(runs)
  const clash = laneClashes(lanes)
  for (const l of lanes) {
    if (l.state !== 'open' || listed.has(l.id)) continue
    const run = latest.get(l.id)
    if (run?.state === 'running') continue
    // A dismissed run is one the person has asked not to hear about.
    if (endedBadly(run) && run.dismissed) continue
    // Waiting on the forge is waiting, not idle: an open merge request whose pipeline runs.
    if (mrOpen(l) && l.pipeline_status !== 'failed' && l.pipeline_status !== 'passed') continue
    items.push({
      key: `idle:${l.id}`, group: 'idle', name: clash.has(l.name) && l.repo ? `${l.repo}:${l.name}` : l.name,
      repo: l.repo, laneId: l.id, lane: l, run, at: run?.ended ?? l.created,
      detail: run ? `last run ${run.id} ${run.state}` : 'no run yet',
    })
  }

  const rank = (g: BacklogGroup) => BACKLOG_GROUPS.indexOf(g)
  items.sort((a, b) => rank(a.group) - rank(b.group) || Date.parse(a.at) - Date.parse(b.at))

  // Newest first, so each lane finds its latest row on the first hit.
  const newest = feed.filter((f) => shown.has(f.id)).sort((a, b) => b.id - a.id)
  const refs = {
    runs,
    decisionRuns: new Map(decisions.map((d) => [d.id, d.run_id])),
    requests,
  }
  for (const it of items) it.anchor = anchorFor(it, newest, refs)
  return items
}

function fromBoard(b: BoardItem): BacklogItem {
  return {
    key: b.key, group: b.group as BacklogGroup, glyph: b.glyph, name: b.name, repo: b.repo, laneId: b.laneId,
    lane: b.lane, run: b.run, decision: b.decision, request: b.request, detail: b.detail, at: b.at,
  }
}

function anchorFor(it: BacklogItem, newest: FeedEntry[], refs: Parameters<typeof aboutLane>[2]): string | undefined {
  if (it.decision) {
    const id = it.decision.id
    const row = newest.find((f) => f.event === 'decision_asked' && f.ref === id)
    // An open decision always has a place in the thread, in its row or at the bottom.
    return row ? `f${row.id}` : `d${id}`
  }
  if (it.request) {
    const id = it.request.id
    const row = newest.find((f) => f.event === 'request_attention' && f.ref === id)
    return row ? `f${row.id}` : undefined
  }
  const lane = it.lane
  if (!lane) return undefined
  const key = { id: lane.id, name: lane.name, repo: it.repo }
  const row = newest.find((f) => aboutLane(f, key, refs))
  return row ? `f${row.id}` : undefined
}

export function backlogCounts(items: BacklogItem[]): Record<BacklogGroup, number> {
  const n: Record<BacklogGroup, number> = { needs: 0, broken: 0, idle: 0, working: 0, review: 0 }
  for (const it of items) n[it.group]++
  return n
}

/** "2 needs you · 1 broken · 3 idle", the groups that hold anything. */
export function backlogText(n: Record<BacklogGroup, number>): string {
  return BACKLOG_GROUPS.filter((g) => n[g] > 0).map((g) => `${n[g]} ${BACKLOG_NAMES[g]}`).join(' · ')
}

/** How many rows the rail shows before "more"; about a third of a screen. */
export const RAIL_ROWS = 8

const OPEN_KEY = 'shepherd.chat.backlog'

/** The stored open/closed choice; null when none was made. */
export function readRailOpen(store: Pick<Storage, 'getItem'>): boolean | null {
  try {
    const v = store.getItem(OPEN_KEY)
    return v === 'open' ? true : v === 'closed' ? false : null
  } catch {
    return null
  }
}

export function writeRailOpen(store: Pick<Storage, 'setItem'>, open: boolean) {
  try {
    store.setItem(OPEN_KEY, open ? 'open' : 'closed')
  } catch {
    // no storage: the choice lasts until the page reloads
  }
}

// The attention selectors: every count of "what needs me" is read from the one list the
// store holds (GET /v1/waiting), so the title, favicon, sidebar, backlog head and topic
// chips cannot disagree.
import type { Waiting, WaitingItem } from '../api/types'
import { boardItems, counts, type BoardInput } from '../model/board'

/** How many things wait on the person; 0 until the list has loaded. */
export function attentionCount(w: Waiting | null): number {
  return w?.count ?? 0
}

/** Kinds counted elsewhere: requests under "needs routing", blocked lanes under "broken". */
const COUNTED_ELSEWHERE: WaitingItem['kind'][] = ['request', 'blocked']

/** Items that wait on the person alone: everything but what needs routing or is broken. */
export function waitingOnYou(w: Waiting | null): WaitingItem[] {
  return (w?.items ?? []).filter((it) => !COUNTED_ELSEWHERE.includes(it.kind))
}

/** Items of one kind, e.g. the decisions and requests behind the sidebar's count. */
export function attentionOf(w: Waiting | null, ...kinds: WaitingItem['kind'][]): WaitingItem[] {
  return (w?.items ?? []).filter((it) => kinds.includes(it.kind))
}

/** The Decisions entry's count: what waits on the person, as the sidebar's own count. */
export function inboxCount(w: Waiting | null): number {
  return w ? waitingOnYou(w).length : 0
}

/** Items that wait on the lane named `lane` of `repo`. */
export function attentionForLane(w: Waiting | null, repo: string, lane: string): WaitingItem[] {
  return (w?.items ?? []).filter((it) => it.lane === lane && (!it.repo || it.repo === repo))
}

/** "3 waiting on you" in the window title, or '' when nothing waits. */
export function attentionTitle(w: Waiting | null): string {
  const n = attentionCount(w)
  return n > 0 ? `${n} waiting on you` : ''
}

/** The numbers of the page head's strip; the sidebar and title read the same parts. */
export interface StatusCounts {
  /** Lanes with a run in flight: the board's working group. */
  running: number
  /** What waits on the person alone, not counted under broken or routing. */
  waiting: number
  /** The board's broken group plus blocked items, one per lane. */
  broken: number
  /** Requests no rule could route. */
  routing: number
}

/** The part of the snapshot the counts read. */
export type StatusInput = Omit<BoardInput, 'showDismissed'> & { waiting: Waiting | null; showDismissed?: boolean }

/** The status counts, from the one list and the board's own rules. */
export function statusCounts(snap: StatusInput): StatusCounts {
  const items = boardItems(snap)
  const n = counts(items)
  // A lane that both failed and is blocked counts once.
  const lanes = new Set<string>()
  for (const it of items) if (it.group === 'broken') lanes.add(it.laneId ? `id:${it.laneId}` : `name:${it.repo}:${it.name}`)
  for (const it of attentionOf(snap.waiting, 'blocked')) {
    const id = it.lane_id ?? snap.lanes.find((l) => l.name === it.lane && (!it.repo || l.repo === it.repo))?.id
    lanes.add(id ? `id:${id}` : `name:${it.repo ?? ''}:${it.lane ?? it.title}`)
  }
  return {
    running: n.working,
    waiting: waitingOnYou(snap.waiting).length,
    broken: lanes.size,
    routing: attentionOf(snap.waiting, 'request').length,
  }
}

/** The Decisions entry's badge title: the parts of its count. */
export function inboxTitle(w: Waiting | null): string {
  const d = attentionOf(w, 'decision').length
  const r = attentionOf(w, 'request').length
  return `${d} decision${d === 1 ? '' : 's'}, ${r} request${r === 1 ? '' : 's'} needing routing`
}

/** The part of a topic's links that wait counts read. */
interface TopicLinks {
  waits: number
  links: { kind: string; ref: string; waits?: boolean }[]
}

/**
 * How many things under a topic wait on the person, read from the one list: a linked
 * decision, request or lane that has an item there. Before the list loads the topic's own
 * count stands, so the chip never shows less than the daemon said.
 */
export function topicWaits(t: TopicLinks, w: Waiting | null): number {
  if (!w) return t.waits
  const n = (s: string) => Number(s)
  let waits = 0
  for (const l of t.links) {
    const id = n(l.ref)
    const hit = (w.items ?? []).some((it) =>
      (l.kind === 'decision' && it.decision_id === id) ||
      (l.kind === 'request' && it.request_id === id) ||
      (l.kind === 'lane' && it.lane_id === id) ||
      (l.kind === 'next_item' && it.next_id === id),
    )
    if (hit) waits++
  }
  return waits
}

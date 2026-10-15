// The attention selectors: every count of "what needs me" is read from the one list the
// store holds (GET /v1/waiting), so the title, favicon, sidebar, backlog head and topic
// chips cannot disagree.
import type { Waiting, WaitingItem } from '../api/types'

/** How many things wait on the person; 0 until the list has loaded. */
export function attentionCount(w: Waiting | null): number {
  return w?.count ?? 0
}

/** Items of one kind, e.g. the decisions and requests behind the sidebar's count. */
export function attentionOf(w: Waiting | null, ...kinds: WaitingItem['kind'][]): WaitingItem[] {
  return (w?.items ?? []).filter((it) => kinds.includes(it.kind))
}

/** Decisions plus requests that need routing: the Decisions entry's count. */
export function inboxCount(w: Waiting | null): number {
  return attentionOf(w, 'decision', 'request').length
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

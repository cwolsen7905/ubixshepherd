// Stepping through what waits on the person: the header counter's up and down arrows pick
// the next item, and the chat page scrolls to it in the thread and highlights it.
import type { NextUpdateBody, WaitingItem } from '../api/types'
import type { Open } from '../router'

/** A stable key for an item: the daemon's list has no ids of its own. */
export function waitingKey(it: WaitingItem): string {
  return `${it.kind}:${it.question_id ?? it.decision_id ?? it.next_id ?? it.lane_id ?? 0}:${it.mr ?? 0}`
}

/**
 * The index to step to from the item with key `current` (null before the first step).
 * Down goes to the next item and wraps; up goes to the previous. A current item that left
 * the list starts from the top (down) or the bottom (up).
 */
export function stepIndex(keys: readonly string[], current: string | null, dir: 'up' | 'down'): number {
  if (keys.length === 0) return -1
  const at = current === null ? -1 : keys.indexOf(current)
  if (at < 0) return dir === 'down' ? 0 : keys.length - 1
  return (at + (dir === 'down' ? 1 : -1) + keys.length) % keys.length
}

/** Fired on window when an item is to be shown; the chat page takes it up. */
export const STEP_EVENT = 'shepherd:waiting-step'

let pending: WaitingItem | null = null

/** Asks the chat page to show the item. One that is not mounted yet takes it on mount. */
export function requestStep(it: WaitingItem) {
  pending = it
  window.dispatchEvent(new Event(STEP_EVENT))
}

/** The item asked for and not yet shown, once. */
export function takeStep(): WaitingItem | null {
  const it = pending
  pending = null
  return it
}

/** What the thread shows of the work a feed row reports, for decisions and lanes. */
export interface StepContext {
  /** Open decisions' "asked" rows in the thread: decision id to feed row id. */
  askedRows: ReadonlyMap<number, number>
  /** Decisions shown as cards of their own below the thread. */
  unplaced: ReadonlySet<number>
  /** A lane's latest thread anchor, from the backlog. */
  laneAnchor(laneId: number): string | undefined
}

/**
 * The thread anchor (an id suffix for threadId) that shows the item, or null when the
 * thread holds no row for it: a Next-up proposal, or work older than the loaded window.
 */
export function stepAnchor(it: WaitingItem, ctx: StepContext): string | null {
  switch (it.kind) {
    case 'question':
      return it.question_id ? `q${it.question_id}` : null
    case 'decision': {
      const id = it.decision_id ?? 0
      const row = ctx.askedRows.get(id)
      if (row) return `f${row}`
      return ctx.unplaced.has(id) ? `d${id}` : null
    }
    case 'mr':
    case 'ship':
      return (it.lane_id && ctx.laneAnchor(it.lane_id)) || null
    default:
      return null
  }
}

/** Opens the chat page when another page is showing, then shows the item there. */
export function showInThread(it: WaitingItem) {
  if (!window.location.hash.startsWith('#/chat')) window.location.hash = '#/chat'
  requestStep(it)
}

/** The inspector item that shows a waiting item in full, or null for kinds with no pane yet. */
export function openTarget(it: WaitingItem): Open | null {
  if (it.kind === 'decision' && it.decision_id) return { kind: 'decision', id: it.decision_id }
  if (it.kind === 'question' && it.question_id) return { kind: 'question', id: it.question_id }
  return null
}

/** The key of the waiting item the inspector holds, or null when it holds none of them. */
export function openKey(items: readonly WaitingItem[], open: Open | undefined): string | null {
  if (!open) return null
  const hit = items.find((it) => {
    const t = openTarget(it)
    return t !== null && t.kind === open.kind && t.id === open.id
  })
  return hit ? waitingKey(hit) : null
}

/**
 * The next waiting item to move to from the inspector's open item (or the first when none is
 * open) after `dir`, never the item `skip` names. Used by the strip's keys and after an answer.
 */
export function neighbour(items: readonly WaitingItem[], from: string | null, dir: 'up' | 'down', skip?: string): WaitingItem | undefined {
  const rest = skip ? items.filter((it) => waitingKey(it) !== skip) : items
  if (rest.length === 0) return undefined
  const keys = rest.map(waitingKey)
  // An answered item left the list: continue from where it stood, so "next" is its successor.
  let start = from
  if (from !== null && !keys.includes(from)) {
    const at = items.findIndex((it) => waitingKey(it) === from)
    start = at > 0 ? waitingKey(items[at - 1] as WaitingItem) : null
    if (start !== null && !keys.includes(start)) start = null
  }
  return rest[stepIndex(keys, start, dir)]
}

/** What the person can do about a "done?" suggestion on a next-up item. */
export type SuggestionChoice = 'done' | 'drop' | 'keep'

/**
 * Is `it` a suggestion that a next-up item may be done? The daemon sends these as kind `next`
 * with action `decide` and the suggestion line in `detail`. (`decide` is not yet in the
 * WaitingItem action union, hence the widening.)
 */
export function isSuggestion(it: WaitingItem): boolean {
  return it.kind === 'next' && (it.action as string) === 'decide' && !!it.next_id
}

/**
 * The choices a suggestion offers, in button order: "done?" offers done and still needed;
 * "done or drop?" also offers drop. Accepting and dropping are the person's call; "still
 * needed" dismisses the suggestion and resets the weekly re-check.
 */
export function suggestionChoices(it: WaitingItem): SuggestionChoice[] {
  return /^done or drop\b/i.test(it.detail ?? '') ? ['done', 'drop', 'keep'] : ['done', 'keep']
}

/** The PATCH /v1/next/{id} body for a choice. An empty `suggestion` dismisses it. */
export type SuggestionBody = NextUpdateBody & { suggestion?: string }

export function suggestionPatch(choice: SuggestionChoice): SuggestionBody {
  switch (choice) {
    case 'done':
      return { state: 'done' }
    case 'drop':
      return { state: 'dropped' }
    case 'keep':
      return { suggestion: '' }
  }
}

export const SUGGESTION_LABEL: Record<SuggestionChoice, string> = { done: 'Done', drop: 'Drop', keep: 'Still needed' }

// What the person has seen, kept in this browser. Quiet by default: only things that wait
// on the person carry an unseen mark, and a mark clears when the item is opened. The record
// also remembers when the person last looked, so a return after a gap can say what happened.
import { useSyncExternalStore } from 'react'
import type { WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { waitingKey } from '../model/waitingStep'

const KEY = 'shepherd.seen.v1'
/** The most seen keys kept: the oldest fall away. */
export const KEEP_SEEN = 300
/** A gap shorter than this is not "away". */
export const AWAY_AFTER_MS = 5 * 60_000

export interface SeenRecord {
  /** Waiting keys the person has opened, oldest first. */
  seen: string[]
  /** When the person last looked at the tab (ms since the epoch); 0 when never. */
  looked: number
  /** Browser notifications for what waits on the person: off until the person turns them on. */
  notify: boolean
}

const EMPTY: SeenRecord = { seen: [], looked: 0, notify: false }

function read(storage: Storage | null): SeenRecord {
  try {
    const raw = storage?.getItem(KEY)
    if (!raw) return EMPTY
    const v = JSON.parse(raw) as Partial<SeenRecord>
    return {
      seen: Array.isArray(v.seen) ? v.seen.filter((s): s is string => typeof s === 'string') : [],
      looked: typeof v.looked === 'number' ? v.looked : 0,
      notify: v.notify === true,
    }
  } catch {
    return EMPTY
  }
}

export class SeenStore {
  private rec: SeenRecord
  private readonly subs = new Set<() => void>()
  constructor(private readonly storage: Storage | null) {
    this.rec = read(storage)
  }

  subscribe = (fn: () => void) => {
    this.subs.add(fn)
    return () => void this.subs.delete(fn)
  }
  getSnapshot = () => this.rec

  /** True when nothing was ever recorded here: the first visit, whose waiting items are not "new". */
  get fresh() {
    return this.rec.looked === 0 && this.rec.seen.length === 0
  }

  private set(next: SeenRecord, notify = true) {
    this.rec = next
    try {
      this.storage?.setItem(KEY, JSON.stringify(next))
    } catch {
      // A full or blocked store only loses the memory; the page works without it.
    }
    if (notify) this.subs.forEach((fn) => fn())
  }

  mark(keys: readonly string[]) {
    const have = new Set(this.rec.seen)
    const add = keys.filter((k) => !have.has(k))
    if (add.length === 0) return
    this.set({ ...this.rec, seen: [...this.rec.seen, ...add].slice(-KEEP_SEEN) })
  }
  /** Records that the person looked at `now`. */
  look(now: number) {
    // Quiet: nothing on screen depends on it, so subscribers are not woken every tick.
    this.set({ ...this.rec, looked: now }, false)
  }
  setNotify(on: boolean) {
    if (this.rec.notify !== on) this.set({ ...this.rec, notify: on })
  }
}

/** The browser's storage, or null where reading it throws (blocked cookies, a sandboxed frame). */
export function browserStorage(): Storage | null {
  try {
    return typeof window === 'undefined' ? null : window.localStorage
  } catch {
    return null
  }
}

export const seenStore = new SeenStore(browserStorage())

export function useSeen(store: SeenStore = seenStore): SeenRecord {
  return useSyncExternalStore(store.subscribe, store.getSnapshot)
}

export const isUnseen = (rec: SeenRecord, it: WaitingItem) => !rec.seen.includes(waitingKey(it))

export interface Away {
  /** Items that wait on the person and were not there before they left, first in line. */
  needs: WaitingItem[]
  runs: number
  failed: number
  merges: number
}

/** True when the summary has nothing to say. */
export const quiet = (a: Away) => a.needs.length + a.runs + a.failed + a.merges === 0

const MERGED = /\bmerged\b/i
const ALREADY = /already merged/i

/** What happened since `since`: what now waits and is newer, and the routine finishes folded to counts. */
export function awaySummary(waiting: readonly WaitingItem[], feed: readonly FeedEntry[], since: number): Away {
  const newer = (iso: string | undefined) => {
    const t = iso ? Date.parse(iso) : NaN
    return Number.isFinite(t) && t > since
  }
  const recent = feed.filter((f) => newer(f.created))
  return {
    needs: waiting.filter((w) => newer(w.since)),
    runs: recent.filter((f) => f.event === 'run_passed' || f.event === 'run_ended').length,
    failed: recent.filter((f) => f.event === 'run_failed').length,
    merges: recent.filter((f) => f.event === 'mr' && MERGED.test(f.text) && !ALREADY.test(f.text)).length,
  }
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

/** Waiting items by what the person must do, as "2 decisions, 1 question". */
export function needsLine(items: readonly WaitingItem[]): string {
  const n = (f: (w: WaitingItem) => boolean) => items.filter(f).length
  const kinds: [WaitingItem['kind'], string, string][] = [
    ['decision', 'decision', 'decisions'],
    ['question', 'question', 'questions'],
    ['mr', 'merge request to review', 'merge requests to review'],
    ['ship', 'lane to ship', 'lanes to ship'],
    ['next', 'next-up item', 'next-up items'],
  ]
  const named = kinds.map(([k, one, many]) => [n((w) => w.kind === k), one, many] as const)
  const other = items.length - named.reduce((t, [c]) => t + c, 0)
  return [...named.map(([c, one, many]) => c > 0 && plural(c, one, many)), other > 0 && plural(other, 'other item')]
    .filter(Boolean)
    .join(', ')
}

/** The routine part, kept short: "3 runs finished, 1 failed, 1 merge". */
export function routineLine(a: Away): string {
  return [a.runs && `${plural(a.runs, 'run')} finished`, a.failed && `${a.failed} failed`, a.merges && plural(a.merges, 'merge')]
    .filter(Boolean)
    .join(', ')
}

/** What a new waiting item is called when announced: "New decision from feat/export-csv". */
export function announceText(it: WaitingItem): string {
  const what = it.kind === 'decision' ? 'decision' : it.kind === 'question' ? 'question' : it.kind === 'mr' ? 'merge request to review' : 'item'
  return `New ${what}${it.lane ? ` from ${it.lane}` : ''}`
}

/** Only a question or decision waits on the person in a way worth a notification. */
export const notifiable = (it: WaitingItem) => it.kind === 'decision' || it.kind === 'question'

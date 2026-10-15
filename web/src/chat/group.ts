import type { DeskEvent } from '../api/types'
import type { FeedEntry } from '../model/feed'
import type { ThreadItem } from '../state/desk'

/** Consecutive feed events at or above this count fold into one "N updates" row. */
export const FOLD_AT = 3

/** What the thread draws: a single item, a folded run of events, or a reply with its work. */
export type Row =
  | { type: 'item'; key: string; it: ThreadItem }
  | { type: 'updates'; key: string; feed: FeedEntry[] }
  | { type: 'reply'; key: string; e: DeskEvent; work: DeskEvent[] }
  | { type: 'work'; key: string; work: DeskEvent[] }

/** A decision stays on its own row: it waits on the person and has a place to jump to. */
const standalone = (f: FeedEntry) => f.event === 'decision_asked'

/** The feed ids a row holds, for a jump to a row that is folded. */
export function rowFeedIds(r: Row): number[] {
  if (r.type === 'updates') return r.feed.map((f) => f.id)
  if (r.type === 'item' && r.it.type === 'feed') return [r.it.f.id]
  return []
}

/**
 * Groups a thread for reading: three or more consecutive feed events (decisions apart)
 * become one row; a desk turn's tool calls and "woke the front desk" notes gather under
 * the reply they produced when nothing else is drawn between them, or stand as one folded
 * row where they happened. Order is kept; nothing is dropped.
 */
export function groupThread(items: ThreadItem[]): Row[] {
  const out: Row[] = []
  let run: FeedEntry[] = []
  let work: DeskEvent[] = []

  const flushRun = () => {
    if (run.length >= FOLD_AT) out.push({ type: 'updates', key: `u${run[0]?.id}`, feed: run })
    else for (const f of run) out.push({ type: 'item', key: `f${f.id}`, it: { type: 'feed', key: `f${f.id}`, f } })
    run = []
  }
  const flushWork = () => {
    if (work.length) out.push({ type: 'work', key: `w${work[0]?.seq}`, work })
    work = []
  }

  for (const it of items) {
    if (it.type === 'feed' && !standalone(it.f)) {
      flushWork()
      run.push(it.f)
      continue
    }
    flushRun()
    // Work stays where it happened: anything else that is drawn first closes the open fold.
    if (work.length > 0 && !(it.type === 'desk' && (it.e.kind === 'tool' || it.e.kind === 'system' || it.e.kind === 'assistant' || it.e.kind === 'turn_start'))) flushWork()
    if (it.type === 'desk' && (it.e.kind === 'tool' || it.e.kind === 'system')) {
      work.push(it.e)
    } else if (it.type === 'desk' && it.e.kind === 'assistant') {
      out.push({ type: 'reply', key: it.key, e: it.e, work })
      work = []
    } else {
      out.push({ type: 'item', key: it.key, it })
    }
  }
  flushRun()
  flushWork()
  return out
}

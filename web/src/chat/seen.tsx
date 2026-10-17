import { useEffect, useMemo, useRef, useState } from 'react'
import type { WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { openKey, waitingKey } from '../model/waitingStep'
import { clockTime, duration } from '../format'
import { useNow } from '../hooks'
import { href, parse } from '../router'
import {
  AWAY_AFTER_MS, announceText, awaySummary, notifiable, quiet, routineLine, seenStore, useSeen,
} from '../state/seen'
import { mayNotify, notifyWaiting } from './notify'
import { showWaiting } from './strip'

const LOOK_EVERY = 30_000

/**
 * Keeps the seen record: the first visit counts what waits as seen, an item the inspector
 * opens is marked seen, and the time of the last look is kept so a return can say what changed.
 * Returns when the person went away (ms), or null.
 */
export function useSeenWatch(items: readonly WaitingItem[] | null, hash: string, now: () => number = Date.now) {
  const [away, setAway] = useState<number | null>(() => {
    const looked = seenStore.getSnapshot().looked
    return looked > 0 && now() - looked > AWAY_AFTER_MS ? looked : null
  })
  const first = useRef(seenStore.fresh)
  useEffect(() => {
    const stamp = () => seenStore.look(now())
    stamp()
    let hiddenAt = 0
    const onVis = () => {
      if (document.visibilityState === 'hidden') {
        hiddenAt = now()
        stamp()
      } else {
        if (hiddenAt > 0 && now() - hiddenAt > AWAY_AFTER_MS) setAway(hiddenAt)
        hiddenAt = 0
        stamp()
      }
    }
    const id = window.setInterval(() => document.visibilityState !== 'hidden' && stamp(), LOOK_EVERY)
    document.addEventListener('visibilitychange', onVis)
    window.addEventListener('pagehide', stamp)
    return () => {
      window.clearInterval(id)
      document.removeEventListener('visibilitychange', onVis)
      window.removeEventListener('pagehide', stamp)
    }
  }, [now])
  useEffect(() => {
    if (!items) return
    if (first.current) {
      first.current = false
      seenStore.mark(items.map(waitingKey))
      return
    }
    const open = openKey(items, parse(hash).open)
    if (open && document.visibilityState !== 'hidden') seenStore.mark([open])
  }, [items, hash])
  return { away, dismiss: () => setAway(null) }
}

/**
 * Says a new waiting item once, politely, through a region of its own (the thread's log is not
 * disturbed and focus never moves). With the opt-in on and the tab hidden, a question or a
 * decision also becomes a browser notification; routine events never do.
 */
export function Announcer({ items }: { items: readonly WaitingItem[] | null }) {
  const { notify } = useSeen()
  const known = useRef<Set<string> | null>(null)
  const [said, setSaid] = useState('')
  const clearTimer = useRef<number | undefined>(undefined)
  const sayTimer = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (!items) return
    const keys = new Set(items.map(waitingKey))
    const before = known.current
    known.current = keys
    if (!before) return
    const added = items.filter((it) => !before.has(waitingKey(it)))
    if (added.length === 0) return
    const text = added.length === 1 ? announceText(added[0] as WaitingItem) : `${added.length} new items waiting on you`
    // Deferred so the effect does not set state synchronously. A later announcement replaces
    // the clear timer; an unrelated re-run leaves it alone, so the text always goes away.
    window.clearTimeout(clearTimer.current)
    window.clearTimeout(sayTimer.current)
    sayTimer.current = window.setTimeout(() => setSaid(text), 0)
    clearTimer.current = window.setTimeout(() => setSaid(''), 8000)
    if (mayNotify(notify) && document.visibilityState === 'hidden') {
      const waits = added.filter(notifiable)
      if (waits.length > 0) notifyWaiting(waits, () => showWaiting(waits[0] as WaitingItem))
    }
  }, [items, notify])
  useEffect(
    () => () => {
      window.clearTimeout(clearTimer.current)
      window.clearTimeout(sayTimer.current)
    },
    [],
  )
  return (
    <p className="visually-hidden" role="status" aria-live="polite" aria-atomic="true">
      {said}
    </p>
  )
}

const LABEL: Partial<Record<WaitingItem['kind'], string>> = { blocked: 'Blocked', scope: 'Scope', decision: 'Decision', question: 'Question', mr: 'Merge request', ship: 'To ship', next: 'Next up', request: 'Request' }
/** The most waiting items the card lists by name; the rest are counted. */
const AWAY_ITEMS = 5

/**
 * "Since you were away" (`.sh-away`): what now waits on the person and is newer than their last
 * look, each named with a way to open it, and the routine finishes folded to one line. "Mark all
 * seen" clears the dots and the card. A gap with nothing to say shows nothing.
 */
export function AwayBlock({
  since, items, feed, onDismiss, now: given,
}: {
  since: number | null
  items: readonly WaitingItem[]
  feed: readonly FeedEntry[]
  /** Closes the card; "Mark all seen" calls it after marking everything waiting as seen. */
  onDismiss: () => void
  now?: number
}) {
  const ticked = useNow(60_000)
  const now = given ?? ticked
  const a = useMemo(() => (since === null ? null : awaySummary(items, feed, since)), [since, items, feed])
  if (!a || since === null || quiet(a)) return null
  const routine = routineLine(a)
  const named = a.needs.slice(0, AWAY_ITEMS)
  const rest = a.needs.length - named.length
  return (
    <section className="sh-away" data-component="AwayCard" aria-label="Since you were away">
      <div className="sh-away-head">
        <b>Since you were away</b>
        <span className="sh-away-when">
          {duration(new Date(since).toISOString(), undefined, now)}, last here {clockTime(new Date(since).toISOString(), now)}
        </span>
        <button
          type="button"
          className="sh-away-mark"
          onClick={() => {
            seenStore.mark(items.map(waitingKey))
            onDismiss()
          }}
        >
          Mark all seen
        </button>
      </div>
      <ul>
        {named.map((it) => (
          <li key={waitingKey(it)}>
            {LABEL[it.kind] ?? it.kind}: {it.title}{' '}
            <button type="button" className="sh-link sh-away-open" onClick={() => showWaiting(it)}>
              {it.action === 'answer' ? 'Answer' : it.action === 'reply' ? 'Reply' : 'Open'}
            </button>
          </li>
        ))}
        {rest > 0 && (
          <li>
            {rest} more waiting on you. <a href={href.inbox()}>Open the inbox</a>
          </li>
        )}
        {routine && <li>{routine}.</li>}
      </ul>
    </section>
  )
}

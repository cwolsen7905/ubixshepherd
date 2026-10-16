import { useEffect, useMemo, useRef, useState } from 'react'
import type { WaitingItem } from '../api/types'
import type { FeedEntry } from '../model/feed'
import { openKey, waitingKey } from '../model/waitingStep'
import { href, parse } from '../router'
import {
  AWAY_AFTER_MS, announceText, awaySummary, needsLine, notifiable, quiet, routineLine, seenStore, useSeen,
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
  useEffect(() => {
    if (!items) return
    const keys = new Set(items.map(waitingKey))
    const before = known.current
    known.current = keys
    if (!before) return
    const added = items.filter((it) => !before.has(waitingKey(it)))
    if (added.length === 0) return
    const text = added.length === 1 ? announceText(added[0] as WaitingItem) : `${added.length} new items waiting on you`
    // Deferred so the effect does not set state synchronously.
    const t = window.setTimeout(() => setSaid(text), 0)
    const clear = window.setTimeout(() => setSaid(''), 8000)
    if (mayNotify(notify) && document.visibilityState === 'hidden') {
      for (const it of added) if (notifiable(it)) notifyWaiting(it, () => showWaiting(it))
    }
    return () => {
      window.clearTimeout(t)
      window.clearTimeout(clear)
    }
  }, [items, notify])
  return (
    <p className="visually-hidden" role="status" aria-live="polite" aria-atomic="true">
      {said}
    </p>
  )
}

/** "While you were away": what needs the person first, the routine news folded to one quiet line. */
export function AwayBlock({
  since, items, feed, onDismiss,
}: {
  since: number | null
  items: readonly WaitingItem[]
  feed: readonly FeedEntry[]
  onDismiss: () => void
}) {
  const a = useMemo(() => (since === null ? null : awaySummary(items, feed, since)), [since, items, feed])
  if (!a || quiet(a)) return null
  const routine = routineLine(a)
  return (
    <section className="away" aria-label="While you were away">
      <p className="away-line">
        <strong>While you were away</strong>
        {a.needs.length > 0 && (
          <>
            {': '}
            <a className="away-needs" href={href.inbox()}>
              {needsLine(a.needs)} waiting on you
            </a>
          </>
        )}
        {routine && <span className="faint away-routine">{a.needs.length > 0 ? '. Also: ' : ': '}{routine}.</span>}
        <button type="button" className="link-button away-dismiss" onClick={onDismiss}>
          Dismiss
        </button>
      </p>
    </section>
  )
}

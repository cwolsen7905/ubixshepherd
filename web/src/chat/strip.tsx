import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { WaitingItem } from '../api/types'
import { useKeys } from '../hooks'
import { neighbour, openKey, openTarget, requestStep, waitingKey } from '../model/waitingStep'
import { go, parse, withOpen } from '../router'
import { cutAtWord, fitCount } from '../components/TopicBar'
import { UnseenDot } from '../components/UnseenDot'
import { useSnapshot } from '../state/context'
import { isUnseen, useSeen } from '../state/seen'

const LABEL: Partial<Record<WaitingItem['kind'], string>> = {
  decision: 'Decision',
  question: 'Question',
  mr: 'Merge request',
  ship: 'To ship',
  next: 'Next up',
  request: 'Request',
}

/** The longest an item's title gets in the strip; the full text is the tooltip. */
export const ITEM_MAX = 24

/** A title cut at a whole word to at most `max` characters, "…" after a cut. */
export const shortTitle = (title: string, max = ITEM_MAX) => cutAtWord(title, max)

/** Shows one waiting item: in the inspector when it has a pane, else in the thread. */
export function showWaiting(it: WaitingItem) {
  const target = openTarget(it)
  if (target) go(withOpen(window.location.hash, target))
  else requestStep(it)
}

/** Opens the item after the one just answered, or closes the inspector when nothing else waits. */
export function advanceFrom(items: readonly WaitingItem[], answered: string) {
  const next = neighbour(items, answered, 'down', answered)
  if (!next) go(withOpen(window.location.hash, undefined))
  else showWaiting(next)
}

/**
 * Above the composer: how many things wait on the person, each as a chip, with `]` and `[`
 * to step to the next and previous one. Stepping opens the item in the inspector and leaves
 * the thread where it is.
 */
export function PendingStrip() {
  const list = useSnapshot().waiting?.items
  const seen = useSeen()
  const items = useMemo(() => list ?? [], [list])
  const at = openKey(items, parse(window.location.hash).open)
  const step = useCallback(
    (dir: 'up' | 'down') => {
      const it = neighbour(items, openKey(items, parse(window.location.hash).open), dir)
      if (it) showWaiting(it)
    },
    [items],
  )
  const box = useRef<HTMLUListElement>(null)
  const [fit, setFit] = useState<number | null>(null)
  // Every chip is measured (the ones past the fit stay in the DOM, invisible); as many as fit show.
  useLayoutEffect(() => {
    const el = box.current
    if (!el) return
    const measure = () => {
      const widths = Array.from(el.querySelectorAll('button'), (c) => c.offsetWidth)
      const n = el.clientWidth === 0 ? widths.length : fitCount(widths, el.clientWidth - 2, 0)
      setFit((f) => (f === n ? f : n))
    }
    measure()
    if (typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(measure)
    ro.observe(el)
    return () => ro.disconnect()
  }, [items])
  useKeys({ ']': () => step('down'), '[': () => step('up') }, [step])
  if (items.length === 0) return null
  return (
    <nav className="pending-strip" aria-label="Waiting on you">
      <span className="pending-count tone-warn">{items.length} waiting on you</span>
      <ul ref={box}>
        {items.map((it, i) => {
          const key = waitingKey(it)
          return (
            <li key={key}>
              <button
                type="button" className={`pending-chip${fit !== null && i >= fit ? ' is-over' : ''}`} aria-current={key === at ? 'true' : undefined}
                title={`${LABEL[it.kind] ?? it.kind}: ${it.title}`} tabIndex={fit !== null && i >= fit ? -1 : undefined} onClick={() => showWaiting(it)}
              >
                {isUnseen(seen, it) ? <UnseenDot /> : null}
                <span className="faint">{LABEL[it.kind] ?? it.kind}</span> {shortTitle(it.title)}
              </button>
            </li>
          )
        })}
      </ul>
      <span className="faint pending-keys">
        <kbd>]</kbd> next
      </span>
    </nav>
  )
}
